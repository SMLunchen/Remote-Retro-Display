// Package textview implementiert eine terminal-artige Textansicht für
// Log-Streams und serielle Daten: Zeilenumbruch, Scrolling, C0-Steuerzeichen,
// ein ANSI/VT100-Subset und Telnet-IAC-Filterung. Alle Formatierungs-Features
// sind zuschaltbar. ANSI-Farben werden auf Helligkeitsstufen abgebildet.
package textview

import (
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/fonts"
	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

type Options struct {
	Wrap     bool   `json:"wrap"`     // automatischer Zeilenumbruch
	ANSI     bool   `json:"ansi"`     // ESC-Sequenzen interpretieren
	Controls bool   `json:"controls"` // C0-Steuerzeichen (TAB, BS, FF, …) interpretieren
	Charset  string `json:"charset"`  // "utf8" (Default) oder "latin1"
}

func DefaultOptions() Options {
	return Options{Wrap: true, ANSI: true, Controls: true, Charset: "utf8"}
}

const defaultFg byte = 200

// ANSI-Farben 30–37 / 90–97 als Luminanz (der Monitor macht daraus Grün).
var sgrFg = map[int]byte{
	30: 80, 31: 220, 32: 255, 33: 240, 34: 140, 35: 180, 36: 210, 37: 230,
	90: 120, 91: 235, 92: 255, 93: 250, 94: 160, 95: 200, 96: 225, 97: 255,
}

type cell struct {
	r   rune
	fg  byte
	inv bool
}

type View struct {
	mu   sync.Mutex
	opts Options

	cols, rows int
	cells      []cell
	cx, cy     int

	fg          byte
	bold, dim   bool
	inv         bool

	// Parser-Zustand
	state   int // 0 normal, 1 nach ESC, 2 CSI, 3 OSC, 4 Charset-Byte verwerfen
	csiBuf  []byte
	oscEsc  bool
	utf8Buf []byte

	telnetState int // 0 normal, 1 nach IAC, 2 Option verwerfen, 3 Subnegotiation, 4 Sub nach IAC

	// Rohpuffer (nach Telnet-Filter) für Reflow bei Größen-/Optionswechsel.
	ring    []byte
	ringMax int
	refeed  bool

	dirty bool
}

func New(opts Options) *View {
	v := &View{opts: opts, ringMax: 32 * 1024, fg: defaultFg}
	v.resize(80, 24)
	return v
}

func (v *View) Options() Options {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.opts
}

// SetOptions ändert die Darstellungsoptionen und baut die Ansicht aus dem
// Rohpuffer neu auf.
func (v *View) SetOptions(opts Options) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.opts = opts
	v.rebuild()
}

// Feed verarbeitet eingehende Rohbytes (threadsicher).
func (v *View) Feed(b []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.feed(b)
	v.dirty = true
}

// Clear löscht Ansicht und Rohpuffer.
func (v *View) Clear() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ring = v.ring[:0]
	v.reset()
	v.dirty = true
}

// Dirty meldet und löscht das Änderungsflag.
func (v *View) Dirty() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	d := v.dirty
	v.dirty = false
	return d
}

// Render zeichnet die Ansicht. Passt sich der Zielgröße an; bei Änderung wird
// der Rohpuffer neu eingespeist (Reflow).
func (v *View) Render(dst *surface.Surface, f *fonts.Font, scale int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if scale < 1 {
		scale = 1
	}
	fw, fh := f.Cell()
	cols, rows := dst.W/(fw*scale), dst.H/(fh*scale)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols != v.cols || rows != v.rows {
		v.resize(cols, rows)
		v.rebuild()
	}
	dst.Clear(0)
	for y := 0; y < v.rows; y++ {
		for x := 0; x < v.cols; x++ {
			c := v.cells[y*v.cols+x]
			if c.r == 0 {
				continue
			}
			px, py := x*fw*scale, y*fh*scale
			if c.inv {
				f.Draw(dst, px, py, c.r, scale, 0, c.fg, true)
			} else {
				f.Draw(dst, px, py, c.r, scale, c.fg, 0, false)
			}
		}
	}
}

// --- intern (Aufrufer hält v.mu) ---

func (v *View) resize(cols, rows int) {
	v.cols, v.rows = cols, rows
	v.cells = make([]cell, cols*rows)
	v.cx, v.cy = 0, 0
}

func (v *View) reset() {
	v.cells = make([]cell, v.cols*v.rows)
	v.cx, v.cy = 0, 0
	v.fg, v.bold, v.dim, v.inv = defaultFg, false, false, false
	v.state = 0
	v.csiBuf = v.csiBuf[:0]
	v.utf8Buf = v.utf8Buf[:0]
}

func (v *View) rebuild() {
	v.reset()
	v.refeed = true
	v.feed(v.ring)
	v.refeed = false
	v.dirty = true
}

