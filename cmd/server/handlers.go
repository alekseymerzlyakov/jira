package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alekseymerzlyakov/jira/internal/history"
	"github.com/alekseymerzlyakov/jira/internal/jira"
)

type searchRequest struct {
	Query      string   `json:"query"`      // natural language or JQL
	JQL        string   `json:"jql"`        // explicit JQL override
	MaxResults int      `json:"maxResults"` // optional limit
	Fields     []string `json:"fields"`     // optional fields
	Projects   []string `json:"projects"`   // optional project keys
	Users      []string `json:"users"`      // optional assignee logins
	DryRun     bool     `json:"dryRun"`     // if true, return JQL only
	Analysis   bool     `json:"analysis"`   // if true, LLM summarizes results
	SprintID   int      `json:"sprintId"`   // optional sprint id
}

type searchResponse struct {
	JQL       string          `json:"jql"`
	Raw       json.RawMessage `json:"raw"`
	History   []history.Entry `json:"history"`
	Executed  time.Time       `json:"executedAt"`
	Analysis  string          `json:"analysis,omitempty"`
	Total     int             `json:"total,omitempty"`
	Issues    []issueLink     `json:"issues,omitempty"`
	Steps     []history.Step  `json:"steps,omitempty"`
	HistoryID string          `json:"historyId,omitempty"`
}

func (h *apiHandler) health() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (h *apiHandler) myself() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, status, err := h.jira.Myself(r.Context())
		if err != nil {
			respondErrorWithBody(w, status, err, body, "")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

func (h *apiHandler) projects() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := h.jira.Get(r.Context(), "/rest/api/2/project")
		if err != nil {
			respondErrorWithBody(w, http.StatusBadGateway, err, body, "")
			return
		}
		var raw []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			http.Error(w, "cannot parse projects", http.StatusInternalServerError)
			return
		}
		raw = filterProjects(raw)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(raw); err != nil {
			http.Error(w, "encode response", http.StatusInternalServerError)
		}
	})
}

