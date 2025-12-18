package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Addr         string
	JiraHost     string
	JiraUser     string
	JiraPassword string
	WebDir       string
	DataDir      string
	OpenAIKey    string
	OpenAIModel  string
	BoardID      int
}

func Load() (Config, error) {
	cfg := Config{
		Addr:     env("ADDR", ":8080"),
		JiraHost: env("JIRA_HOST", ""),
		JiraUser: env("JIRA_USER", ""),
		JiraPassword: env("JIRA_PASSWORD", func() string {
			if v := os.Getenv("JIRA_PASS"); v != "" {
				return v
			}
			return ""
		}()),
		WebDir:      env("WEB_DIR", filepath.Join(".", "web")),
		DataDir:     env("DATA_DIR", filepath.Join(".", "data")),
		OpenAIKey:   env("OPENAI_API_KEY", ""),
		OpenAIModel: env("OPENAI_MODEL", "gpt-4o-mini"),
		BoardID:     intFromEnv("JIRA_BOARD_ID", 0),
	}

	if cfg.JiraHost == "" || cfg.JiraUser == "" || cfg.JiraPassword == "" {
		return Config{}, errors.New("JIRA_HOST, JIRA_USER, JIRA_PASSWORD are required")
	}
	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intFromEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func (c Config) String() string {
	return fmt.Sprintf("addr=%s jira=%s user=%s web=%s data=%s", c.Addr, c.JiraHost, c.JiraUser, c.WebDir, c.DataDir)
}
