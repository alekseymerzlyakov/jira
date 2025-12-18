package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

type OpenAI struct {
	client *openai.Client
	model  string
}

func NewOpenAI(apiKey, model string) *OpenAI {
	if apiKey == "" {
		return nil
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	c := openai.NewClient(apiKey)
	return &OpenAI{client: c, model: model}
}

func (o *OpenAI) DeriveJQL(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("empty query")
	}

	system := `You are a Jira JQL expert. Given a user request, output ONLY a JQL string, no prose.
Rules:
- Keep it concise and valid for Jira Server 7.12 (JQL 2.x API).
- Prefer fields: project, issuetype, status, assignee, reporter, summary, description, updated, created, priority, resolution, labels, worklogAuthor, worklogDate, timespent.
- When user talks about “мои задачи / я делал / assigned to me” use assignee = currentUser().
- When user asks about tasks they reported (“я создал/завел”) use reporter = currentUser().
- For “сколько времени списал я за этот месяц” use: worklogAuthor = currentUser() AND worklogDate >= startOfMonth() AND worklogDate <= endOfMonth().
- If nothing specific is given, search by text: text ~ "user query".
- Do not use functions unavailable in server 7.12 (avoid IN with empty).
- Never include quotes around field names.`

	user := fmt.Sprintf("User request: %s", query)

	resp, err := o.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: o.model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: system},
				{Role: openai.ChatMessageRoleUser, Content: user},
			},
			Temperature: 0.2,
			MaxTokens:   120,
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no choices")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	// Some models might wrap in code fences; strip them.
	out = strings.Trim(out, "`")
	if strings.HasPrefix(out, "SQL") || strings.HasPrefix(out, "JQL") {
		out = strings.TrimSpace(out[3:])
	}
	return out, nil
}

func (o *OpenAI) Analyze(ctx context.Context, userQuery string, jql string, rawJSON []byte) (string, error) {
	if len(rawJSON) == 0 {
		return "", errors.New("empty results")
	}
	system := `You are a Jira expert. Given:
- the original user request,
- the JQL that was executed,
- the raw Jira search JSON (issues array with fields),
Produce a concise answer in Russian with:
1) краткое резюме (1-3 предложения),
2) если спрашивали про время/лог worklog — покажи итоговое время (hours) суммарно,
3) перечисли ключи задач с короткими заголовками (5-10 задач максимум),
4) если данных мало, скажи об этом.
Формат: резюме, затем список задач.
Не выдумывай данных, опирайся только на JSON.`

	user := fmt.Sprintf("User request: %s\nExecuted JQL: %s\nJira raw JSON: %s", userQuery, jql, string(rawJSON))

	resp, err := o.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: o.model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: system},
				{Role: openai.ChatMessageRoleUser, Content: user},
			},
			Temperature: 0.2,
			MaxTokens:   400,
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