func (h *apiHandler) phrases() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list := h.phrasesStore.List()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var payload struct {
				Phrases []string `json:"phrases"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if err := h.phrasesStore.Replace(payload.Phrases); err != nil {
				http.Error(w, "cannot save phrases", http.StatusInternalServerError)
				return
			}
			list := h.phrasesStore.List()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (h *apiHandler) projectSprints() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/projects/") {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
		if len(parts) < 2 || parts[1] != "sprints" {
			http.NotFound(w, r)
			return
		}
		projectKey := parts[0]
		if strings.ToUpper(projectKey) != "CE" {
			// временно поддерживаем спринты только для CE (Simulator.Company)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		limit := 5
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
				limit = n
			}
		}

		// жёстко используем борд 209 (рабочий для CE), чтобы не зависеть от env
		boardID := 209
		if boardID == 0 {
			http.Error(w, "board not found for project", http.StatusBadGateway)
			return
		}

		maxFetch := 50
		all := make([]struct {
			ID        int       `json:"id"`
			Name      string    `json:"name"`
			StartDate time.Time `json:"startDate"`
			EndDate   time.Time `json:"endDate"`
		}, 0)
		states := []string{"active", "future"}
		var lastErr error
		success := false
		var errs []string
		for _, st := range states {
			sps, status, err := h.jira.ListSprints(r.Context(), boardID, st, maxFetch)
			if err != nil || status >= 400 {
				errs = append(errs, fmt.Sprintf("state=%s status=%d err=%v", st, status, err))
				lastErr = fmt.Errorf("list sprints state=%s status=%d err=%v", st, status, err)
				continue
			}
			for _, sp := range sps {
				all = append(all, struct {
					ID        int       `json:"id"`
					Name      string    `json:"name"`
					StartDate time.Time `json:"startDate"`
					EndDate   time.Time `json:"endDate"`
				}{ID: sp.ID, Name: sp.Name, StartDate: sp.StartDate, EndDate: sp.EndDate})
			}
			if len(sps) > 0 {
				success = true
			}
		}
		if len(all) == 0 {
			if !success && lastErr != nil {
				msg := lastErr.Error()
				if len(errs) > 0 {
					msg = strings.Join(errs, "; ")
				}
				http.Error(w, msg, http.StatusBadGateway)
				return
			}
			all = []struct {
				ID        int       `json:"id"`
				Name      string    `json:"name"`
				StartDate time.Time `json:"startDate"`
				EndDate   time.Time `json:"endDate"`
			}{} // force [] instead of null
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].EndDate.After(all[j].EndDate)
		})
		if len(all) > limit {
			all = all[:limit]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(all)
	})
}

func (h *apiHandler) historyList() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		entries := h.history.Latest(20)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})
}

func (h *apiHandler) historyItem() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/history/") {
			http.NotFound(w, r)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/history/")
		trimmed = strings.Trim(trimmed, "/")
		if trimmed == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(trimmed, "/")
		entryID := parts[0]
		entry, ok := h.history.Get(entryID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entry)
			return
		}
		switch parts[1] {
		case "search":
			h.handleHistorySearch(w, r, entry)
			return
		case "action":
			h.handleHistoryAction(w, r, entry)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})
}

func (h *apiHandler) handleHistorySearch(w http.ResponseWriter, r *http.Request, entry history.Entry) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	matches := make([]history.IssueSnapshot, 0)
	if query != "" {
		for _, iss := range entry.Issues {
			content := strings.ToLower(fmt.Sprintf("%s %s %s", iss.Key, iss.Title, iss.URL))
			if strings.Contains(content, query) {
				matches = append(matches, iss)
			}
		}
	}
	resp := struct {
		Entry   history.Entry           `json:"entry"`
		Matches []history.IssueSnapshot `json:"matches"`
	}{
		Entry:   entry,
		Matches: matches,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *apiHandler) handleHistoryAction(w http.ResponseWriter, r *http.Request, entry history.Entry) {
	if h.llm == nil {
		http.Error(w, "LLM not configured", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	contextText := buildFollowUpContext(entry)
	answer, err := h.llm.FollowUp(r.Context(), contextText, command)
	if err != nil {
		http.Error(w, fmt.Sprintf("llm: %v", err), http.StatusBadGateway)
		return
	}
	resp := struct {
		Result string `json:"result"`
	}{
		Result: answer,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func buildFollowUpContext(entry history.Entry) string {
	var b strings.Builder
	if entry.Query != "" {
		b.WriteString("Original query: ")
		b.WriteString(entry.Query)
		b.WriteString("\n")
	}
	if entry.JQL != "" {
		b.WriteString("Executed JQL: ")
		b.WriteString(entry.JQL)
		b.WriteString("\n")
	}
	if entry.Analysis != "" {
		b.WriteString("Analysis: ")
		b.WriteString(entry.Analysis)
		b.WriteString("\n")
	}
	if len(entry.Issues) > 0 {
		b.WriteString("Issues:\n")
		for i, iss := range entry.Issues {
			if i >= 6 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s: %s (%s)\n", iss.Key, iss.Title, iss.URL))
		}
	}
	if raw := findStepResult(entry.Steps, "Execute Jira search"); len(raw) > 0 {
		b.WriteString("Raw JSON:\n")
		b.WriteString(truncateString(string(raw), 2000))
		b.WriteString("\n")
	}
	return truncateString(b.String(), 4000)
}

func findStepResult(steps []history.Step, name string) json.RawMessage {
	for _, step := range steps {
		if step.Name == name && len(step.Result) > 0 {
			return step.Result
		}
	}
	return nil
}

func truncateString(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}

var titleDirective = regexp.MustCompile(`(?i)(?:название|названием|title)\s*[:\-]\s*(.+)`)
var titleTruncateKeywords = []string{"проанализ", "опис", "тест", "найд", "напиш"}

func (h *apiHandler) search() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req searchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		titleMatch, hasTitle := extractTitleFromQuery(req.Query)

		jql := strings.TrimSpace(req.JQL)
		if jql == "" {
			if hasTitle {
				jql = fmt.Sprintf(`summary ~ "\"%s\""`, escapeQuotes(titleMatch))
			} else {
				// Prefer LLM if configured.
				if h.llm != nil {
					if derived, err := h.llm.DeriveJQL(r.Context(), req.Query); err == nil && strings.TrimSpace(derived) != "" {
						jql = strings.TrimSpace(derived)
					}
				}
				if jql == "" {
					jql = deriveJQL(req.Query)
				}
			}
		}
		if jql == "" {
			http.Error(w, "empty jql", http.StatusBadRequest)
			return
		}

		max := req.MaxResults
		if max <= 0 || max > 300 {
			max = 300
		}

		intentWorklog := hasWorklogIntent(req.Query, jql)
		intentBug := hasBugIntent(req.Query, jql)
		intentSprint := strings.Contains(strings.ToLower(req.Query+" "+jql), "спринт") || strings.Contains(strings.ToLower(req.Query+" "+jql), "sprint")
		var sprintRange *dateRange
		if intentSprint || req.SprintID > 0 {
			if len(req.Projects) > 1 {
				http.Error(w, "для спринта выбери один проект", http.StatusBadRequest)
				return
			}
			// жёстко используем борд 209 для спринтов CE
			boardID := 209
			if boardID > 0 {
				if req.SprintID > 0 {
					if dr, err := h.fetchSprintByID(r.Context(), req.SprintID); err == nil {
						sprintRange = dr
					}
				} else {
					sprintNum := parseSprintNumber(req.Query + " " + jql)
					if sprintNum > 0 {
						if dr, err := h.fetchSprintByNumber(r.Context(), boardID, sprintNum); err == nil {
							sprintRange = dr
						}
					} else {
						if dr, err := h.fetchActiveSprintRange(r.Context(), boardID); err == nil {
							sprintRange = dr
						}
					}
				}
			}
			if sprintRange == nil {
				sprintRange = fallbackSprintRange(time.Now().UTC())
			}
		}
		if sprintRange != nil {
			jql = applySprintRange(jql, sprintRange)
		}
		allowedAuthors := req.Users

		// If user selected specific users and this is a worklog query, replace currentUser with explicit users
		if intentWorklog && len(req.Users) > 0 {
			jql = overrideWorklogAuthor(jql, req.Users)
			if intentBug {
				jql = overrideReporterClause(jql, req.Users)
			}
			// prevent adding assignee filter later
			if len(req.Projects) > 0 {
				jql = overrideProjectsClause(jql, req.Projects)
			}
			jql = applyFilters(jql, nil, nil)
		} else if intentBug {
			// For bugs we treat users as reporters.
			if len(req.Users) > 0 {
				jql = overrideReporterClause(jql, req.Users)
			}
			if len(req.Projects) > 0 {
				jql = overrideProjectsClause(jql, req.Projects)
			}
			jql = applyFilters(jql, nil, nil) // do not add assignee filter
		} else {
			if len(req.Projects) > 0 {
				jql = overrideProjectsClause(jql, req.Projects)
			}
			if len(req.Users) > 0 {
				jql = overrideAssigneeClause(jql, req.Users)
			}
			jql = applyFilters(jql, nil, req.Users)
		}

		jql = cleanJQL(jql)

		if req.DryRun {
			resp := searchResponse{
				JQL:      jql,
				Raw:      json.RawMessage(`[]`),
				History:  h.history.Latest(10),
				Executed: time.Now().UTC(),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		fields := ensureValidFields(req.Fields)
		if intentWorklog && !containsField(fields, "worklog") {
			fields = append(fields, "worklog")
		}

		raw, status, err := h.jira.Search(r.Context(), jql, max, fields)
		if err != nil {
			respondErrorWithBody(w, status, err, raw, jql)
			return
		}
		total := extractTotal(raw)
		links := extractIssueLinks(raw, h.jira.BaseURL())
		firstIssueKey := ""
		if len(links) > 0 {
			firstIssueKey = links[0].Key
		}
		var issueDetail json.RawMessage
		if hasTitle && firstIssueKey != "" {
			if detail, err := fetchIssueDetail(r.Context(), h.jira, firstIssueKey); err == nil {
				issueDetail = detail
			}
		}

		analysisText := ""
		if intentWorklog {
			if hours, err := sumWorklogHoursFullAcrossPages(r.Context(), h.jira, jql, allowedAuthors); err == nil {
				analysisText = fmt.Sprintf("Списано за текущий месяц: %.2f ч", hours)
			}
		}
		if req.Analysis && h.llm != nil && analysisText == "" {
			if summary, err := h.llm.Analyze(r.Context(), req.Query, jql, raw); err == nil {
				analysisText = summary
			}
		}

		// Persist history.
		steps := buildHistorySteps(jql, raw, total, links, analysisText, titleMatch, firstIssueKey, issueDetail)
		entry := history.Entry{
			ID:         history.NewID(),
			Query:      strings.TrimSpace(req.Query),
			JQL:        jql,
			CreatedAt:  time.Now().UTC(),
			MaxResults: max,
			Steps:      steps,
			Issues:     issueLinksToSnapshots(links),
			Analysis:   analysisText,
		}
		_ = h.history.Append(entry)

		resp := searchResponse{
			JQL:       jql,
			Raw:       raw,
			History:   h.history.Latest(10),
			Executed:  entry.CreatedAt,
			Analysis:  analysisText,
			Total:     total,
			Issues:    links,
			Steps:     steps,
			HistoryID: entry.ID,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "encode response", http.StatusInternalServerError)
		}
	})
}

func deriveJQL(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	lq := strings.ToLower(query)

	// Bugs/defects intent.
	if strings.Contains(lq, "ошиб") || strings.Contains(lq, "bug") || strings.Contains(lq, "баг") || strings.Contains(lq, "дефект") || strings.Contains(lq, "заведен") {
		// Здесь спринт ещё не определён; при обработке запроса он будет подставлен (search())
		return creationDateRangeClause(query, nil) + " AND issuetype = Bug AND reporter = currentUser()"
	}

	// Worklog/time-tracking intent.
	if strings.Contains(lq, "сколько времени") ||
		strings.Contains(lq, "затрек") ||
		strings.Contains(lq, "списал") ||
		strings.Contains(lq, "списан") ||
		strings.Contains(lq, "списанное") ||
		strings.Contains(lq, "затрачен") ||
		strings.Contains(lq, "worklog") ||
		strings.Contains(lq, "time spent") {
		return worklogDateRangeClause(query) + " AND worklogAuthor = currentUser()"
	}

	// Heuristic: if user already wrote "project =" or "assignee =" treat as JQL.
	if strings.Contains(lq, "project ") || strings.Contains(lq, "assignee ") || strings.Contains(lq, "status ") {
		return query
	}
	// fallback: simple text search in summary/description
	return `text ~ "` + escapeQuotes(query) + `"`
}

func buildHistorySteps(jql string, raw json.RawMessage, total int, links []issueLink, analysis string, titleSearch string, firstIssueKey string, issueDetail json.RawMessage) []history.Step {
	steps := []history.Step{
		{
			Name:        "Generate JQL",
			Description: buildJQLStepDescription(titleSearch),
			Status:      "completed",
			Result:      marshalStepResult(map[string]string{"jql": jql}),
		},
		{
			Name:        "Execute Jira search",
			Description: fmt.Sprintf("Fetched %d issues via Jira", total),
			Status:      "completed",
			Result:      marshalStepResult(raw),
		},
	}
	if analysis != "" {
		steps = append(steps, history.Step{
			Name:        "Analysis",
			Description: "Summary generated by worklog aggregation or the LLM",
			Status:      "completed",
			Result:      marshalStepResult(map[string]string{"analysis": analysis}),
		})
	}
	if len(issueDetail) > 0 {
		desc := "Собрал дополнительные детали по найденной задаче"
		if firstIssueKey != "" {
			desc = fmt.Sprintf("Собрал дополнительные детали по задаче %s", firstIssueKey)
		}
		steps = append(steps, history.Step{
			Name:        "Issue details",
			Description: desc,
			Status:      "completed",
			Result:      issueDetail,
		})
	}
	return steps
}

func marshalStepResult(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case json.RawMessage:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return json.RawMessage(data)
	}
}

func buildJQLStepDescription(titleSearch string) string {
	if titleSearch == "" {
		return "Derived from the user query and selected filters"
	}
	return fmt.Sprintf("Найти по названию: %s", titleSearch)
}

func extractTitleFromQuery(query string) (string, bool) {
	matches := titleDirective.FindStringSubmatch(query)
	if len(matches) < 2 {
		return "", false
	}
	candidate := strings.TrimSpace(matches[1])
	candidate = truncateBeforeKeywords(candidate, titleTruncateKeywords)
	candidate = strings.Trim(candidate, `"'⟨⟩“”`)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}
	return candidate, true
}

