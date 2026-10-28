// Package fonts lädt Bitmap-Schriften: ein eingebauter 7x13-Fallback sowie
// PSF/PSF2-Konsolenfonts (auch .gz), wie sie unter Linux in
// /usr/share/consolefonts liegen (z. B. Terminus).
package fonts

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

const BuiltinName = "builtin-7x13"

// Font ist eine monospaced Bitmap-Schrift. Glyphen sind bitgepackte Zeilen
// (MSB zuerst), rowBytes = (W+7)/8.
type Font struct {
	Name     string
	W, H     int
	rowBytes int
	glyphs   map[rune][]byte
}

func (f *Font) Cell() (w, h int) { return f.W, f.H }

// Draw zeichnet eine Glyphe mit ganzzahliger Skalierung. Bei opaque wird der
// Zellenhintergrund mit bg gefüllt, sonst nur gesetzte Pixel gezeichnet.
func (f *Font) Draw(dst *surface.Surface, x, y int, r rune, scale int, fg, bg byte, opaque bool) {
	if scale < 1 {
		scale = 1
	}
	if opaque {
		dst.FillRect(x, y, f.W*scale, f.H*scale, bg)
	}
	g, ok := f.glyphs[r]
	if !ok {
		g, ok = f.glyphs['?']
		if !ok {
			return
		}
	}
	for gy := 0; gy < f.H; gy++ {
		row := g[gy*f.rowBytes : (gy+1)*f.rowBytes]
		for gx := 0; gx < f.W; gx++ {
			if row[gx/8]&(0x80>>(gx%8)) != 0 {
				dst.FillRect(x+gx*scale, y+gy*scale, scale, scale, fg)
			}
		}
	}
}

func (f *Font) DrawString(dst *surface.Surface, x, y int, s string, scale int, fg, bg byte, opaque bool) {
	if scale < 1 {
		scale = 1
	}
	cx := x
	for _, r := range s {
		if r == '\n' {
			cx = x
			y += f.H * scale
			continue
		}
		f.Draw(dst, cx, y, r, scale, fg, bg, opaque)
		cx += f.W * scale
	}
}

// StringWidth liefert die Pixelbreite eines einzeiligen Strings.
func (f *Font) StringWidth(s string, scale int) int {
	if scale < 1 {
		scale = 1
	}
	return utf8.RuneCountInString(s) * f.W * scale
}

// Builtin rastert den 7x13-Font aus golang.org/x/image in das interne Format.
func Builtin() *Font {
	face := basicfont.Face7x13
	m := face.Metrics()
	ascent := m.Ascent.Ceil()
	h := ascent + m.Descent.Ceil()
	adv, _ := face.GlyphAdvance('M')
	w := adv.Ceil()
	f := &Font{Name: BuiltinName, W: w, H: h, rowBytes: (w + 7) / 8, glyphs: map[rune][]byte{}}
	rasterize := func(r rune) {
		dr, mask, maskp, _, ok := face.Glyph(fixed.P(0, ascent), r)
		if !ok {
			return
		}
		g := make([]byte, f.rowBytes*h)
		for py := dr.Min.Y; py < dr.Max.Y; py++ {
			for px := dr.Min.X; px < dr.Max.X; px++ {
				if py < 0 || py >= h || px < 0 || px >= w {
					continue
				}
				a := color.AlphaModel.Convert(mask.At(maskp.X+px-dr.Min.X, maskp.Y+py-dr.Min.Y)).(color.Alpha).A
				if a > 127 {
					g[py*f.rowBytes+px/8] |= 0x80 >> (px % 8)
				}
			}
		}
		f.glyphs[r] = g
	}
	for r := rune(32); r < 127; r++ {
		rasterize(r)
	}
	f.synthesizeGerman()
	return f
}

