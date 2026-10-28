// Package server stellt REST-API, Web-UI und Live-Vorschau bereit.
package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/SMLunchen/Remote-Retro-Display/internal/compositor"
	"github.com/SMLunchen/Remote-Retro-Display/internal/config"
	"github.com/SMLunchen/Remote-Retro-Display/internal/fonts"
	"github.com/SMLunchen/Remote-Retro-Display/internal/source"
	"github.com/SMLunchen/Remote-Retro-Display/internal/textview"
)

//go:embed webui
var webui embed.FS

type Server struct {
	store *config.Store

	reg    *source.Registry
	comp   *compositor.Compositor
	fonts  *fonts.Registry
	fp     source.FontProvider
	net    *source.Net
	img    *source.Image
	stream *source.Stream
	test   *source.Test

	pngMu      sync.Mutex
	pngVersion uint64
	pngData    []byte
}

func New(store *config.Store, reg *source.Registry, comp *compositor.Compositor,
	fr *fonts.Registry, fp source.FontProvider,
	net *source.Net, img *source.Image, stream *source.Stream, test *source.Test) *Server {
	return &Server{store: store, reg: reg, comp: comp, fonts: fr, fp: fp,
		net: net, img: img, stream: stream, test: test}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /preview", s.handlePreviewStream)
	mux.HandleFunc("GET /api/preview.png", s.handlePreviewPNG)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/layout", s.handleLayout)
	mux.HandleFunc("POST /api/screen", s.handleScreen)
	mux.HandleFunc("DELETE /api/screen", s.handleScreenDelete)
	mux.HandleFunc("POST /api/print", s.handlePrint)
	mux.HandleFunc("POST /api/image", s.handleImage)
	mux.HandleFunc("GET /api/serial/ports", s.handleSerialPorts)
	mux.HandleFunc("POST /api/serial", s.handleSerial)
	mux.HandleFunc("POST /api/test", s.handleTest)
	mux.HandleFunc("POST /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/stream/clear", s.handleStreamClear)
	mux.HandleFunc("GET /api/fonts", s.handleFonts)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := webui.ReadFile("webui/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// --- Antwort-Helfer ---

func ok(w http.ResponseWriter, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"ok": true}
	for k, v := range extra {
		resp[k] = v
	}
	json.NewEncoder(w).Encode(resp)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	return dec.Decode(v)
}

// --- Vorschau ---

// encodeFrame liefert den aktuellen Frame als PNG, gecacht pro Frame-Version.
func (s *Server) encodeFrame() []byte {
	version := s.comp.Version()
	s.pngMu.Lock()
	defer s.pngMu.Unlock()
	if s.pngData != nil && s.pngVersion == version {
		return s.pngData
	}
	frame, v := s.comp.Snapshot()
	var buf bytes.Buffer
	if err := png.Encode(&buf, frame.Gray()); err != nil {
		return nil
	}
	s.pngData = buf.Bytes()
	s.pngVersion = v
	return s.pngData
}

func (s *Server) handlePreviewPNG(w http.ResponseWriter, r *http.Request) {
	data := s.encodeFrame()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// handlePreviewStream liefert ~1 fps als multipart/x-mixed-replace für <img>.
func (s *Server) handlePreviewStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	var last uint64
	for {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		v := s.comp.Version()
		if v == last {
			continue
		}
		last = v
		data := s.encodeFrame()
		if data == nil {
			continue
		}
		fmt.Fprintf(w, "--frame\r\nContent-Type: image/png\r\nContent-Length: %d\r\n\r\n", len(data))
		w.Write(data)
		io.WriteString(w, "\r\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// --- Status ---

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	mode, vps := s.comp.Layout()
	type srcInfo struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var sources []srcInfo
	for _, src := range s.reg.List() {
		sources = append(sources, srcInfo{src.ID(), src.Title()})
	}
	names, active := s.net.ScreenNames()
	ports, _ := source.ListPorts()

	cfgCopy := s.store.Snapshot()
	cfgCopy.Stream.Options = s.stream.ViewOptions()

	ok(w, map[string]any{
		"layout":      map[string]any{"mode": mode, "viewports": vps},
		"sources":     sources,
		"screens":     map[string]any{"names": names, "active": active},
		"fonts":       s.fonts.Names(),
		"serialPorts": ports,
		"testPattern": s.test.Pattern(),
		"config":      cfgCopy,
	})
}

func (s *Server) handleFonts(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"fonts": s.fonts.Names()})
}

// --- Steuerung ---

func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode      string   `json:"mode"`
		Viewports []string `json:"viewports"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	switch req.Mode {
	case "", "single", "hsplit", "vsplit":
	default:
		fail(w, 400, fmt.Errorf("mode muss single|hsplit|vsplit sein"))
		return
	}
	s.comp.SetLayout(req.Mode, req.Viewports)
	s.persist(func(c *config.Config) {
		mode, vps := s.comp.Layout()
		c.Layout.Mode = mode
		c.Layout.Viewports = vps
	})
	ok(w, nil)
}

func (s *Server) handleScreen(w http.ResponseWriter, r *http.Request) {
	var req source.ScreenRequest
	if err := decode(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.net.Apply(&req); err != nil {
		fail(w, 400, err)
		return
	}
	ok(w, nil)
}

func (s *Server) handleScreenDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		fail(w, 400, fmt.Errorf("Parameter name fehlt"))
		return
	}
	if err := s.net.Delete(name); err != nil {
		fail(w, 404, err)
		return
	}
	ok(w, nil)
}

func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	s.stream.Feed(data)
	ok(w, nil)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.img.SetImage(data); err != nil {
		fail(w, 400, err)
		return
	}
	if r.URL.Query().Get("show") == "1" {
		s.comp.SetLayout("", []string{"image"})
	}
	ok(w, nil)
}

func (s *Server) handleSerialPorts(w http.ResponseWriter, r *http.Request) {
	ports, err := source.ListPorts()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if ports == nil {
		ports = []string{}
	}
	ok(w, map[string]any{"ports": ports})
}

func (s *Server) handleSerial(w http.ResponseWriter, r *http.Request) {
	var cfg source.SerialConfig
	if err := decode(r, &cfg); err != nil {
		fail(w, 400, err)
		return
	}
	if cfg.Port == "" {
		fail(w, 400, fmt.Errorf("port fehlt"))
		return
	}
	id := "serial:" + cfg.Port
	var ser *source.Serial
	if existing := s.reg.Get(id); existing != nil {
		ser = existing.(*source.Serial)
		ser.Apply(cfg)
	} else {
		ser = source.NewSerial(s.fp, cfg)
		s.reg.Add(ser)
		ser.Apply(cfg)
	}
	s.persist(func(c *config.Config) {
		for i := range c.Serial {
			if c.Serial[i].Port == cfg.Port {
				c.Serial[i] = cfg
				return
			}
		}
		c.Serial = append(c.Serial, cfg)
	})
	s.comp.Invalidate()
	ok(w, map[string]any{"id": id})
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pattern string `json:"pattern"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.test.SetPattern(req.Pattern); err != nil {
		fail(w, 400, err)
		return
	}
	ok(w, nil)
}

