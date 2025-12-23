package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	host   string
	user   string
	pass   string
	client *http.Client
}

func NewClient(host, user, pass string) *Client {
	return &Client{
		host:   trimTrailingSlash(host),
		user:   user,
		pass:   pass,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Myself(ctx context.Context) ([]byte, int, error) {
	return c.get(ctx, "/rest/api/2/myself")
}

func (c *Client) Search(ctx context.Context, jql string, maxResults int, fields []string) ([]byte, int, error) {
	return c.searchWith(ctx, jql, 0, maxResults, fields)
}

func (c *Client) SearchWithPaging(ctx context.Context, jql string, startAt, maxResults int, fields []string) ([]byte, int, error) {
	return c.searchWith(ctx, jql, startAt, maxResults, fields)
}

func (c *Client) User() string {
	return c.user
}

func (c *Client) BaseURL() string {
	return c.host
}

func (c *Client) ActiveSprints(ctx context.Context, boardID int) ([]Sprint, int, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=active", boardID)
	body, status, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, status, err
	}
	var resp struct {
		Values []Sprint `json:"values"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, status, err
	}
	return resp.Values, status, nil
}

func (c *Client) BoardsForProject(ctx context.Context, projectKey string) ([]Board, int, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/board?projectKeyOrId=%s&type=scrum", url.QueryEscape(projectKey))
	body, status, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, status, err
	}
	var resp struct {
		Values []Board `json:"values"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, status, err
	}
	return resp.Values, status, nil
}

type Sprint struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	StartDate time.Time `json:"startDate"`
	EndDate   time.Time `json:"endDate"`
}

type Board struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListSprints fetches sprints for a board with a given state (active, future, closed).
func (c *Client) ListSprints(ctx context.Context, boardID int, state string, max int) ([]Sprint, int, error) {
	if max <= 0 || max > 200 {
		max = 200
	}
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=%s&maxResults=%d", boardID, url.QueryEscape(state), max)
	body, status, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, status, fmt.Errorf("%w body=%s", err, string(body))
	}
	var resp struct {
		Values []Sprint `json:"values"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, status, err
	}
	return resp.Values, status, nil
}

// GetSprint returns a sprint by id.
func (c *Client) GetSprint(ctx context.Context, sprintID int) (*Sprint, int, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/sprint/%d", sprintID)
	body, status, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, status, err
	}
	var sp Sprint
	if err := json.Unmarshal(body, &sp); err != nil {
		return nil, status, err
	}
	return &sp, status, nil
}

func (c *Client) searchWith(ctx context.Context, jql string, startAt, maxResults int, fields []string) ([]byte, int, error) {
	payload := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"startAt":    startAt,
	}
	if len(fields) > 0 {
		payload["fields"] = fields
	}
	return c.post(ctx, "/rest/api/2/search", payload)
}

// Get performs a raw GET and returns body or error.
func (c *Client) Get(ctx context.Context, p string) ([]byte, error) {
	body, status, err := c.get(ctx, p)
	if err != nil {
		return body, fmt.Errorf("status %d: %w", status, err)
	}
	return body, nil
}

type Worklog struct {
	ID               string
	TimeSpentSeconds int
	Started          string
	Author           struct {
		Name string `json:"name"`
	} `json:"author"`
}

// ListWorklogs returns all worklog entries for an issue (paged).
func (c *Client) ListWorklogs(ctx context.Context, issueKey string) ([]Worklog, int, error) {
	const pageSize = 1000
	startAt := 0
	var out []Worklog
	var lastStatus int

	for {
		endpoint := fmt.Sprintf("/rest/api/2/issue/%s/worklog?startAt=%d&maxResults=%d", url.PathEscape(issueKey), startAt, pageSize)
		body, status, err := c.get(ctx, endpoint)
		lastStatus = status
		if err != nil {
			return nil, status, err
		}
		var resp struct {
			StartAt    int       `json:"startAt"`
			MaxResults int       `json:"maxResults"`
			Total      int       `json:"total"`
			Worklogs   []Worklog `json:"worklogs"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, status, err
		}
		out = append(out, resp.Worklogs...)
		startAt = resp.StartAt + resp.MaxResults
		if startAt >= resp.Total || resp.MaxResults == 0 {
			break
		}
	}
	return out, lastStatus, nil
}

// AddWorklog adds a worklog entry to an issue.
func (c *Client) AddWorklog(ctx context.Context, issueKey string, started time.Time, timeSpentSeconds int, comment string) ([]byte, int, error) {
	// Jira commonly expects: 2025-12-18T08:09:00.000+0200
	startedStr := started.Format("2006-01-02T15:04:05.000-0700")
	payload := map[string]any{
		"started":          startedStr,
		"timeSpentSeconds": timeSpentSeconds,
	}
	if comment != "" {
		payload["comment"] = comment
	}
	endpoint := fmt.Sprintf("/rest/api/2/issue/%s/worklog", url.PathEscape(issueKey))
	return c.post(ctx, endpoint, payload)
}

func ParseJiraTime(s string) (time.Time, error) {
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
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

func (c *Client) get(ctx context.Context, p string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(p), nil)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, fmt.Errorf("jira: %s", resp.Status)
	}
	return body, resp.StatusCode, nil
}

func (c *Client) post(ctx context.Context, p string, body any) ([]byte, int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(p), bytes.NewReader(buf))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, fmt.Errorf("jira: %s", resp.Status)
	}
	return respBody, resp.StatusCode, nil
}

func (c *Client) addAuth(req *http.Request) {
	token := base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.pass))
	req.Header.Set("Authorization", "Basic "+token)
	req.Header.Set("Accept", "application/json")
}

func (c *Client) url(p string) string {
	u, _ := url.Parse(c.host)
	if strings.Contains(p, "?") {
		parts := strings.SplitN(p, "?", 2)
		u.Path = path.Join(u.Path, parts[0])
		u.RawQuery = parts[1]
	} else {
		u.Path = path.Join(u.Path, p)
	}
	return u.String()
}

func trimTrailingSlash(s string) string {
	if s == "" {
		return s
	}
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
