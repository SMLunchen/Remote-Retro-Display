package surface

import "testing"

// Regression: Rechtecke komplett außerhalb der Fläche dürfen nicht panicken
// (Slice-Bounds-Panic bei x0 > W, siehe Crash auf dem Pi mit Chart-Widget
// außerhalb des hsplit-Viewports).
func TestFillRectOutOfBounds(t *testing.T) {
	s := New(333, 440)
	cases := [][4]int{
		{894, 312, 2, 260},   // komplett rechts außerhalb (der Original-Crash)
		{0, 500, 100, 50},    // komplett unterhalb
		{-50, -50, 20, 20},   // komplett links oben außerhalb
		{330, 435, 100, 100}, // ragt über beide Ränder
		{0, 0, 333, 440},     // exakt passend
		{-10, -10, 400, 500}, // größer als die Fläche
	}
	for _, c := range cases {
		s.FillRect(c[0], c[1], c[2], c[3], 255)
		s.Rect(c[0], c[1], c[2], c[3], 2, 255)
		s.InvertRect(c[0], c[1], c[2], c[3])
	}
}

func TestBlitClipping(t *testing.T) {
	dst := New(100, 100)
	src := New(50, 50)
	src.Clear(200)
	for _, off := range [][2]int{{-25, -25}, {90, 90}, {-100, 0}, {0, 150}} {
		dst.Blit(src, off[0], off[1])
	}
	dst.BlitScaled(src, -10, -10, 200, 200)
}