func (s *Server) handleStreamClear(w http.ResponseWriter, r *http.Request) {
	s.stream.ClearView()
	ok(w, nil)
}

type cfgPatch struct {
	Font   *config.FontCfg   `json:"font"`
	Insets *config.Insets    `json:"insets"`
	MaxFPS *int              `json:"maxFPS"`
	Stream *textview.Options `json:"stream"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	var p cfgPatch
	if err := decode(r, &p); err != nil {
		fail(w, 400, err)
		return
	}
	s.persist(func(c *config.Config) {
		if p.Font != nil {
			c.Font = *p.Font
		}
		if p.Insets != nil {
			c.Insets = *p.Insets
		}
		if p.MaxFPS != nil {
			c.MaxFPS = *p.MaxFPS
		}
		if p.Stream != nil {
			c.Stream.Options = *p.Stream
		}
	})
	snap := s.store.Snapshot()
	if p.Insets != nil {
		s.comp.SetInsets(snap.Insets)
	}
	if p.MaxFPS != nil {
		s.comp.SetMaxFPS(snap.MaxFPS)
	}
	if p.Stream != nil {
		s.stream.SetOptions(snap.Stream.Options)
	}
	s.comp.Invalidate()
	ok(w, nil)
}

// persist wendet eine Änderung auf die Konfiguration an und speichert sie.
func (s *Server) persist(apply func(*config.Config)) {
	if err := s.store.Update(apply); err != nil {
		log.Printf("config: Speichern fehlgeschlagen: %v", err)
	}
}
