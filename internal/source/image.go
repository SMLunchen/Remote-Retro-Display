package source

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

// Image zeigt das zuletzt gepushte Vollbild (Protokoll-Ebene 0).
type Image struct {
	mu     sync.Mutex
	orig   *surface.Surface // Graustufen in Originalgröße
	scaled *surface.Surface // Cache für die letzte Zielgröße
	dirty  bool
	fp     FontProvider
}

func NewImage(fp FontProvider) *Image {
	return &Image{fp: fp}
}

func (im *Image) ID() string { return "image" }

func (im *Image) Title() string {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.orig == nil {
		return "Bild (leer)"
	}
	return fmt.Sprintf("Bild %dx%d", im.orig.W, im.orig.H)
}

// SetImage dekodiert PNG/JPEG/GIF und übernimmt es als Graustufenbild.
func (im *Image) SetImage(data []byte) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("Bild nicht dekodierbar: %v", err)
	}
	b := img.Bounds()
	s := surface.New(b.Dx(), b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// Luma (Rec. 601)
			s.Pix[y*s.W+x] = byte((299*r + 587*g + 114*bl) / 1000 >> 8)
		}
	}
	im.mu.Lock()
	im.orig = s
	im.scaled = nil
	im.dirty = true
	im.mu.Unlock()
	return nil
}

func (im *Image) Dirty() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	d := im.dirty
	im.dirty = false
	return d
}

func (im *Image) Render(dst *surface.Surface) {
	im.mu.Lock()
	defer im.mu.Unlock()
	dst.Clear(0)
	if im.orig == nil {
		f, scale := im.fp()
		msg := "kein Bild empfangen (POST /api/image)"
		f.DrawString(dst, dst.W/2-f.StringWidth(msg, scale)/2, dst.H/2, msg, scale, 120, 0, false)
		return
	}
	// proportional einpassen
	dw, dh := dst.W, dst.H
	sw, sh := im.orig.W, im.orig.H
	w, h := dw, sh*dw/sw
	if h > dh {
		h = dh
		w = sw * dh / sh
	}
	if im.scaled == nil || im.scaled.W != w || im.scaled.H != h {
		sc := surface.New(w, h)
		sc.BlitScaled(im.orig, 0, 0, w, h)
		im.scaled = sc
	}
	dst.Blit(im.scaled, (dw-w)/2, (dh-h)/2)
}
