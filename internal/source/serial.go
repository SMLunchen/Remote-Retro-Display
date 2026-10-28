package source

import (
	"fmt"
	"log"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
	"github.com/SMLunchen/Remote-Retro-Display/internal/textview"
)

type SerialConfig struct {
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"databits"`
	Parity   string `json:"parity"` // none|even|odd
	StopBits int    `json:"stopbits"`
	Enabled  bool   `json:"enabled"`
}

func (c *SerialConfig) normalize() {
	if c.Baud <= 0 {
		c.Baud = 9600
	}
	if c.DataBits == 0 {
		c.DataBits = 8
	}
	if c.StopBits == 0 {
		c.StopBits = 1
	}
	if c.Parity == "" {
		c.Parity = "none"
	}
}

// Serial zeigt die Daten eines USB-RS232-Wandlers als Terminal-Ansicht an.
// Bei Fehlern wird alle 5 s neu verbunden, solange die Quelle aktiviert ist.
type Serial struct {
	id   string
	view *textview.View
	fp   FontProvider

	mu     sync.Mutex
	cfg    SerialConfig
	status string
	gen    int // Generation: invalidiert laufende run()-Schleifen
	port   serial.Port
	dirty  bool
}

func NewSerial(fp FontProvider, cfg SerialConfig) *Serial {
	cfg.normalize()
	s := &Serial{
		id:   "serial:" + cfg.Port,
		view: textview.New(textview.DefaultOptions()),
		fp:   fp,
		cfg:  cfg,
	}
	s.setStatus("nicht verbunden")
	return s
}

func (s *Serial) ID() string { return s.id }

func (s *Serial) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.status
	if state == "" {
		state = "verbunden"
	}
	return fmt.Sprintf("Seriell %s @%d (%s)", s.cfg.Port, s.cfg.Baud, state)
}

func (s *Serial) Config() SerialConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Apply übernimmt eine neue Konfiguration und startet die Verbindung neu.
func (s *Serial) Apply(cfg SerialConfig) {
	cfg.normalize()
	s.mu.Lock()
	s.cfg = cfg
	s.gen++
	gen := s.gen
	if s.port != nil {
		s.port.Close()
		s.port = nil
	}
	s.mu.Unlock()
	if cfg.Enabled {
		s.setStatus("verbinde …")
		go s.run(gen)
	} else {
		s.setStatus("deaktiviert")
	}
}

func (s *Serial) Stop() {
	s.mu.Lock()
	s.gen++
	s.cfg.Enabled = false
	if s.port != nil {
		s.port.Close()
		s.port = nil
	}
	s.mu.Unlock()
	s.setStatus("deaktiviert")
}

func (s *Serial) setStatus(msg string) {
	s.mu.Lock()
	s.status = msg
	s.dirty = true
	s.mu.Unlock()
}

func (s *Serial) current(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen == gen && s.cfg.Enabled
}

func (s *Serial) run(gen int) {
	for s.current(gen) {
		s.mu.Lock()
		cfg := s.cfg
		s.mu.Unlock()

		mode := &serial.Mode{BaudRate: cfg.Baud, DataBits: cfg.DataBits}
		switch cfg.Parity {
		case "even":
			mode.Parity = serial.EvenParity
		case "odd":
			mode.Parity = serial.OddParity
		default:
			mode.Parity = serial.NoParity
		}
		if cfg.StopBits == 2 {
			mode.StopBits = serial.TwoStopBits
		} else {
			mode.StopBits = serial.OneStopBit
		}

		p, err := serial.Open(cfg.Port, mode)
		if err != nil {
			s.setStatus("Fehler: " + err.Error())
			log.Printf("serial %s: %v", cfg.Port, err)
			for i := 0; i < 50 && s.current(gen); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			continue
		}
		s.mu.Lock()
		if s.gen != gen {
			s.mu.Unlock()
			p.Close()
			return
		}
		s.port = p
		s.mu.Unlock()
		s.setStatus("")
		log.Printf("serial %s: verbunden (%d baud)", cfg.Port, cfg.Baud)

		buf := make([]byte, 4096)
		for s.current(gen) {
			n, err := p.Read(buf)
			if n > 0 {
				s.view.Feed(buf[:n])
			}
			if err != nil {
				s.setStatus("Fehler: " + err.Error())
				break
			}
		}
		p.Close()
		s.mu.Lock()
		if s.port == p {
			s.port = nil
		}
		s.mu.Unlock()
		for i := 0; i < 50 && s.current(gen); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (s *Serial) Dirty() bool {
	s.mu.Lock()
	d := s.dirty
	s.dirty = false
	s.mu.Unlock()
	return s.view.Dirty() || d
}

func (s *Serial) Render(dst *surface.Surface) {
	f, scale := s.fp()
	s.view.Render(dst, f, scale)
	s.mu.Lock()
	status := s.status
	cfg := s.cfg
	s.mu.Unlock()
	if status != "" {
		msg := fmt.Sprintf("[%s @%d: %s]", cfg.Port, cfg.Baud, status)
		fh := f.H
		dst.FillRect(0, dst.H-fh-4, dst.W, fh+4, 0)
		f.DrawString(dst, 2, dst.H-fh-2, msg, 1, 140, 0, false)
	}
}

func (s *Serial) SetOptions(o textview.Options) { s.view.SetOptions(o) }
func (s *Serial) ViewOptions() textview.Options { return s.view.Options() }

// ListPorts nennt die im System vorhandenen seriellen Schnittstellen.
func ListPorts() ([]string, error) {
	return serial.GetPortsList()
}
