package phrases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
	list []string
}

func NewStore(path string) *Store {
	s := &Store{path: path}
	_ = s.load()
	return s
}

func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}

// Replace overwrites the store with provided phrases (dedup + trim empties).
func (s *Store) Replace(list []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uniq := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, v := range list {
		v = trim(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
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