// synthesizeGerman ergänzt fehlende deutsche Sonderzeichen aus Basisglyphen
// (der eingebaute Face kennt nur ASCII). PSF-Fonts bringen echte Glyphen mit.
func (f *Font) synthesizeGerman() {
	withDots := func(target, base rune) {
		if _, exists := f.glyphs[target]; exists {
			return
		}
		g0, ok := f.glyphs[base]
		if !ok {
			return
		}
		g := make([]byte, len(g0))
		copy(g, g0)
		top := 0
		for row := 0; row < f.H; row++ {
			used := false
			for bx := 0; bx < f.rowBytes; bx++ {
				if g[row*f.rowBytes+bx] != 0 {
					used = true
					break
				}
			}
			if used {
				top = row
				break
			}
		}
		dotRow := top - 2
		if dotRow < 0 {
			dotRow = 0
		}
		for _, col := range []int{1, 4} {
			if col < f.W {
				g[dotRow*f.rowBytes+col/8] |= 0x80 >> (col % 8)
			}
		}
		f.glyphs[target] = g
	}
	alias := func(target, base rune) {
		if _, exists := f.glyphs[target]; exists {
			return
		}
		if g, ok := f.glyphs[base]; ok {
			f.glyphs[target] = g
		}
	}
	block := func(target rune, x0, y0, w, h int) {
		if _, exists := f.glyphs[target]; exists {
			return
		}
		g := make([]byte, f.rowBytes*f.H)
		for y := y0; y < y0+h && y < f.H; y++ {
			for x := x0; x < x0+w && x < f.W; x++ {
				g[y*f.rowBytes+x/8] |= 0x80 >> (x % 8)
			}
		}
		f.glyphs[target] = g
	}
	withDots('ä', 'a')
	withDots('ö', 'o')
	withDots('ü', 'u')
	withDots('Ä', 'A')
	withDots('Ö', 'O')
	withDots('Ü', 'U')
	alias('ß', 's')
	alias('µ', 'u')
	alias('€', 'E')
	alias('§', 'S')
	block('·', 2, f.H/2-1, 2, 2) // Ersatzzeichen für Nicht-Darstellbares
	block('°', 1, 1, 2, 2)       // Gradzeichen
}

var _ font.Face = basicfont.Face7x13 // Compile-Zeit-Check der API-Annahme

// LoadPSF lädt einen PSF1- oder PSF2-Font, transparent auch gzip-komprimiert.
func LoadPSF(path string) (*Font, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		raw, err = io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
	}
	name := filepath.Base(path)
	for _, suf := range []string{".gz", ".psfu", ".psf"} {
		name = strings.TrimSuffix(name, suf)
	}
	switch {
	case len(raw) > 4 && binary.LittleEndian.Uint32(raw) == 0x864ab572:
		return parsePSF2(name, raw)
	case len(raw) > 2 && raw[0] == 0x36 && raw[1] == 0x04:
		return parsePSF1(name, raw)
	default:
		return nil, fmt.Errorf("%s: kein PSF-Font", path)
	}
}