func (v *View) feed(b []byte) {
	for _, raw := range b {
		if !v.refeed {
			// Telnet-IAC nur im Live-Pfad filtern; der Ring enthält
			// bereits gefilterte Bytes.
			ok, emit := v.telnetFilter(raw)
			if !ok {
				continue
			}
			raw = emit
			v.ring = append(v.ring, raw)
			if len(v.ring) > v.ringMax {
				v.ring = v.ring[len(v.ring)-v.ringMax:]
			}
		}
		v.consume(raw)
	}
}

// telnetFilter verarbeitet Telnet-Negotiation (RFC 854). Liefert false, wenn
// das Byte verschluckt wird.
func (v *View) telnetFilter(b byte) (bool, byte) {
	switch v.telnetState {
	case 0:
		if b == 0xFF {
			v.telnetState = 1
			return false, 0
		}
		return true, b
	case 1:
		switch {
		case b == 0xFF: // IAC IAC = literales 0xFF
			v.telnetState = 0
			return true, 0xFF
		case b >= 251 && b <= 254: // WILL/WONT/DO/DONT + Option
			v.telnetState = 2
			return false, 0
		case b == 250: // SB … IAC SE
			v.telnetState = 3
			return false, 0
		default: // sonstige Kommandos
			v.telnetState = 0
			return false, 0
		}
	case 2:
		v.telnetState = 0
		return false, 0
	case 3:
		if b == 0xFF {
			v.telnetState = 4
		}
		return false, 0
	case 4:
		if b == 240 { // SE
			v.telnetState = 0
		} else {
			v.telnetState = 3
		}
		return false, 0
	}
	return true, b
}

func (v *View) consume(b byte) {
	switch v.state {
	case 1: // nach ESC
		v.handleEscByte(b)
		return
	case 2: // CSI
		v.handleCSIByte(b)
		return
	case 3: // OSC: bis BEL oder ESC \ verschlucken
		if b == 0x07 {
			v.state = 0
		} else if b == 0x1B {
			v.oscEsc = true
		} else if v.oscEsc {
			v.oscEsc = false
			v.state = 0 // ESC + beliebig beendet OSC (üblich: ESC \)
		}
		return
	case 4: // Charset-Designator (ESC ( X u. ä.): ein Byte verwerfen
		v.state = 0
		return
	}

	if b == 0x1B {
		v.state = 1
		v.utf8Buf = v.utf8Buf[:0]
		return
	}
	if b < 0x20 || b == 0x7F {
		v.control(b)
		return
	}
	// Zeichendekodierung
	if v.opts.Charset == "latin1" {
		v.putRune(rune(b))
		return
	}
	v.feedUTF8(b)
}

func (v *View) feedUTF8(b byte) {
	if len(v.utf8Buf) == 0 {
		switch {
		case b < 0x80:
			v.putRune(rune(b))
		case b >= 0xC2 && b <= 0xF4:
			v.utf8Buf = append(v.utf8Buf, b)
		default:
			v.putRune('·')
		}
		return
	}
	if b&0xC0 != 0x80 { // kein Fortsetzungsbyte → Sequenz verwerfen
		v.utf8Buf = v.utf8Buf[:0]
		v.putRune('·')
		v.consume(b)
		return
	}
	v.utf8Buf = append(v.utf8Buf, b)
	first := v.utf8Buf[0]
	need := 2
	if first >= 0xF0 {
		need = 4
	} else if first >= 0xE0 {
		need = 3
	}
	if len(v.utf8Buf) == need {
		r := decodeUTF8(v.utf8Buf)
		v.utf8Buf = v.utf8Buf[:0]
		v.putRune(r)
	}
}

func decodeUTF8(b []byte) rune {
	var r rune
	switch len(b) {
	case 2:
		r = rune(b[0]&0x1F)<<6 | rune(b[1]&0x3F)
	case 3:
		r = rune(b[0]&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F)
	case 4:
		r = rune(b[0]&0x07)<<18 | rune(b[1]&0x3F)<<12 | rune(b[2]&0x3F)<<6 | rune(b[3]&0x3F)
	default:
		r = '·'
	}
	if r == 0 {
		r = '·'
	}
	return r
}

func (v *View) control(b byte) {
	// LF und CR wirken immer, sonst wäre die Ansicht unbrauchbar.
	switch b {
	case '\n':
		v.newline()
		return
	case '\r':
		v.cx = 0
		return
	}
	if !v.opts.Controls {
		if b != 0 {
			v.putRune('·')
		}
		return
	}
	switch b {
	case '\t':
		next := (v.cx/8 + 1) * 8
		for v.cx < next && v.cx < v.cols {
			v.putRune(' ')
		}
	case 0x08: // BS
		if v.cx > 0 {
			v.cx--
		}
	case 0x0C: // FF → Bildschirm löschen
		v.clearAll()
	case 0x07, 0x00, 0x0B, 0x0E, 0x0F:
		// BEL/NUL/VT/SO/SI ignorieren
	default:
		// übrige C0 ignorieren
	}
}

