package llm

import "context"

// JQLGenerator generates JQL from a natural-language query.
type JQLGenerator interface {
	DeriveJQL(ctx context.Context, query string) (string, error)
}

// Analyzer produces a human summary/answer based on Jira search results and the original query.
type Analyzer interface {
	Analyze(ctx context.Context, userQuery string, jql string, rawJSON []byte) (string, error)
}
