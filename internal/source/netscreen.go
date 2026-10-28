package source

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/SMLunchen/Remote-Retro-Display/internal/fonts"
	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
)

// Widget ist ein Element eines Screens. Koordinaten in Zeichenzellen der
// globalen Schrift; size skaliert die Schrift des Widgets.
type Widget struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	W          int    `json:"w,omitempty"`
	H          int    `json:"h,omitempty"`
	Size       int    `json:"size,omitempty"`
	Brightness *int   `json:"brightness,omitempty"`
	Invert     bool   `json:"invert,omitempty"`

	Text  string `json:"text,omitempty"`
	Align string `json:"align,omitempty"`

	Cols []string   `json:"cols,omitempty"`
	Rows [][]string `json:"rows,omitempty"`

	Kind      string    `json:"kind,omitempty"`
	Series    []float64 `json:"series,omitempty"`
	Min       *float64  `json:"min,omitempty"`
	Max       *float64  `json:"max,omitempty"`
	MaxPoints int       `json:"maxPoints,omitempty"`
	Label     string    `json:"label,omitempty"`

	Value *float64 `json:"value,omitempty"`
	Fill  bool     `json:"fill,omitempty"`

	// Append hängt Werte an Series an (nur in Updates sinnvoll).
	Append json.RawMessage `json:"append,omitempty"`
}

func (w *Widget) brightness() byte {
	if w.Brightness == nil {
		return 255
	}
	b := *w.Brightness
	if b < 0 {
		b = 0
	}
	if b > 255 {
		b = 255
	}
	return byte(b)
}

type screen struct {
	widgets []*Widget
	byID    map[string]*Widget
}

// ScreenRequest ist der Body von POST /api/screen.
type ScreenRequest struct {
	Name     string          `json:"name"`
	Activate bool            `json:"activate"`
	Widgets  []*Widget       `json:"widgets"`
	Update   json.RawMessage `json:"update"`
}

// Net verwaltet benannte Screens aus Widgets (Protokoll-Ebene 2).
type Net struct {
	mu      sync.Mutex
	screens map[string]*screen
	active  string
	fp      FontProvider
	dirty   bool
}

func NewNet(fp FontProvider) *Net {
	n := &Net{screens: map[string]*screen{}, active: "default", fp: fp, dirty: true}
	n.screens["default"] = &screen{byID: map[string]*Widget{}}
	return n
}

func (n *Net) ID() string { return "net" }

func (n *Net) Title() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return fmt.Sprintf("Netzwerk-Screens (aktiv: %s)", n.active)
}

func (n *Net) Dirty() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	d := n.dirty
	n.dirty = false
	return d
}

func (n *Net) ScreenNames() (names []string, active string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for name := range n.screens {
		names = append(names, name)
	}
	return names, n.active
}

// Apply verarbeitet einen ScreenRequest (Definition, Update oder Aktivierung).
func (n *Net) Apply(req *ScreenRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	name := req.Name
	if name == "" {
		name = "default"
	}
	sc := n.screens[name]
	if sc == nil {
		sc = &screen{byID: map[string]*Widget{}}
		n.screens[name] = sc
	}

	if req.Widgets != nil {
		sc.widgets = req.Widgets
		sc.byID = map[string]*Widget{}
		for i, w := range sc.widgets {
			if w.ID == "" {
				w.ID = fmt.Sprintf("w%d", i)
			}
			if err := w.applyAppend(); err != nil {
				return err
			}
			sc.byID[w.ID] = w
		}
	}

	if len(req.Update) > 0 {
		var idOnly struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Update, &idOnly); err != nil {
			return fmt.Errorf("update: %v", err)
		}
		w := sc.byID[idOnly.ID]
		if w == nil {
			return fmt.Errorf("update: Widget %q nicht in Screen %q", idOnly.ID, name)
		}
		// JSON-Unmarshal in das bestehende Struct wirkt als Merge:
		// nicht mitgeschickte Felder bleiben erhalten.
		if err := json.Unmarshal(req.Update, w); err != nil {
			return fmt.Errorf("update: %v", err)
		}
		if err := w.applyAppend(); err != nil {
			return err
		}
	}

	if req.Activate {
		n.active = name
	}
	if n.active == name || req.Activate {
		n.dirty = true
	}
	return nil
}