func (v *View) handleEscByte(b byte) {
	switch b {
	case '[':
		v.state = 2
		v.csiBuf = v.csiBuf[:0]
	case ']':
		v.state = 3
		v.oscEsc = false
	case '(', ')', '*', '+':
		v.state = 4
	case 'c': // RIS: Reset
		v.state = 0
		if v.opts.ANSI {
			v.clearAll()
			v.fg, v.bold, v.dim, v.inv = defaultFg, false, false, false
		}
	case 'D': // IND
		v.state = 0
		if v.opts.ANSI {
			v.newline()
		}
	case 'M': // RI
		v.state = 0
		if v.opts.ANSI && v.cy > 0 {
			v.cy--
		}
	default:
		v.state = 0
	}
}

func (v *View) handleCSIByte(b byte) {
	if b >= 0x20 && b <= 0x3F {
		if len(v.csiBuf) < 64 {
			v.csiBuf = append(v.csiBuf, b)
		}
		return
	}
	if b < 0x40 || b > 0x7E { // kaputt → abbrechen
		v.state = 0
		return
	}
	v.state = 0
	if !v.opts.ANSI {
		return
	}
	params := parseParams(v.csiBuf)
	p := func(i, def int) int {
		if i < len(params) && params[i] > 0 {
			return params[i]
		}
		return def
	}
	switch b {
	case 'A':
		v.cy = max(0, v.cy-p(0, 1))
	case 'B':
		v.cy = min(v.rows-1, v.cy+p(0, 1))
	case 'C':
		v.cx = min(v.cols-1, v.cx+p(0, 1))
	case 'D':
		v.cx = max(0, v.cx-p(0, 1))
	case 'H', 'f':
		v.cy = clamp(p(0, 1)-1, 0, v.rows-1)
		v.cx = clamp(p(1, 1)-1, 0, v.cols-1)
	case 'J':
		switch pVal(params, 0) {
		case 0:
			v.clearRange(v.cy*v.cols+v.cx, len(v.cells))
		case 1:
			v.clearRange(0, v.cy*v.cols+v.cx+1)
		default:
			v.clearAll()
		}
	case 'K':
		switch pVal(params, 0) {
		case 0:
			v.clearRange(v.cy*v.cols+v.cx, (v.cy+1)*v.cols)
		case 1:
			v.clearRange(v.cy*v.cols, v.cy*v.cols+v.cx+1)
		default:
			v.clearRange(v.cy*v.cols, (v.cy+1)*v.cols)
		}
	case 'm':
		if len(params) == 0 {
			params = []int{0}
		}
		for _, n := range params {
			v.sgr(n)
		}
	}
}

func pVal(params []int, i int) int {
	if i < len(params) {
		return params[i]
	}
	return 0
}

func parseParams(buf []byte) []int {
	var out []int
	cur, has := 0, false
	for _, c := range buf {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			has = true
		case c == ';':
			out = append(out, cur)
			cur, has = 0, false
		}
	}
	if has || len(out) > 0 {
		out = append(out, cur)
	}
	return out
}

func (v *View) sgr(n int) {
	switch {
	case n == 0:
		v.fg, v.bold, v.dim, v.inv = defaultFg, false, false, false
	case n == 1:
		v.bold, v.dim = true, false
	case n == 2:
		v.dim, v.bold = true, false
	case n == 7:
		v.inv = true
	case n == 22:
		v.bold, v.dim = false, false
	case n == 27:
		v.inv = false
	case n == 39:
		v.fg = defaultFg
	case sgrFg[n] != 0:
		v.fg = sgrFg[n]
	case n == 38 || n == 48:
		// 256-/Truecolor-Angaben nicht unterstützt; Folgeparameter sind in
		// derselben Sequenz bereits mitgelesen und werden hier ignoriert.
	}
}

func (v *View) effFg() byte {
	f := v.fg
	if v.bold {
		f = 255
	} else if v.dim {
		f = f / 2
	}
	return f
}

func (v *View) putRune(r rune) {
	if v.cx >= v.cols {
		if v.opts.Wrap {
			v.newline()
		} else {
			return // abschneiden bis zum nächsten LF
		}
	}
	v.cells[v.cy*v.cols+v.cx] = cell{r: r, fg: v.effFg(), inv: v.inv}
	v.cx++
}

func (v *View) newline() {
	v.cx = 0
	if v.cy < v.rows-1 {
		v.cy++
		return
	}
	copy(v.cells, v.cells[v.cols:])
	tail := v.cells[(v.rows-1)*v.cols:]
	for i := range tail {
		tail[i] = cell{}
	}
}

func (v *View) clearAll() {
	for i := range v.cells {
		v.cells[i] = cell{}
	}
	v.cx, v.cy = 0, 0
}

func (v *View) clearRange(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(v.cells) {
		to = len(v.cells)
	}
	for i := from; i < to; i++ {
		v.cells[i] = cell{}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
