// Package compositor setzt Quellen gemäß Layout (Vollbild/Split) zu Frames
// zusammen und verteilt sie an die Ausgaben.
package compositor

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/SMLunchen/Remote-Retro-Display/internal/config"
	"github.com/SMLunchen/Remote-Retro-Display/internal/output"
	"github.com/SMLunchen/Remote-Retro-Display/internal/source"
	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

const dividerGap = 6 // Pixel zwischen Viewports (inkl. 2 px Trennlinie)

type Compositor struct {
	mu sync.Mutex

	w, h   int
	insets config.Insets
	mode   string
	vps    []string
	maxFPS int

	reg *source.Registry
	fp  source.FontProvider

	frame   *surface.Surface
	vpSurfs []*surface.Surface
	version uint64
	force   bool
}

func New(w, h int, reg *source.Registry, fp source.FontProvider, cfg *config.Config) *Compositor {
	c := &Compositor{
		w: w, h: h,
		insets: cfg.Insets,
		mode:   cfg.Layout.Mode,
		vps:    append([]string{}, cfg.Layout.Viewports...),
		maxFPS: cfg.MaxFPS,
		reg:    reg,
		fp:     fp,
		frame:  surface.New(w, h),
	}
	c.force = true
	return c
}

func (c *Compositor) SetLayout(mode string, vps []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mode != "" {
		c.mode = mode
	}
	for i, id := range vps {
		if i < len(c.vps) && id != "" {
			c.vps[i] = id
		} else if i >= len(c.vps) && id != "" {
			c.vps = append(c.vps, id)
		}
	}
	c.force = true
}

func (c *Compositor) Layout() (string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode, append([]string{}, c.vps...)
}

func (c *Compositor) SetInsets(in config.Insets) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.insets = in
	c.force = true
}

func (c *Compositor) SetMaxFPS(fps int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fps >= 1 && fps <= 30 {
		c.maxFPS = fps
	}
}

// Invalidate erzwingt einen kompletten Neuaufbau des nächsten Frames.
func (c *Compositor) Invalidate() {
	c.mu.Lock()
	c.force = true
	c.mu.Unlock()
}

// Snapshot liefert eine Kopie des aktuellen Frames samt Versionszähler.
func (c *Compositor) Snapshot() (*surface.Surface, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frame.Copy(), c.version
}

func (c *Compositor) Version() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

// Run betreibt die Frame-Schleife, bis ctx beendet wird.
func (c *Compositor) Run(ctx context.Context, outs []output.Output) {
	for {
		c.mu.Lock()
		fps := c.maxFPS
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second / time.Duration(fps)):
		}
		if frame := c.tick(); frame != nil {
			for _, o := range outs {
				if err := o.Push(frame); err != nil {
					log.Printf("output: %v", err)
				}
			}
		}
	}
}

// tick prüft auf Änderungen und rendert bei Bedarf. Gibt den neuen Frame
// zurück oder nil, wenn nichts zu tun war.
func (c *Compositor) tick() *surface.Surface {
	c.mu.Lock()
	mode := c.mode
	vps := append([]string{}, c.vps...)
	need := c.force
	c.mu.Unlock()

	active := vps[:1]
	if mode != "single" {
		active = vps[:min(2, len(vps))]
	}
	for _, id := range active {
		if s := c.reg.Get(id); s != nil && s.Dirty() {
			need = true
		}
	}
	if !need {
		return nil
	}
	return c.render(mode, active)
}

func (c *Compositor) render(mode string, active []string) *surface.Surface {
	c.mu.Lock()
	in := c.insets
	c.mu.Unlock()

	frame := surface.New(c.w, c.h)
	cx, cy := in.Left, in.Top
	cw := max(c.w-in.Left-in.Right, 32)
	ch := max(c.h-in.Top-in.Bottom, 32)

	type vpRect struct{ x, y, w, h int }
	var rects []vpRect
	switch mode {
	case "hsplit":
		w0 := (cw - dividerGap) / 2
		rects = []vpRect{
			{cx, cy, w0, ch},
			{cx + w0 + dividerGap, cy, cw - w0 - dividerGap, ch},
		}
		frame.VLine(cx+w0+dividerGap/2-1, cy, ch, 2, 110)
	case "vsplit":
		h0 := (ch - dividerGap) / 2
		rects = []vpRect{
			{cx, cy, cw, h0},
			{cx, cy + h0 + dividerGap, cw, ch - h0 - dividerGap},
		}
		frame.HLine(cx, cy+h0+dividerGap/2-1, cw, 2, 110)
	default:
		rects = []vpRect{{cx, cy, cw, ch}}
	}

	c.mu.Lock()
	if len(c.vpSurfs) != len(rects) {
		c.vpSurfs = make([]*surface.Surface, len(rects))
	}
	c.mu.Unlock()

	for i, r := range rects {
		if i >= len(active) {
			break
		}
		c.mu.Lock()
		vs := c.vpSurfs[i]
		if vs == nil || vs.W != r.w || vs.H != r.h {
			vs = surface.New(r.w, r.h)
			c.vpSurfs[i] = vs
		}
		c.mu.Unlock()

		src := c.reg.Get(active[i])
		if src == nil {
			vs.Clear(0)
			f, scale := c.fp()
			msg := "keine Quelle: " + active[i]
			f.DrawString(vs, 4, 4, msg, scale, 120, 0, false)
		} else {
			src.Render(vs)
		}
		frame.Blit(vs, r.x, r.y)
	}

	c.mu.Lock()
	c.frame = frame
	c.version++
	c.force = false
	c.mu.Unlock()
	return frame
}
