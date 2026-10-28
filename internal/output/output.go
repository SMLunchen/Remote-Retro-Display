// Package output abstrahiert die physische Bildausgabe.
package output

import "github.com/SMLunchen/Remote-Retro-Display/internal/surface"

type Output interface {
	// Push gibt einen fertigen Frame aus.
	Push(s *surface.Surface) error
	// Size liefert die native Auflösung der Ausgabe.
	Size() (w, h int)
	Close() error
}
