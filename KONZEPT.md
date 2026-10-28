# Remote-RetroDisplay — Konzept

Ein Raspberry Pi 1 bedient über seinen Composite-Ausgang einen Commodore DM602
(monochrom, grün). Der anzuzeigende Inhalt kommt über das Netzwerk oder von
lokalen seriellen Schnittstellen (USB-RS232-Wandler).

## Architektur

Auf dem Pi läuft **ein einziger Daemon** (`rrdpd`, Go, statisches ARMv6-Binary),
der direkt in den Linux-Framebuffer (`/dev/fb0`) rendert — kein X, kein Browser.
Alle Clients bleiben „dumm": sie schicken JSON, Text oder Bilder; gerendert wird
auf dem Pi.

```
                        ┌──────────────────────────────────────────┐
  curl / Skripte  ────► │ REST-API (HTTP :8080)                    │
  Browser         ────► │ Web-UI + Live-Vorschau                   │      ┌─────────┐
  nc / telnet     ────► │ TCP-Stream (:2323)          Compositor   │ ───► │ /dev/fb0 │ ───► DM602
  USB-RS232       ────► │ Serial-Reader               (Split/Full) │      └─────────┘
                        └──────────────────────────────────────────┘
```

### Komponenten

| Komponente   | Aufgabe |
|--------------|---------|
| `surface`    | 8-Bit-Graustufen-Zeichenfläche, Primitive (Linien, Rechtecke, Text) |
| `fonts`      | Bitmap-Fonts: eingebauter Fallback + **PSF/PSF2-Loader** (Linux-Konsolenfonts, z. B. Terminus), Skalierungsfaktor |
| `textview`   | Terminal-artige Textansicht: Zeilenumbruch, Scrolling, C0-Steuerzeichen, ANSI/VT100-Subset, Telnet-IAC-Filter |
| `source`     | Quellen: `net` (Screens/Widgets), `image` (Bild-Push), `stream` (TCP-Logs), `serial:<port>`, `test` (Testbilder) |
| `compositor` | Layout (Vollbild, horizontaler/vertikaler Split), Overscan-Ränder, Frame-Loop mit Dirty-Tracking |
| `output`     | Framebuffer-Ausgabe (Linux); auf anderen Plattformen nur Web-Vorschau (Dev-Modus) |
| `server`     | REST-API, Web-UI (eingebettet), Live-Vorschau als Multipart-PNG-Stream |

## Quellen-Konzept

Der Daemon kennt mehrere **Quellen**; jedes Viewport des Layouts zeigt genau eine:

- **`net`** — benannte Screens aus Widgets (Text, Tabelle, Diagramm, Fortschrittsbalken, Box), per REST befüllt und partiell aktualisierbar.
- **`image`** — zuletzt gepushtes Vollbild (PNG/JPEG/GIF), serverseitig skaliert und nach Graustufen gewandelt.
- **`stream`** — TCP-Listener (Port 2323): „Logs einfach hinballern" per `nc`, `telnet`, Syslog-Weiterleitung o. ä. Darstellung als scrollendes Terminal.
- **`serial:<port>`** — je USB-RS232-Wandler eine Quelle; Baudrate/Parität/Stopbits konfigurierbar; gleiche Terminal-Darstellung wie `stream`.
- **`test`** — Testbilder (Gitter/Geometrie, Grauverlauf, Font-Muster) zum Justieren des CRT.

Umgeschaltet wird pro Viewport über das Web-UI **oder** die API — auch ein
entferntes System kann den Monitor also umschalten.

## Split-Screen

Drei Layout-Modi: `single`, `hsplit` (nebeneinander), `vsplit` (übereinander).
Jedes Viewport bekommt eine Quelle zugewiesen. Trennlinie 2 px (Interlace-fest).
Text-Quellen reflowen ihren Puffer beim Größenwechsel (Ringpuffer der letzten
Rohbytes wird neu eingespeist).

## Textdarstellung (Stream & Seriell)

ASCII sieht Steuerzeichen tatsächlich vor (C0-Bereich 0x00–0x1F): LF, CR, TAB,
BS, FF (Bildschirm löschen), BEL, und ESC (0x1B) als Einstieg in
ANSI/VT100-Sequenzen. Genau darauf baut die Darstellung auf — **zuschaltbar**:

| Option     | Wirkung |
|------------|---------|
| `wrap`     | automatischer Zeilenumbruch am Zeilenende (aus: abschneiden bis LF) |
| `controls` | C0-Steuerzeichen (TAB, BS, FF, …) interpretieren; aus: als `·` sichtbar. LF/CR wirken immer. |
| `ansi`     | ESC-Sequenzen interpretieren (Cursor, Löschen, SGR); aus: Sequenzen werden verschluckt. ANSI-**Farben** werden auf **Helligkeitsstufen** des grünen Phosphors abgebildet. |
| `charset`  | `utf8` (Standard) oder `latin1` |

Telnet-Negotiation (IAC-Bytes 0xFF …) wird transparent herausgefiltert, damit
sowohl `telnet` als auch `nc` funktionieren.

## CRT-/DM602-Besonderheiten

- `config.txt`: `sdtv_mode=2` (PAL), `sdtv_aspect=1`, **`sdtv_disable_colourburst=1`**
  (ohne Farbträger deutlich schärferes Monochrombild).
- 576i flimmert bei 1-px-Strukturen → Fonts/Linien mindestens 2 px stark
  (Standard: Font-Skalierung ≥ 2, Trennlinien 2 px).
- Kein Antialiasing — Bitmap-Fonts, hoher Kontrast.
- Overscan: der DM602 schneidet Ränder ab → einstellbare Insets im Web-UI,
  Testbild `grid` zum Ausmessen.
- Monochrom: alles wird als Luminanz gerendert; „Farben" = Helligkeitsstufen.

## Schriftarten

Konfigurierbar: eingebauter 7×13-Font als Fallback plus beliebige
PSF/PSF2-Konsolenfonts (auch `.gz`) aus dem `fonts/`-Verzeichnis — auf dem Pi
liegen unter `/usr/share/consolefonts/` Dutzende (Terminus in vielen Größen).
Globale Schrift + Skalierung über Web-UI/API; Widgets können zusätzlich eine
eigene Größe (`size`) setzen.

## Protokoll

Siehe [docs/PROTOKOLL.md](docs/PROTOKOLL.md). Kurzfassung — drei Ebenen,
von universell bis komfortabel:

0. **Rohbild**: `POST /api/image` mit PNG/JPEG → anzeigen. Funktioniert mit allem.
1. **Text**: `POST /api/print` oder TCP :2323 → Terminal-Ansicht.
2. **Screens/Widgets**: `POST /api/screen` mit JSON (Text, Tabelle, Chart, …),
   partielle Updates pro Widget-ID, mehrere benannte Screens.

Geplant (nicht in v0): WebSocket-Push, MQTT-Bridge, Screen-Rotation,
Split in mehr als 2 Viewports.

## Deployment

Cross-Compile vom Entwicklungsrechner: `GOOS=linux GOARCH=arm GOARM=6`.
Ein Binary + `config.json` + optional `fonts/` auf den Pi, systemd-Unit
(`deploy/rrdpd.service`). Details im [README.md](README.md).
