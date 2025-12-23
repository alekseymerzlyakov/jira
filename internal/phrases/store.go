package phrases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Phrase struct {
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
	list []Phrase
}

func NewStore(path string) *Store {
	s := &Store{path: path}
	_ = s.load()
	return s
}

func (s *Store) List() []Phrase {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Phrase, len(s.list))
	copy(out, s.list)
	return out
}

// Replace overwrites the store with provided phrases (dedup by Text + trim empties).
func (s *Store) Replace(list []Phrase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uniq := make([]Phrase, 0, len(list))
	seen := map[string]struct{}{}
	for _, v := range list {
		v.Text = trim(v.Text)
		v.Description = trim(v.Description)
		if v.Text == "" {
			continue
		}
		if _, ok := seen[v.Text]; ok {
			continue
		}
		seen[v.Text] = struct{}{}
		uniq = append(uniq, v)
	}
	s.list = uniq
	return s.save()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil // ignore missing
	}
	// New format: []Phrase
	var phrases []Phrase
	if err := json.Unmarshal(data, &phrases); err == nil {
		s.list = phrases
		return nil
	}
	// Legacy format: []string
	var legacy []string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil
	}
	out := make([]Phrase, 0, len(legacy))
	for _, t := range legacy {
		t = trim(t)
		if t == "" {
			continue
		}
		out = append(out, Phrase{Text: t})
	}
	s.list = out
	_ = s.save()
	return nil
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

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\n' || last == '\t' || last == '\r' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}
