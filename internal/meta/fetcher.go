package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alekseymerzlyakov/jira/internal/jira"
)

// Fetcher pulls reference data from Jira and stores as json files.
type Fetcher struct {
	Jira   *jira.Client
	OutDir string
}

func (f Fetcher) FetchAll(ctx context.Context) error {
	if err := os.MkdirAll(f.OutDir, 0o755); err != nil {
		return err
	}

	steps := []struct {
		name string
		call func(context.Context) ([]byte, error)
	}{
		{"jira_fields", func(ctx context.Context) ([]byte, error) { return f.Jira.Get(ctx, "/rest/api/2/field") }},
		{"jira_projects", func(ctx context.Context) ([]byte, error) { return f.Jira.Get(ctx, "/rest/api/2/project") }},
		{"jira_statuses", func(ctx context.Context) ([]byte, error) { return f.Jira.Get(ctx, "/rest/api/2/status") }},
		{"jira_issue_types", func(ctx context.Context) ([]byte, error) { return f.Jira.Get(ctx, "/rest/api/2/issuetype") }},
		{"jira_priorities", func(ctx context.Context) ([]byte, error) { return f.Jira.Get(ctx, "/rest/api/2/priority") }},
	}

	type logEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		At   string `json:"fetchedAt"`
		Err  string `json:"error,omitempty"`
	}
	var summary []logEntry

	for _, step := range steps {
		data, err := step.call(ctx)
		entry := logEntry{Name: step.name, At: time.Now().UTC().Format(time.RFC3339)}
		if err != nil {
			entry.Err = err.Error()
			summary = append(summary, entry)
			continue
		}
		outPath := filepath.Join(f.OutDir, step.name+".json")
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			entry.Err = err.Error()
		} else {
			entry.Path = outPath
		}
		summary = append(summary, entry)
	}

	// Projects may be needed for versions; try per project if available.
	versionsErr := f.fetchVersions(ctx)
	if versionsErr != nil {
		summary = append(summary, logEntry{
			Name: "jira_versions",
			At:   time.Now().UTC().Format(time.RFC3339),
			Err:  versionsErr.Error(),
		})
	} else {
		summary = append(summary, logEntry{
			Name: "jira_versions",
			At:   time.Now().UTC().Format(time.RFC3339),
			Path: filepath.Join(f.OutDir, "jira_versions.json"),
		})
	}

	// Boards/sprints (agile) can be helpful; ignore errors silently.
	if boards, err := f.Jira.Get(ctx, "/rest/agile/1.0/board"); err == nil {
		_ = os.WriteFile(filepath.Join(f.OutDir, "jira_boards.json"), boards, 0o644)
	}

	summaryPath := filepath.Join(f.OutDir, "summary.json")
	if data, err := json.MarshalIndent(summary, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, data, 0o644)
	}

	for _, e := range summary {
		if e.Err != "" {
			return fmt.Errorf("some fetches failed, see %s", summaryPath)
		}
	}
	return nil
}

func (f Fetcher) fetchVersions(ctx context.Context) error {
	projectsRaw, err := f.Jira.Get(ctx, "/rest/api/2/project")
	if err != nil {
		return err
	}
	var projects []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return fmt.Errorf("parse projects: %w", err)
	}
	versions := make(map[string]json.RawMessage)
	for _, p := range projects {
		if p.ID == "" {
			continue
		}
		body, err := f.Jira.Get(ctx, "/rest/api/2/project/"+p.ID+"/versions")
		if err != nil {
			continue // skip on error
		}
		versions[p.ID] = body
	}
	out, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.OutDir, "jira_versions.json"), out, 0o644)
}
