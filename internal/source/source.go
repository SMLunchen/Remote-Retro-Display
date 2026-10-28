// Package source definiert die anzeigbaren Quellen (net, image, stream,
// serial:<port>, test) und deren Registry.
package source

import (
	"sort"
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/fonts"
	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

// FontProvider liefert die aktuell konfigurierte Schrift samt Skalierung.
type FontProvider func() (*fonts.Font, int)

type Source interface {
	ID() string
	Title() string
	// Render zeichnet den aktuellen Zustand vollflächig auf dst.
	Render(dst *surface.Surface)
	// Dirty meldet Änderungen seit dem letzten Aufruf und setzt das Flag zurück.
	Dirty() bool
}

type Registry struct {
	mu sync.RWMutex
	m  map[string]Source
}

func NewRegistry() *Registry {
	return &Registry{m: map[string]Source{}}
}

func (r *Registry) Add(s Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[s.ID()] = s
}

func (r *Registry) Get(id string) Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[id]
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

func (r *Registry) List() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.m))
	for id := range r.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Source, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.m[id])
	}
	return out
}
