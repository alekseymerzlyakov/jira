## Stepwise Request Workflow Design

Goal: turn a single natural-language instruction into a sequence of concrete tasks, execute them in order, surface intermediate results in the UI, and keep the final output/queryable history.

### 1. Request decomposition
- LLM receives the incoming query and returns:
  1. A ranked list of discrete steps (e.g. “derive JQL”, “fetch matching issues”, “summarize descriptions”, “describe test cases”).
  2. Optional metadata for each step (required API call, expected artifact type, related history entry).
- Backend stores these steps along with the original query/derived JQL in an enriched history record.
- Frontend renders the steps as collapsible cards that show the planned work before execution.

### 2. Step execution pipeline
- Execute steps sequentially:
  1. Generate/preview JQL.
  2. Run Jira search.
  3. For each issue, collect description/comments; optionally call LLM for analysis.
  4. Derive advice/test cases summarizing result set.
- Each completed step pushes its result (JQL text, issue list, summary text, worklog totals) to the response payload.
- The payload references the `historyId` so later requests can refer to this work product.

### 3. History + contextual follow-up
- History entries keep structured fields:
  - `id`, `query`, `jql`, `steps`, `issues`, `analysis`, `createdAt`.
  - Each step can hold `status`, `result`, `llmResponse`.
- New endpoints allow:
  - Fetching stored answers (`GET /api/history/{id}`).
  - Narrowing down existing responses (`POST /api/history/{id}/query`).
  - Listing recent answers (already available via `history.json`).
- Frontend adds “reuse last response” UI, letting the user pick history records, ask follow-up questions (e.g. “find in step results where tag=bug”).

### 4. UI implications
- Show the “task list” card above the current response.
- Display each completed step’s result in place, with action buttons to re-run or dig deeper.
- Tag stored responses with short summaries (“analysis ready”, “contains bugs”, etc.) so the user can reference them by name later.

### Next
- Implement the data model and endpoints described, starting with request parsing/storage (this doc covers step 1 of the plan).

