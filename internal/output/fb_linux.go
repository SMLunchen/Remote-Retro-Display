//go:build linux

package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

// Framebuffer schreibt Frames direkt nach /dev/fbN. Geometrie und Format
// kommen aus sysfs (/sys/class/graphics/fbN/...), damit weder ioctl noch
// cgo nötig sind.
type Framebuffer struct {
	f      *os.File
	w, h   int
	bpp    int // Bits pro Pixel
	stride int // Bytes pro Zeile
	buf    []byte
}

func OpenFramebuffer(dev string) (*Framebuffer, error) {
	name := filepath.Base(dev)
	sys := "/sys/class/graphics/" + name

	vs, err := os.ReadFile(sys + "/virtual_size")
	if err != nil {
		return nil, fmt.Errorf("framebuffer %s: %v", dev, err)
	}
	parts := strings.Split(strings.TrimSpace(string(vs)), ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("framebuffer %s: virtual_size unlesbar", dev)
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])

	bppRaw, err := os.ReadFile(sys + "/bits_per_pixel")
	if err != nil {
		return nil, fmt.Errorf("framebuffer %s: %v", dev, err)
	}
	bpp, _ := strconv.Atoi(strings.TrimSpace(string(bppRaw)))

	stride := w * bpp / 8
	if st, err := os.ReadFile(sys + "/stride"); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(st))); err == nil && v > 0 {
			stride = v
		}
	}

	switch bpp {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("framebuffer %s: %d bpp nicht unterstützt", dev, bpp)
	}
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("framebuffer %s: Größe %dx%d unplausibel", dev, w, h)
	}

	f, err := os.OpenFile(dev, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &Framebuffer{f: f, w: w, h: h, bpp: bpp, stride: stride, buf: make([]byte, stride*h)}, nil
}

func (fb *Framebuffer) Size() (int, int) { return fb.w, fb.h }

func (fb *Framebuffer) Push(s *surface.Surface) error {
	w := min(s.W, fb.w)
	h := min(s.H, fb.h)
	for y := 0; y < h; y++ {
		src := s.Pix[y*s.W : y*s.W+w]
		row := fb.buf[y*fb.stride:]
		switch fb.bpp {
		case 16: // RGB565
			for x, v := range src {
				p := uint16(v>>3)<<11 | uint16(v>>2)<<5 | uint16(v>>3)
				row[x*2] = byte(p)
				row[x*2+1] = byte(p >> 8)
			}
		case 24: // BGR
			for x, v := range src {
				row[x*3] = v
				row[x*3+1] = v
				row[x*3+2] = v
			}
		case 32: // BGRX
			for x, v := range src {
				row[x*4] = v
				row[x*4+1] = v
				row[x*4+2] = v
				row[x*4+3] = 0
			}
		}
	}
	_, err := fb.f.WriteAt(fb.buf, 0)
	return err
}

func (fb *Framebuffer) Close() error { return fb.f.Close() }
