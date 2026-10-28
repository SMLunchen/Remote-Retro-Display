// Package surface stellt eine 8-Bit-Graustufen-Zeichenfläche mit einfachen
// Zeichenprimitiven bereit. Alle Operationen clippen an den Rändern.
package surface

import "image"

type Surface struct {
	W, H int
	Pix  []byte // row-major, 1 Byte Luminanz pro Pixel
}

func New(w, h int) *Surface {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &Surface{W: w, H: h, Pix: make([]byte, w*h)}
}

func (s *Surface) Clear(v byte) {
	for i := range s.Pix {
		s.Pix[i] = v
	}
}

func (s *Surface) Set(x, y int, v byte) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.Pix[y*s.W+x] = v
}

func (s *Surface) Get(x, y int) byte {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return 0
	}
	return s.Pix[y*s.W+x]
}

func (s *Surface) FillRect(x, y, w, h int, v byte) {
	x0, y0, x1, y1 := clipRect(x, y, w, h, s.W, s.H)
	for yy := y0; yy < y1; yy++ {
		row := s.Pix[yy*s.W+x0 : yy*s.W+x1]
		for i := range row {
			row[i] = v
		}
	}
}

// InvertRect invertiert die Helligkeit eines Bereichs (für Hervorhebungen).
func (s *Surface) InvertRect(x, y, w, h int) {
	x0, y0, x1, y1 := clipRect(x, y, w, h, s.W, s.H)
	for yy := y0; yy < y1; yy++ {
		row := s.Pix[yy*s.W+x0 : yy*s.W+x1]
		for i := range row {
			row[i] = 255 - row[i]
		}
	}
}

// Rect zeichnet einen Rahmen mit Linienstärke t (nach innen).
func (s *Surface) Rect(x, y, w, h, t int, v byte) {
	if w <= 0 || h <= 0 {
		return
	}
	s.FillRect(x, y, w, t, v)
	s.FillRect(x, y+h-t, w, t, v)
	s.FillRect(x, y, t, h, v)
	s.FillRect(x+w-t, y, t, h, v)
}

func (s *Surface) HLine(x, y, w, t int, v byte) { s.FillRect(x, y, w, t, v) }
func (s *Surface) VLine(x, y, h, t int, v byte) { s.FillRect(x, y, t, h, v) }

// Line zeichnet eine Linie (Bresenham) mit Strichstärke t.
func (s *Surface) Line(x0, y0, x1, y1, t int, v byte) {
	if t < 1 {
		t = 1
	}
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		s.FillRect(x0-t/2, y0-t/2, t, t, v)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// Circle zeichnet einen Kreisring mit Strichstärke t (Midpoint-Algorithmus).
func (s *Surface) Circle(cx, cy, r, t int, v byte) {
	if r < 1 {
		return
	}
	if t < 1 {
		t = 1
	}
	x, y, err := r, 0, 1-r
	plot := func(px, py int) { s.FillRect(px-t/2, py-t/2, t, t, v) }
	for x >= y {
		plot(cx+x, cy+y)
		plot(cx-x, cy+y)
		plot(cx+x, cy-y)
		plot(cx-x, cy-y)
		plot(cx+y, cy+x)
		plot(cx-y, cy+x)
		plot(cx+y, cy-x)
		plot(cx-y, cy-x)
		y++
		if err < 0 {
			err += 2*y + 1
		} else {
			x--
			err += 2*(y-x) + 1
		}
	}
}

// Blit kopiert src vollständig nach (dx,dy) auf s.
func (s *Surface) Blit(src *Surface, dx, dy int) {
	for sy := 0; sy < src.H; sy++ {
		ty := dy + sy
		if ty < 0 || ty >= s.H {
			continue
		}
		sx0, tx0 := 0, dx
		if tx0 < 0 {
			sx0 = -tx0
			tx0 = 0
		}
		n := src.W - sx0
		if tx0+n > s.W {
			n = s.W - tx0
		}
		if n <= 0 {
			continue
		}
		copy(s.Pix[ty*s.W+tx0:ty*s.W+tx0+n], src.Pix[sy*src.W+sx0:sy*src.W+sx0+n])
	}
}

// BlitScaled skaliert src per Nearest-Neighbor in das Zielrechteck.
func (s *Surface) BlitScaled(src *Surface, dx, dy, dw, dh int) {
	if dw <= 0 || dh <= 0 || src.W == 0 || src.H == 0 {
		return
	}
	for ty := 0; ty < dh; ty++ {
		yy := dy + ty
		if yy < 0 || yy >= s.H {
			continue
		}
		sy := ty * src.H / dh
		for tx := 0; tx < dw; tx++ {
			xx := dx + tx
			if xx < 0 || xx >= s.W {
				continue
			}
			s.Pix[yy*s.W+xx] = src.Pix[sy*src.W+tx*src.W/dw]
		}
	}
}

func (s *Surface) Copy() *Surface {
	c := New(s.W, s.H)
	copy(c.Pix, s.Pix)
	return c
}

// Gray liefert eine image.Gray-Sicht auf die Pixel (ohne Kopie).
func (s *Surface) Gray() *image.Gray {
	return &image.Gray{Pix: s.Pix, Stride: s.W, Rect: image.Rect(0, 0, s.W, s.H)}
}

// clipRect garantiert 0 ≤ x0 ≤ x1 ≤ maxW und 0 ≤ y0 ≤ y1 ≤ maxH — auch wenn
// das Rechteck komplett außerhalb der Fläche liegt.
func clipRect(x, y, w, h, maxW, maxH int) (x0, y0, x1, y1 int) {
	x0 = clampInt(x, 0, maxW)
	y0 = clampInt(y, 0, maxH)
	x1 = clampInt(x+w, x0, maxW)
	y1 = clampInt(y+h, y0, maxH)
	return
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
