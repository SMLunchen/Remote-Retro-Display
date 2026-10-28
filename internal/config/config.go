// Package config lädt und persistiert die Daemon-Konfiguration (config.json).
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/SMLunchen/Remote-Retro-Display/internal/source"
	"github.com/SMLunchen/Remote-Retro-Display/internal/textview"
)

type Insets struct {
	Left   int `json:"left"`
	Right  int `json:"right"`
	Top    int `json:"top"`
	Bottom int `json:"bottom"`
}

type FontCfg struct {
	Name  string `json:"name"`
	Scale int    `json:"scale"`
}

type StreamCfg struct {
	Port int `json:"port"`
	textview.Options
}

type Layout struct {
	Mode      string   `json:"mode"` // single|hsplit|vsplit
	Viewports []string `json:"viewports"`
}

type Config struct {
	Listen   string                `json:"listen"`
	FB       string                `json:"fb"`     // "" = keine Framebuffer-Ausgabe (Dev)
	Width    int                   `json:"width"`  // genutzt ohne Framebuffer
	Height   int                   `json:"height"` //
	FontsDir string                `json:"fontsDir"`
	Insets   Insets                `json:"insets"`
	Font     FontCfg               `json:"font"`
	MaxFPS   int                   `json:"maxFPS"`
	Layout   Layout                `json:"layout"`
	Stream   StreamCfg             `json:"stream"`
	Serial   []source.SerialConfig `json:"serial"`
}

func Default() *Config {
	return &Config{
		Listen:   ":8080",
		FB:       "/dev/fb0",
		Width:    720,
		Height:   576,
		FontsDir: "fonts",
		Insets:   Insets{Left: 24, Right: 24, Top: 20, Bottom: 20},
		Font:     FontCfg{Name: "builtin-7x13", Scale: 2},
		MaxFPS:   10,
		Layout:   Layout{Mode: "single", Viewports: []string{"net", "stream"}},
		Stream:   StreamCfg{Port: 2323, Options: textview.DefaultOptions()},
	}
}

// Load liest die Konfiguration; fehlende Datei liefert Defaults.
func Load(path string) (*Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	c.sanitize()
	return c, nil
}

func (c *Config) sanitize() {
	if c.MaxFPS < 1 {
		c.MaxFPS = 1
	}
	if c.MaxFPS > 30 {
		c.MaxFPS = 30
	}
	if c.Font.Scale < 1 {
		c.Font.Scale = 1
	}
	if c.Font.Scale > 8 {
		c.Font.Scale = 8
	}
	if c.Width < 64 {
		c.Width = 720
	}
	if c.Height < 64 {
		c.Height = 576
	}
	if len(c.Layout.Viewports) < 2 {
		c.Layout.Viewports = append(c.Layout.Viewports, "net", "stream")
		c.Layout.Viewports = c.Layout.Viewports[:2]
	}
	switch c.Layout.Mode {
	case "single", "hsplit", "vsplit":
	default:
		c.Layout.Mode = "single"
	}
	if c.Stream.Charset == "" {
		c.Stream.Charset = "utf8"
	}
	if c.Stream.Port == 0 {
		c.Stream.Port = 2323
	}
}

func (c *Config) Save(path string) error {
	c.sanitize()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