func (n *Net) Delete(name string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.screens[name]; !ok {
		return fmt.Errorf("Screen %q existiert nicht", name)
	}
	delete(n.screens, name)
	if n.active == name {
		n.active = "default"
		if n.screens["default"] == nil {
			n.screens["default"] = &screen{byID: map[string]*Widget{}}
		}
	}
	n.dirty = true
	return nil
}

func (w *Widget) applyAppend() error {
	if len(w.Append) == 0 {
		return nil
	}
	var one float64
	var many []float64
	if err := json.Unmarshal(w.Append, &one); err == nil {
		w.Series = append(w.Series, one)
	} else if err := json.Unmarshal(w.Append, &many); err == nil {
		w.Series = append(w.Series, many...)
	} else {
		return fmt.Errorf("append: Zahl oder Zahlenliste erwartet")
	}
	maxPts := w.MaxPoints
	if maxPts <= 0 {
		maxPts = 200
	}
	if len(w.Series) > maxPts {
		w.Series = w.Series[len(w.Series)-maxPts:]
	}
	w.Append = nil
	return nil
}

// --- Rendering ---

func (n *Net) Render(dst *surface.Surface) {
	n.mu.Lock()
	sc := n.screens[n.active]
	var widgets []*Widget
	if sc != nil {
		widgets = append(widgets, sc.widgets...)
	}
	n.mu.Unlock()

	dst.Clear(0)
	f, baseScale := n.fp()
	for _, w := range widgets {
		n.renderWidget(dst, f, baseScale, w)
	}
}

func (n *Net) renderWidget(dst *surface.Surface, f *fonts.Font, baseScale int, w *Widget) {
	scale := baseScale
	if w.Size > 0 {
		scale = w.Size
	}
	fw, fh := f.Cell()
	cw, ch := fw*baseScale, fh*baseScale // Zellenraster immer in Basis-Schrift
	px, py := w.X*cw, w.Y*ch
	fg := w.brightness()

	switch w.Type {
	case "text":
		n.renderText(dst, f, w, px, py, scale, cw, fg)
	case "table":
		n.renderTable(dst, f, w, px, py, scale, fg)
	case "chart":
		n.renderChart(dst, f, w, px, py, scale, cw, ch, fg)
	case "progress":
		n.renderProgress(dst, f, w, px, py, scale, cw, ch, fg)
	case "box":
		bw, bh := max(w.W, 1)*cw, max(w.H, 1)*ch
		if w.Fill {
			dst.FillRect(px, py, bw, bh, fg)
		} else {
			dst.Rect(px, py, bw, bh, 2, fg)
		}
	}
	if w.Invert && w.Type != "text" {
		// text invertiert pro Zeile selbst (siehe renderText)
		bw := max(w.W, 1) * cw
		bh := max(w.H, 1) * ch
		dst.InvertRect(px, py, bw, bh)
	}
}

func (n *Net) renderText(dst *surface.Surface, f *fonts.Font, w *Widget, px, py, scale, cw int, fg byte) {
	fwS := f.W * scale
	fhS := f.H * scale
	for i, line := range strings.Split(w.Text, "\n") {
		x := px
		if w.W > 0 {
			avail := w.W * cw
			lw := f.StringWidth(line, scale)
			switch w.Align {
			case "center":
				x = px + (avail-lw)/2
			case "right":
				x = px + avail - lw
			}
		}
		y := py + i*fhS
		if w.Invert {
			dst.FillRect(x-2, y-1, f.StringWidth(line, scale)+4, fhS+2, fg)
			f.DrawString(dst, x, y, line, scale, 0, fg, false)
		} else {
			f.DrawString(dst, x, y, line, scale, fg, 0, false)
		}
	}
	_ = fwS
}