func truncateBeforeKeywords(text string, keywords []string) string {
	lower := strings.ToLower(text)
	cut := len(text)
	for _, kw := range keywords {
		idx := strings.Index(lower, strings.ToLower(kw))
		if idx >= 0 && idx < cut {
			cut = idx
		}
	}
	return strings.TrimSpace(text[:cut])
}

func fetchIssueDetail(ctx context.Context, client *jira.Client, key string) (json.RawMessage, error) {
	if key == "" {
		return nil, errors.New("empty issue key")
	}
	body, err := client.Get(ctx, fmt.Sprintf("/rest/api/2/issue/%s?fields=summary,description,status,issuetype", key))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func issueLinksToSnapshots(links []issueLink) []history.IssueSnapshot {
	snapshots := make([]history.IssueSnapshot, 0, len(links))
	for _, link := range links {
		snapshots = append(snapshots, history.IssueSnapshot{
			Key:   link.Key,
			Title: link.Title,
			URL:   link.URL,
		})
	}
	return snapshots
}

func escapeQuotes(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func applyFilters(jql string, projects, users []string) string {
	var clauses []string
	base := normalizeClause(jql)
	if base != "" {
		clauses = append(clauses, base)
	}
	lower := strings.ToLower(jql)
	projects = ensureValidFields(projects)
	if len(projects) > 0 && !strings.Contains(lower, "project in") && !strings.Contains(lower, "project=") && !strings.Contains(lower, "project ") {
		clauses = append(clauses, "project in ("+quoteList(projects)+")")
	}
	users = ensureValidFields(users)
	// Avoid adding assignee filter if JQL already constrains assignee/worklogAuthor (to not break worklog queries).
	if len(users) > 0 && !strings.Contains(lower, "assignee") && !strings.Contains(lower, "worklogauthor") {
		clauses = append(clauses, "assignee in ("+quoteList(users)+")")
	}
	if len(clauses) == 0 {
		return jql
	}
	return strings.Join(clauses, " AND ")
}

func quoteList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, it := range items {
		quoted = append(quoted, `"`+escapeQuotes(it)+`"`)
	}
	return strings.Join(quoted, ",")
}

func hasWorklogIntent(query, jql string) bool {
	lq := strings.ToLower(query + " " + jql)
	return strings.Contains(lq, "worklog") ||
		strings.Contains(lq, "time spent") ||
		strings.Contains(lq, "сколько времени") ||
		strings.Contains(lq, "затрек") ||
		strings.Contains(lq, "списал") ||
		strings.Contains(lq, "списан") ||
		strings.Contains(lq, "списанное") ||
		strings.Contains(lq, "затрачен") ||
		strings.Contains(lq, "недел") ||
		strings.Contains(lq, "week")
}

func hasBugIntent(query, jql string) bool {
	lq := strings.ToLower(query + " " + jql)
	return strings.Contains(lq, "ошиб") ||
		strings.Contains(lq, "bug") ||
		strings.Contains(lq, "баг") ||
		strings.Contains(lq, "дефект") ||
		strings.Contains(lq, "заведен")
}

func worklogDateRangeClause(query string) string {
	lq := strings.ToLower(query)
	if strings.Contains(lq, "недел") || strings.Contains(lq, "week") || strings.Contains(lq, "спринт") || strings.Contains(lq, "sprint") {
		return "worklogDate >= startOfWeek() AND worklogDate <= endOfWeek()"
	}
	return "worklogDate >= startOfMonth() AND worklogDate <= endOfMonth()"
}

func creationDateRangeClause(query string, sprint *dateRange) string {
	if sprint != nil {
		return fmt.Sprintf("created >= \"%s\" AND created <= \"%s\"", sprint.Start.Format("2006-01-02"), sprint.End.Format("2006-01-02"))
	}
	lq := strings.ToLower(query)
	if strings.Contains(lq, "недел") || strings.Contains(lq, "week") || strings.Contains(lq, "спринт") || strings.Contains(lq, "sprint") {
		return "created >= startOfWeek() AND created <= endOfWeek()"
	}
	return "created >= startOfMonth() AND created <= endOfMonth()"
}

func containsField(fields []string, target string) bool {
	for _, f := range fields {
		if strings.EqualFold(strings.TrimSpace(f), target) {
			return true
		}
	}
	return false
}

func sumWorklogHoursFullAcrossPages(ctx context.Context, client *jira.Client, jql string, authors []string) (float64, error) {
	const pageSize = 50
	const hardLimit = 2000 // cap to avoid runaway; adjust if needed

	effectiveAuthors := ensureValidFields(authors)
	if len(effectiveAuthors) == 0 && client.User() != "" {
		effectiveAuthors = []string{client.User()}
	}
	startMonth, endMonth := monthRangeUTC(time.Now())

	var totalSeconds int
	startAt := 0
	for startAt < hardLimit {
		body, status, err := client.SearchWithPaging(ctx, jql, startAt, pageSize, []string{"worklog"})
		if err != nil {
			return 0, fmt.Errorf("search page: status %d: %w", status, err)
		}
		var payload struct {
			StartAt    int `json:"startAt"`
			MaxResults int `json:"maxResults"`
			Total      int `json:"total"`
			Issues     []struct {
				Key    string `json:"key"`
				Fields struct {
					Worklog struct {
						Worklogs   []worklogEntry `json:"worklogs"`
						Total      int            `json:"total"`
						MaxResults int            `json:"maxResults"`
						StartAt    int            `json:"startAt"`
					} `json:"worklog"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return 0, err
		}
		for _, issue := range payload.Issues {
			for _, wl := range issue.Fields.Worklog.Worklogs {
				if filterWorklogEntry(wl, effectiveAuthors, startMonth, endMonth) {
					totalSeconds += wl.TimeSpentSeconds
				}
			}
			if issue.Fields.Worklog.Total > len(issue.Fields.Worklog.Worklogs) {
				remaining, err := fetchIssueWorklogs(ctx, client, issue.Key)
				if err == nil {
					for _, r := range remaining {
						if filterWorklogEntry(r, effectiveAuthors, startMonth, endMonth) {
							totalSeconds += r.TimeSpentSeconds
						}
					}
				}
			}
		}
		startAt += payload.MaxResults
		if startAt >= payload.Total {
			break
		}
	}
	return float64(totalSeconds) / 3600.0, nil
}

type worklogEntry struct {
	TimeSpentSeconds int    `json:"timeSpentSeconds"`
	StartedRaw       string `json:"started"`
	Author           struct {
		Name string `json:"name"`
	} `json:"author"`
}

func fetchIssueWorklogs(ctx context.Context, client *jira.Client, issueKey string) ([]worklogEntry, error) {
	// Fetch up to 1000 per issue; adjust if needed.
	body, err := client.Get(ctx, "/rest/api/2/issue/"+issueKey+"/worklog?maxResults=1000")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Worklogs []worklogEntry `json:"worklogs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Worklogs, nil
}

func filterWorklog(w worklogEntry, user string, start, end time.Time) bool {
	return filterWorklogEntry(w, []string{user}, start, end)
}

func monthRangeUTC(now time.Time) (time.Time, time.Time) {
	y, m, _ := now.UTC().Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1).Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	return start, end
}

func filterWorklogEntry(w worklogEntry, users []string, start, end time.Time) bool {
	if len(users) > 0 {
		matched := false
		for _, u := range users {
			if strings.EqualFold(strings.TrimSpace(w.Author.Name), strings.TrimSpace(u)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	t, err := parseJiraTime(w.StartedRaw)
	if err != nil || t.IsZero() {
		return false
	}
	return !t.Before(start) && !t.After(end)
}

func parseJiraTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	// Jira returns like 2025-12-17T14:00:00.000+0000
	layouts := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
	}
	var lastErr error
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

var reWorklogAuthorClause = regexp.MustCompile(`(?i)(\s+(and|or)\s+)?worklogauthor\s+(in\s*\([^)]*\)|=\s*currentUser\(\))`)
var reProjectClause = regexp.MustCompile(`(?i)project\s+in\s*\([^)]*\)`)
var reReporterClause = regexp.MustCompile(`(?i)(\s+(and|or)\s+)?reporter\s+(in\s*\([^)]*\)|=\s*currentUser\(\))`)
var reAssigneeClause = regexp.MustCompile(`(?i)(\s+(and|or)\s+)?assignee\s+(in\s*\([^)]*\)|=\s*[^)\s]+|=\s*"[^"]+")`)

// overrideWorklogAuthor removes existing worklogAuthor clauses and sets to provided users.
func overrideWorklogAuthor(jql string, users []string) string {
	if len(users) == 0 {
		return jql
	}
	newClause := "worklogAuthor in (" + quoteList(users) + ")"
	// If a worklogAuthor clause exists, replace it.
	clean := reWorklogAuthorClause.ReplaceAllString(jql, "")
	clean = normalizeClause(clean)
	if clean == "" {
		return newClause
	}
	return clean + " AND " + newClause
}

func overrideProjectsClause(jql string, projects []string) string {
	if len(projects) == 0 {
		return jql
	}
	newClause := "project in (" + quoteList(projects) + ")"
	if reProjectClause.MatchString(jql) {
		return reProjectClause.ReplaceAllString(jql, newClause)
	}
	clean := normalizeClause(jql)
	if clean == "" {
		return newClause
	}
	return clean + " AND " + newClause
}

func overrideReporterClause(jql string, reporters []string) string {
	if len(reporters) == 0 {
		return jql
	}
	newClause := "reporter in (" + quoteList(reporters) + ")"
	clean := reReporterClause.ReplaceAllString(jql, "")
	clean = normalizeClause(clean)
	if clean == "" {
		return newClause
	}
	return clean + " AND " + newClause
}

func overrideAssigneeClause(jql string, assignees []string) string {
	if len(assignees) == 0 {
		return jql
	}
	newClause := "assignee in (" + quoteList(assignees) + ")"
	clean := reAssigneeClause.ReplaceAllString(jql, "")
	clean = normalizeClause(clean)
	if clean == "" {
		return newClause
	}
	return clean + " AND " + newClause
}

// boardForProject returns the first scrum board id for the project, if any.
func (h *apiHandler) boardForProject(ctx context.Context, projectKey string) (int, bool) {
	boards, status, err := h.jira.BoardsForProject(ctx, projectKey)
	if err != nil || status >= 400 || len(boards) == 0 {
		return 0, false
	}
	return boards[0].ID, true
}

func trimLeadingLogical(s string) string {
	s = strings.TrimSpace(s)
	for {
		ls := strings.ToLower(s)
		switch {
		case strings.HasPrefix(ls, "and "):
			s = strings.TrimSpace(s[3:])
		case ls == "and":
			return ""
		case strings.HasPrefix(ls, "or "):
			s = strings.TrimSpace(s[2:])
		case ls == "or":
			return ""
		default:
			return s
		}
	}
}

func trimTrailingLogical(s string) string {
	s = strings.TrimSpace(s)
	for {
		ls := strings.ToLower(s)
		switch {
		case strings.HasSuffix(ls, " and"):
			s = strings.TrimSpace(s[:len(s)-3])
		case strings.HasSuffix(ls, " or"):
			s = strings.TrimSpace(s[:len(s)-2])
		default:
			return s
		}
	}
}

func normalizeClause(s string) string {
	s = trimLeadingLogical(s)
	s = trimTrailingLogical(s)
	return strings.TrimSpace(s)
}

func extractTotal(raw json.RawMessage) int {
	var tmp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(raw, &tmp); err == nil {
		return tmp.Total
	}
	return 0
}

type issueLink struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func extractIssueLinks(raw json.RawMessage, base string) []issueLink {
	// Try to unmarshal full issues. If fails, return nil.
	var tmp struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(raw, &tmp); err != nil {
		return nil
	}
	out := make([]issueLink, 0, len(tmp.Issues))
	for _, iss := range tmp.Issues {
		url := strings.TrimRight(base, "/") + "/browse/" + iss.Key
		out = append(out, issueLink{
			Key:   iss.Key,
			Title: iss.Fields.Summary,
			URL:   url,
		})
	}
	return out
}

type dateRange struct {
	Start time.Time
	End   time.Time
}

func (h *apiHandler) fetchActiveSprintRange(ctx context.Context, boardID int) (*dateRange, error) {
	sprints, status, err := h.jira.ActiveSprints(ctx, boardID)
	if err != nil {
		return nil, fmt.Errorf("active sprints status %d: %w", status, err)
	}
	if len(sprints) == 0 {
		return nil, fmt.Errorf("no active sprints")
	}
	// Assume first active sprint is current.
	sp := sprints[0]
	if sp.StartDate.IsZero() || sp.EndDate.IsZero() {
		return nil, fmt.Errorf("sprint has no dates")
	}
	return &dateRange{Start: sp.StartDate, End: sp.EndDate}, nil
}

func fallbackSprintRange(now time.Time) *dateRange {
	// Спринт: неделя четверг–среда. Берём последнюю прошедшую четверг 00:00 UTC как начало.
	wd := int(now.Weekday()) // Sunday=0
	thu := int(time.Thursday)
	diff := (wd - thu + 7) % 7
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -diff)
	end := start.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	return &dateRange{Start: start, End: end}
}

func applySprintRange(jql string, dr *dateRange) string {
	if dr == nil {
		return jql
	}
	start := dr.Start.Format("2006-01-02")
	end := dr.End.Format("2006-01-02")
	replaced := false
	// убираем заранее дублирующиеся спринтовые диапазоны worklogDate
	jql = stripSprintRange(jql, "worklogDate")
	jql = stripSprintRange(jql, "created")
	repls := []struct {
		old string
		new string
	}{
		{"created >= startOfWeek() AND created <= endOfWeek()", fmt.Sprintf("created >= \"%s\" AND created <= \"%s\"", start, end)},
		{"created >= startOfMonth() AND created <= endOfMonth()", fmt.Sprintf("created >= \"%s\" AND created <= \"%s\"", start, end)},
		{"worklogDate >= startOfWeek() AND worklogDate <= endOfWeek()", fmt.Sprintf("worklogDate >= \"%s\" AND worklogDate <= \"%s\"", start, end)},
		{"worklogDate >= startOfMonth() AND worklogDate <= endOfMonth()", fmt.Sprintf("worklogDate >= \"%s\" AND worklogDate <= \"%s\"", start, end)},
	}
	for _, r := range repls {
		if strings.Contains(jql, r.old) {
			jql = strings.ReplaceAll(jql, r.old, r.new)
			replaced = true
		}
	}
	if !replaced {
		switch {
		case strings.Contains(strings.ToLower(jql), "worklogauthor"):
			jql = jql + fmt.Sprintf(" AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", start, end)
		case strings.Contains(strings.ToLower(jql), "issuetype = bug"):
			jql = jql + fmt.Sprintf(" AND created >= \"%s\" AND created <= \"%s\"", start, end)
		default:
			// fallback: append created range
			jql = jql + fmt.Sprintf(" AND created >= \"%s\" AND created <= \"%s\"", start, end)
		}
	}
	return jql
}

func stripSprintRange(jql, field string) string {
	pat := fmt.Sprintf(`(\s+(and|or)\s+)?%s\s*>=\s*"[0-9-]+"\s*AND\s*%s\s*<=\s*"[0-9-]+"`, field, field)
	re := regexp.MustCompile(pat)
	return re.ReplaceAllString(jql, "")
}

func cleanJQL(jql string) string {
	jql = strings.TrimSpace(jql)
	// remove duplicate logicals
	reDup := regexp.MustCompile(`\b(AND|OR)\s+(AND|OR)\b`)
	for reDup.MatchString(jql) {
		jql = reDup.ReplaceAllString(jql, "$2")
	}
	// collapse multiple spaces
	reSpace := regexp.MustCompile(`\s+`)
	jql = reSpace.ReplaceAllString(jql, " ")
	// trim leading logicals
	jql = trimLeadingLogical(jql)
	return strings.TrimSpace(jql)
}

func parseSprintNumber(text string) int {
	re := regexp.MustCompile(`(?i)(спринт|sprint)\s*([0-9]+)|([0-9]+)\s*(спринт|sprint)`)
	m := re.FindStringSubmatch(text)
	if len(m) == 0 {
		return 0
	}
	if m[2] != "" {
		n, _ := strconv.Atoi(m[2])
		return n
	}
	if m[3] != "" {
		n, _ := strconv.Atoi(m[3])
		return n
	}
	return 0
}

// fetchSprintByNumber finds a sprint by numeric hint (id or number in the name) across active/future/closed.
func (h *apiHandler) fetchSprintByNumber(ctx context.Context, boardID, num int) (*dateRange, error) {
	states := []string{"active", "future", "closed"}
	for _, st := range states {
		sprints, status, err := h.jira.ListSprints(ctx, boardID, st, 200)
		if err != nil || status >= 400 {
			continue
		}
		for _, sp := range sprints {
			if sp.ID == num || strings.Contains(strings.ToLower(sp.Name), fmt.Sprintf("%d", num)) {
				if sp.StartDate.IsZero() || sp.EndDate.IsZero() {
					continue
				}
				return &dateRange{Start: sp.StartDate, End: sp.EndDate}, nil
			}
		}
	}
	return nil, fmt.Errorf("sprint %d not found", num)
}

func (h *apiHandler) fetchSprintByID(ctx context.Context, sprintID int) (*dateRange, error) {
	sp, status, err := h.jira.GetSprint(ctx, sprintID)
	if err != nil {
		return nil, fmt.Errorf("get sprint %d status %d: %w", sprintID, status, err)
	}
	if sp.StartDate.IsZero() || sp.EndDate.IsZero() {
		return nil, fmt.Errorf("sprint has no dates")
	}
	return &dateRange{Start: sp.StartDate, End: sp.EndDate}, nil
}

// filterProjects removes projects by key.
func filterProjects(raw []struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}) []struct {
	Key  string `json:"key"`
	Name string `json:"name"`
} {
	block := map[string]struct{}{
		"AMP":   {},
		"CONE":  {},
		"COR":   {},
		"CRED":  {},
		"DEEP":  {},
		"TP":    {},
		"IC":    {},
		"SEC":   {},
		"MS":    {},
		"QAD":   {},
		"SEN":   {},
		"SIMTW": {},
		"TDS":   {},
		"WU":    {},
	}
	out := make([]struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}, 0, len(raw))
	for _, p := range raw {
		if _, banned := block[p.Key]; banned {
			continue
		}
		out = append(out, p)
	}
	return out
}

// helper for optional int query params (not used yet but handy for future endpoints).
func intFromQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

// respondError allows consistent error formatting (reserve for future richer errors).
func respondError(w http.ResponseWriter, status int, err error, jql string) {
	type errorResp struct {
		Error string `json:"error"`
		JQL   string `json:"jql,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResp{Error: err.Error(), JQL: jql})
}

func respondErrorWithBody(w http.ResponseWriter, status int, err error, body []byte, jql string) {
	msg := err.Error()
	if len(body) > 0 {
		msg = fmt.Sprintf("%s: %s", msg, trimBody(body, 400))
	}
	respondError(w, status, errors.New(msg), jql)
}

func trimBody(b []byte, max int) string {
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// ensureValidFields ensures no empty fields are sent to Jira.
func ensureValidFields(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// normalizeError converts Jira HTTP code to go error for logging (not used externally).
var errUnauthorized = errors.New("jira unauthorized")
