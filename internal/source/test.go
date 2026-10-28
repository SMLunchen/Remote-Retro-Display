package source

import (
	"fmt"
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

// Test zeigt Testbilder zum Justieren des CRT (Geometrie, Helligkeit, Font).
type Test struct {
	mu      sync.Mutex
	pattern string
	dirty   bool
	fp      FontProvider
}

func NewTest(fp FontProvider) *Test {
	return &Test{pattern: "grid", dirty: true, fp: fp}
}

func (t *Test) ID() string { return "test" }

func (t *Test) Title() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return "Testbild (" + t.pattern + ")"
}

func (t *Test) Pattern() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pattern
}

func (t *Test) SetPattern(p string) error {
	switch p {
	case "grid", "bars", "font":
	default:
		return fmt.Errorf("unbekanntes Testbild %q (grid|bars|font)", p)
	}
	t.mu.Lock()
	t.pattern = p
	t.dirty = true
	t.mu.Unlock()
	return nil
}

func (t *Test) Dirty() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.dirty
	t.dirty = false
	return d
}

func (t *Test) Render(dst *surface.Surface) {
	t.mu.Lock()
	pattern := t.pattern
	t.mu.Unlock()
	dst.Clear(0)
	switch pattern {
	case "grid":
		t.renderGrid(dst)
	case "bars":
		t.renderBars(dst)
	case "font":
		t.renderFont(dst)
	}
}

func (t *Test) renderGrid(dst *surface.Surface) {
	w, h := dst.W, dst.H
	for x := 0; x < w; x += 32 {
		dst.VLine(x, 0, h, 1, 90)
	}
	for y := 0; y < h; y += 32 {
		dst.HLine(0, y, w, 1, 90)
	}
	dst.Rect(0, 0, w, h, 2, 255)
	// Mittenkreuz und Kreise für Linearität
	dst.HLine(0, h/2-1, w, 2, 255)
	dst.VLine(w/2-1, 0, h, 2, 255)
	r := h / 2
	dst.Circle(w/2, h/2, r-4, 2, 255)
	dst.Circle(w/2, h/2, r/2, 2, 180)
	// Eckmarkierungen
	m := 24
	for _, c := range [][2]int{{0, 0}, {w - m, 0}, {0, h - m}, {w - m, h - m}} {
		dst.Rect(c[0], c[1], m, m, 2, 255)
	}
	f, scale := t.fp()
	label := fmt.Sprintf("%dx%d", w, h)
	f.DrawString(dst, w/2-f.StringWidth(label, scale)/2, h/2+8, label, scale, 255, 0, true)
}

func (t *Test) renderBars(dst *surface.Surface) {
	w, h := dst.W, dst.H
	f, _ := t.fp()
	steps := 8
	bw := w / steps
	for i := 0; i < steps; i++ {
		v := byte(i * 255 / (steps - 1))
		dst.FillRect(i*bw, 0, bw, h/2, v)
		lbl := fmt.Sprintf("%d", v)
		fg := byte(255)
		if v > 128 {
			fg = 0
		}
		f.DrawString(dst, i*bw+4, 4, lbl, 1, fg, 0, false)
	}
	// kontinuierlicher Verlauf unten
	for x := 0; x < w; x++ {
		v := byte(x * 255 / max(w-1, 1))
		dst.VLine(x, h/2, h/2, 1, v)
	}
	dst.Rect(0, 0, w, h, 2, 255)
}

func (t *Test) renderFont(dst *surface.Surface) {
	f, _ := t.fp()
	fw, fh := f.Cell()
	y := 4
	specimen := "ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz 0123456789 äöüß .,;:!?#@/\\()[]{}"
	for scale := 1; scale <= 3; scale++ {
		label := fmt.Sprintf("%s %dx%d x%d", f.Name, fw, fh, scale)
		f.DrawString(dst, 4, y, label, scale, 255, 0, false)
		y += fh * scale
		f.DrawString(dst, 4, y, specimen, scale, 180, 0, false)
		y += fh*scale + 6
		if y > dst.H {
			break
		}
	}
}
