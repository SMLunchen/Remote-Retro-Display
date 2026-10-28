package source

import (
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/SMLunchen/Remote-Retro-Display/internal/surface"
	"github.com/SMLunchen/Remote-Retro-Display/internal/textview"
)

// Stream ist die „Logs einfach hinballern"-Quelle: ein TCP-Listener, dessen
// Daten (plus REST-POSTs auf /api/print) in einer Terminal-Ansicht landen.
type Stream struct {
	view *textview.View
	fp   FontProvider
	port int

	mu      sync.Mutex
	clients int
	ln      net.Listener
}

func NewStream(fp FontProvider, opts textview.Options, port int) *Stream {
	return &Stream{view: textview.New(opts), fp: fp, port: port}
}

// Listen startet den TCP-Listener (nicht blockierend).
func (s *Stream) Listen() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return nil
}

func (s *Stream) handle(conn net.Conn) {
	s.mu.Lock()
	s.clients++
	s.mu.Unlock()
	log.Printf("stream: Verbindung von %s", conn.RemoteAddr())
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.view.Feed(buf[:n])
		}
		if err != nil {
			break
		}
	}
	conn.Close()
	s.mu.Lock()
	s.clients--
	s.mu.Unlock()
	log.Printf("stream: Verbindung %s beendet", conn.RemoteAddr())
}

func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		s.ln.Close()
		s.ln = nil
	}
}

// Feed nimmt Text aus der REST-API entgegen (Ebene 1).
func (s *Stream) Feed(b []byte) { s.view.Feed(b) }

func (s *Stream) SetOptions(o textview.Options) { s.view.SetOptions(o) }
func (s *Stream) ViewOptions() textview.Options { return s.view.Options() }
func (s *Stream) ClearView()                    { s.view.Clear() }

func (s *Stream) ID() string { return "stream" }

func (s *Stream) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("TCP-Stream :%d (%d verbunden)", s.port, s.clients)
}

func (s *Stream) Dirty() bool { return s.view.Dirty() }

func (s *Stream) Render(dst *surface.Surface) {
	f, scale := s.fp()
	s.view.Render(dst, f, scale)
}
