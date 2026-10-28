// rrdpd — Remote Retro Display Daemon.
// Rendert Netzwerk-Inhalte und serielle Datenströme auf einen Composite-CRT
// (Linux-Framebuffer) und stellt Web-UI + REST-API bereit.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/SMLunchen/Remote-Retro-Display/internal/compositor"
	"github.com/SMLunchen/Remote-Retro-Display/internal/config"
	"github.com/SMLunchen/Remote-Retro-Display/internal/fonts"
	"github.com/SMLunchen/Remote-Retro-Display/internal/output"
	"github.com/SMLunchen/Remote-Retro-Display/internal/server"
	"github.com/SMLunchen/Remote-Retro-Display/internal/source"
)

func main() {
	cfgPath := flag.String("config", "config.json", "Pfad zur Konfigurationsdatei")
	noFB := flag.Bool("no-fb", false, "Framebuffer-Ausgabe deaktivieren (Dev-Modus)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	store := config.NewStore(cfg, *cfgPath)

	fontReg, fontErrs := fonts.NewRegistry(cfg.FontsDir)
	for _, e := range fontErrs {
		log.Printf("fonts: %v", e)
	}
	log.Printf("fonts: verfügbar: %v", fontReg.Names())

	fp := source.FontProvider(func() (*fonts.Font, int) {
		c := store.Snapshot()
		return fontReg.Get(c.Font.Name), c.Font.Scale
	})

	// Ausgabe: Framebuffer, wenn vorhanden — sonst nur Web-Vorschau.
	var outs []output.Output
	w, h := cfg.Width, cfg.Height
	if cfg.FB != "" && !*noFB {
		fb, err := output.OpenFramebuffer(cfg.FB)
		if err != nil {
			log.Printf("framebuffer: %v — weiter ohne Framebuffer", err)
		} else {
			w, h = fb.Size()
			outs = append(outs, fb)
			defer fb.Close()
			log.Printf("framebuffer: %s %dx%d", cfg.FB, w, h)
		}
	}
	if len(outs) == 0 {
		log.Printf("ausgabe: nur Web-Vorschau, virtuelle Größe %dx%d", w, h)
	}
	// tatsächliche Ausgabegröße in der Laufzeit-Config spiegeln (für /api/state)
	store.Update(func(c *config.Config) { c.Width, c.Height = w, h })

	// Quellen
	reg := source.NewRegistry()
	netSrc := source.NewNet(fp)
	imgSrc := source.NewImage(fp)
	testSrc := source.NewTest(fp)
	streamSrc := source.NewStream(fp, cfg.Stream.Options, cfg.Stream.Port)
	reg.Add(netSrc)
	reg.Add(imgSrc)
	reg.Add(testSrc)
	reg.Add(streamSrc)
	if err := streamSrc.Listen(); err != nil {
		log.Printf("stream: Listener auf :%d fehlgeschlagen: %v", cfg.Stream.Port, err)
	} else {
		log.Printf("stream: TCP-Listener auf :%d", cfg.Stream.Port)
	}
	defer streamSrc.Close()

	for _, sc := range cfg.Serial {
		ser := source.NewSerial(fp, sc)
		reg.Add(ser)
		if sc.Enabled {
			ser.Apply(sc)
		}
	}

	comp := compositor.New(w, h, reg, fp, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go comp.Run(ctx, outs)

	srv := server.New(store, reg, comp, fontReg, fp, netSrc, imgSrc, streamSrc, testSrc)
	httpSrv := &http.Server{Addr: cfg.Listen, Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		httpSrv.Close()
	}()
	log.Printf("http: Web-UI und API auf %s", cfg.Listen)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}
