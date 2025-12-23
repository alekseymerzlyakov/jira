package main

import (
	"log"
	"net/http"
	"strings"

	"path/filepath"
	"time"

	"github.com/alekseymerzlyakov/jira/internal/config"
	"github.com/alekseymerzlyakov/jira/internal/history"
	"github.com/alekseymerzlyakov/jira/internal/jira"
	"github.com/alekseymerzlyakov/jira/internal/llm"
	"github.com/alekseymerzlyakov/jira/internal/phrases"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	jiraClient := jira.NewClient(cfg.JiraHost, cfg.JiraUser, cfg.JiraPassword)
	historyStore := history.NewStore(filepath.Join(cfg.DataDir, "history.json"))
	phrasesStore := phrases.NewStore(filepath.Join(cfg.DataDir, "phrases.json"))
	llmClient := llm.NewOpenAI(cfg.OpenAIKey, cfg.OpenAIModel)

	mux := http.NewServeMux()
	api := &apiHandler{
		jira:         jiraClient,
		history:      historyStore,
		phrasesStore: phrasesStore,
		llm:          llmClient,
		boardID:      cfg.BoardID,
	}
	mux.Handle("/api/health", api.health())
	mux.Handle("/api/myself", api.myself())
	mux.Handle("/api/projects", api.projects())
	mux.Handle("/api/search", api.search())
	mux.Handle("/api/phrases", api.phrases())
	mux.Handle("/api/worklog/command", api.worklogCommand())
	mux.Handle("/api/worklog/autofill", api.worklogAutofill())
	mux.Handle("/api/projects/", api.projectSprints())
	mux.Handle("/api/history", api.historyList())
	mux.Handle("/api/history/", api.historyItem())

	// Static files from web directory.
	fs := http.FileServer(http.Dir(cfg.WebDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent stale frontend JS/CSS during development.
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		fs.ServeHTTP(w, r)
	}))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

type apiHandler struct {
	jira         *jira.Client
	history      *history.Store
	phrasesStore *phrases.Store
	llm          *llm.OpenAI
	boardID      int
}