func (n *Net) renderTable(dst *surface.Surface, f *fonts.Font, w *Widget, px, py, scale int, fg byte) {
	fwS, fhS := f.W*scale, f.H*scale
	nCols := len(w.Cols)
	for _, r := range w.Rows {
		if len(r) > nCols {
			nCols = len(r)
		}
	}
	if nCols == 0 {
		return
	}
	widths := make([]int, nCols)
	for i, c := range w.Cols {
		widths[i] = max(widths[i], utf8.RuneCountInString(c))
	}
	for _, r := range w.Rows {
		for i, c := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	totalW := 0
	for i := range widths {
		widths[i] += 2 // Padding
		totalW += widths[i]
	}

	y := py
	if len(w.Cols) > 0 {
		dst.FillRect(px, y, totalW*fwS, fhS, fg)
		x := px
		for i, c := range w.Cols {
			f.DrawString(dst, x+fwS, y, c, scale, 0, fg, false)
			x += widths[i] * fwS
		}
		y += fhS + 2
	}
	for _, row := range w.Rows {
		if y+fhS > dst.H {
			break
		}
		x := px
		for i, c := range row {
			if i >= nCols {
				break
			}
			f.DrawString(dst, x+fwS, y, c, scale, fg, 0, false)
			x += widths[i] * fwS
		}
		y += fhS
	}
}

func (n *Net) renderChart(dst *surface.Surface, f *fonts.Font, w *Widget, px, py, scale, cw, ch int, fg byte) {
	bw := max(w.W, 10) * cw
	bh := max(w.H, 4) * ch
	dst.Rect(px, py, bw, bh, 2, 140)
	if w.Label != "" {
		f.DrawString(dst, px+6, py+4, w.Label, 1, fg, 0, true)
	}
	if len(w.Series) == 0 {
		return
	}
	lo, hi := w.Series[0], w.Series[0]
	for _, v := range w.Series {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if w.Min != nil {
		lo = *w.Min
	}
	if w.Max != nil {
		hi = *w.Max
	}
	if hi <= lo {
		hi = lo + 1
	}
	// Plotbereich mit Innenabstand
	ix, iy := px+4, py+4
	iw, ih := bw-8, bh-8
	if f.H+6 < ih && w.Label != "" {
		iy += f.H + 2
		ih -= f.H + 2
	}
	yOf := func(v float64) int {
		t := (v - lo) / (hi - lo)
		return iy + ih - 1 - int(t*float64(ih-1))
	}
	switch w.Kind {
	case "bar":
		bwid := iw / len(w.Series)
		if bwid < 2 {
			bwid = 2
		}
		for i, v := range w.Series {
			x := ix + i*iw/len(w.Series)
			y := yOf(v)
			dst.FillRect(x, y, max(bwid-2, 2), iy+ih-y, fg)
		}
	default: // line
		if len(w.Series) == 1 {
			dst.FillRect(ix, yOf(w.Series[0]), iw, 2, fg)
			break
		}
		for i := 1; i < len(w.Series); i++ {
			x0 := ix + (i-1)*(iw-1)/(len(w.Series)-1)
			x1 := ix + i*(iw-1)/(len(w.Series)-1)
			dst.Line(x0, yOf(w.Series[i-1]), x1, yOf(w.Series[i]), 2, fg)
		}
	}
	// Min/Max-Beschriftung rechts
	maxLbl := trimNum(hi)
	minLbl := trimNum(lo)
	f.DrawString(dst, px+bw-f.StringWidth(maxLbl, 1)-4, py+4, maxLbl, 1, 160, 0, true)
	f.DrawString(dst, px+bw-f.StringWidth(minLbl, 1)-4, py+bh-f.H-4, minLbl, 1, 160, 0, true)
}

func trimNum(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	s = strings.TrimSuffix(s, ".0")
	return s
}

func (n *Net) renderProgress(dst *surface.Surface, f *fonts.Font, w *Widget, px, py, scale, cw, ch int, fg byte) {
	val := 0.0
	if w.Value != nil {
		val = *w.Value
	}
	if val < 0 {
		val = 0
	}
	if val > 100 {
		val = 100
	}
	y := py
	if w.Label != "" {
		f.DrawString(dst, px, y, w.Label, scale, fg, 0, false)
		y += f.H*scale + 2
	}
	bw := max(w.W, 10) * cw
	bh := f.H*scale + 4
	pct := fmt.Sprintf("%.0f%%", val)
	pctW := f.StringWidth(pct, scale) + cw
	barW := bw - pctW
	if barW < 4*cw {
		barW = 4 * cw
	}
	dst.Rect(px, y, barW, bh, 2, fg)
	fill := int(float64(barW-8) * val / 100)
	dst.FillRect(px+4, y+4, fill, bh-8, fg)
	f.DrawString(dst, px+barW+cw/2, y+2, pct, scale, fg, 0, false)
}
