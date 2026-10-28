//go:build !linux

package output

import (
	"fmt"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

type Framebuffer struct{}

func OpenFramebuffer(dev string) (*Framebuffer, error) {
	return nil, fmt.Errorf("Framebuffer-Ausgabe gibt es nur unter Linux (Dev-Modus: Web-Vorschau nutzen)")
}

func (fb *Framebuffer) Size() (int, int)                { return 0, 0 }
func (fb *Framebuffer) Push(s *surface.Surface) error   { return nil }
func (fb *Framebuffer) Close() error                    { return nil }