func parsePSF2(name string, b []byte) (*Font, error) {
	if len(b) < 32 {
		return nil, fmt.Errorf("PSF2-Header zu kurz")
	}
	headerSize := binary.LittleEndian.Uint32(b[8:])
	flags := binary.LittleEndian.Uint32(b[12:])
	length := int(binary.LittleEndian.Uint32(b[16:]))
	charSize := int(binary.LittleEndian.Uint32(b[20:]))
	height := int(binary.LittleEndian.Uint32(b[24:]))
	width := int(binary.LittleEndian.Uint32(b[28:]))
	rowBytes := (width + 7) / 8
	if charSize != rowBytes*height || width < 1 || height < 1 || length < 1 {
		return nil, fmt.Errorf("PSF2: unplausible Geometrie %dx%d", width, height)
	}
	glyphData := b[headerSize:]
	if len(glyphData) < length*charSize {
		return nil, fmt.Errorf("PSF2: Glyphendaten zu kurz")
	}
	f := &Font{Name: name, W: width, H: height, rowBytes: rowBytes, glyphs: map[rune][]byte{}}
	glyph := func(i int) []byte { return glyphData[i*charSize : (i+1)*charSize] }

	if flags&1 != 0 {
		// Unicode-Tabelle: pro Glyphe UTF-8-Runen, 0xFE leitet Sequenzen ein
		// (überspringen), 0xFF terminiert den Eintrag.
		tab := glyphData[length*charSize:]
		idx := 0
		for pos := 0; pos < len(tab) && idx < length; {
			switch tab[pos] {
			case 0xFF:
				idx++
				pos++
			case 0xFE:
				for pos < len(tab) && tab[pos] != 0xFF {
					pos++
				}
			default:
				r, sz := utf8.DecodeRune(tab[pos:])
				if sz == 0 {
					pos++
					continue
				}
				if r != utf8.RuneError {
					if _, exists := f.glyphs[r]; !exists {
						f.glyphs[r] = glyph(idx)
					}
				}
				pos += sz
			}
		}
	} else {
		for i := 0; i < length && i < 256; i++ {
			f.glyphs[rune(i)] = glyph(i)
		}
	}
	if len(f.glyphs) == 0 {
		return nil, fmt.Errorf("PSF2: keine Glyphen")
	}
	return f, nil
}

func parsePSF1(name string, b []byte) (*Font, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("PSF1-Header zu kurz")
	}
	mode, charSize := b[2], int(b[3])
	length := 256
	if mode&1 != 0 {
		length = 512
	}
	if charSize < 1 || len(b) < 4+length*charSize {
		return nil, fmt.Errorf("PSF1: Daten zu kurz")
	}
	f := &Font{Name: name, W: 8, H: charSize, rowBytes: 1, glyphs: map[rune][]byte{}}
	glyphData := b[4:]
	glyph := func(i int) []byte { return glyphData[i*charSize : (i+1)*charSize] }

	if mode&0x06 != 0 {
		// Unicode-Tabelle: UCS-2 little-endian, 0xFFFF terminiert, 0xFFFE Sequenzen.
		tab := glyphData[length*charSize:]
		idx := 0
		inSeq := false
		for pos := 0; pos+1 < len(tab) && idx < length; pos += 2 {
			u := binary.LittleEndian.Uint16(tab[pos:])
			switch u {
			case 0xFFFF:
				idx++
				inSeq = false
			case 0xFFFE:
				inSeq = true
			default:
				if !inSeq {
					if _, exists := f.glyphs[rune(u)]; !exists {
						f.glyphs[rune(u)] = glyph(idx)
					}
				}
			}
		}
	} else {
		for i := 0; i < length && i < 256; i++ {
			f.glyphs[rune(i)] = glyph(i)
		}
	}
	return f, nil
}

// Registry hält alle verfügbaren Schriften.
type Registry struct {
	mu    sync.RWMutex
	fonts map[string]*Font
}

// NewRegistry lädt den eingebauten Font und scannt dir nach PSF-Dateien.
func NewRegistry(dir string) (*Registry, []error) {
	r := &Registry{fonts: map[string]*Font{}}
	b := Builtin()
	r.fonts[b.Name] = b
	var errs []error
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				n := strings.ToLower(e.Name())
				if !strings.HasSuffix(n, ".psf") && !strings.HasSuffix(n, ".psfu") &&
					!strings.HasSuffix(n, ".psf.gz") && !strings.HasSuffix(n, ".psfu.gz") {
					continue
				}
				f, err := LoadPSF(filepath.Join(dir, e.Name()))
				if err != nil {
					errs = append(errs, err)
					continue
				}
				r.fonts[f.Name] = f
			}
		}
	}
	return r, errs
}

// Get liefert den Font mit diesem Namen, sonst den eingebauten Fallback.
func (r *Registry) Get(name string) *Font {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f, ok := r.fonts[name]; ok {
		return f
	}
	return r.fonts[BuiltinName]
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.fonts))
	for n := range r.fonts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
