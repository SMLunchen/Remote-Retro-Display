package config

import (
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/source"
)

// Store kapselt den threadsicheren Zugriff auf die laufende Konfiguration
// samt Persistierung.
type Store struct {
	mu   sync.Mutex
	c    *Config
	path string
}

func NewStore(c *Config, path string) *Store {
	return &Store{c: c, path: path}
}

// Snapshot liefert eine Kopie der aktuellen Konfiguration.
func (s *Store) Snapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *s.c
	cp.Layout.Viewports = append([]string{}, s.c.Layout.Viewports...)
	cp.Serial = append([]source.SerialConfig{}, s.c.Serial...)
	return cp
}

// Update wendet eine Änderung an und speichert die Datei.
func (s *Store) Update(apply func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply(s.c)
	return s.c.Save(s.path)
}
