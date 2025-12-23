package history

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is a single search attempt.
type Entry struct {
	ID         string          `json:"id"`
	Query      string          `json:"query"`
	JQL        string          `json:"jql"`
	MaxResults int             `json:"maxResults"`
	Steps      []Step          `json:"steps,omitempty"`
	Issues     []IssueSnapshot `json:"issues,omitempty"`
	Analysis   string          `json:"analysis,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// Step represents an individual phase (JQL derivation, query, summary).
type Step struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// IssueSnapshot keeps minimal info for follow-ups.
type IssueSnapshot struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Store persists history to a JSON file with a simple append-then-trim strategy.
type Store struct {
	path string
	mu   sync.Mutex
	list []Entry
}

func NewStore(path string) *Store {
	s := &Store{path: path}
	_ = s.load()
	return s
}

// Append adds an entry and saves to disk. Keeps only the latest 100 entries.
func (s *Store) Append(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.list = append(s.list, e)
	if len(s.list) > 100 {
		s.list = s.list[len(s.list)-100:]
	}
	return s.save()
}

// Latest returns up to n most recent entries (newest first).
func (s *Store) Latest(n int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	if n <= 0 {
		n = len(s.list)
	}
	if n > len(s.list) {
		n = len(s.list)
	}
	out := make([]Entry, 0, n)
	for i := len(s.list) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.list[i])
	}
	return out
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		// ignore missing file
		return nil
	}
	return json.Unmarshal(data, &s.list)
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Get returns entry by ID.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.list) - 1; i >= 0; i-- {
		if s.list[i].ID == id {
			return s.list[i], true
		}
	}
	return Entry{}, false
}

// NewID produces pseudo-unique ID.
func NewID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.999999999"), ".", "")
	}
	return hex.EncodeToString(buf[:])
}
