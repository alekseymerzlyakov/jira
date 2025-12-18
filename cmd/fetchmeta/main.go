package main

import (
	"context"
	"log"
	"time"

	"github.com/alekseymerzlyakov/jira/internal/config"
	"github.com/alekseymerzlyakov/jira/internal/jira"
	"github.com/alekseymerzlyakov/jira/internal/meta"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	client := jira.NewClient(cfg.JiraHost, cfg.JiraUser, cfg.JiraPassword)
	f := meta.Fetcher{
		Jira:   client,
		OutDir: cfg.DataDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := f.FetchAll(ctx); err != nil {
		log.Fatalf("fetch meta: %v", err)
	}
	log.Printf("metadata fetched into %s", cfg.DataDir)
}
