# Remote-RetroDisplay

Ein Raspberry Pi 1 treibt über Composite-Out einen **Commodore DM602**
(monochrom, grün). Der Daemon `rrdpd` rendert Netzwerk-Inhalte (Text, Tabellen,
Diagramme, Bilder) und serielle Datenströme direkt in den Linux-Framebuffer —
gesteuert über ein Web-UI und eine REST-API.

- Konzept: [KONZEPT.md](KONZEPT.md)
- Protokoll: [docs/PROTOKOLL.md](docs/PROTOKOLL.md)

## Features (v0)

- **Quellen**: Netzwerk-Screens (Widgets: Text, Tabelle, Chart, Progress, Box),
  Bild-Push (PNG/JPEG/GIF), TCP-Log-Stream (Port 2323, „Logs hinballern" per
  `nc`/`telnet`), serielle Ports (USB-RS232), Testbilder
- **Split-Screen**: Vollbild, nebeneinander, übereinander — Quelle je Viewport
- **Terminal-Darstellung** mit zuschaltbaren C0-Steuerzeichen, ANSI-Subset
  (Farben → Helligkeitsstufen), Zeilenumbruch, UTF-8/Latin-1, Telnet-IAC-Filter
- **Schriften konfigurierbar**: eingebauter 7×13-Font + beliebige
  PSF/PSF2-Konsolenfonts (z. B. Terminus) aus `fonts/`, frei skalierbar
- **Web-UI** mit Live-Vorschau des CRT-Bilds, Quellen-/Layout-Umschaltung,
  Seriell-Konfiguration, Overscan-Einstellung, Testbildern

## Bauen

Benötigt Go ≥ 1.23. Für den Pi 1 (ARMv6) cross-kompilieren:

```powershell
# Windows (PowerShell)
$env:GOOS="linux"; $env:GOARCH="arm"; $env:GOARM="6"
go build -ldflags "-s -w" -o rrdpd ./cmd/rrdpd
```

```sh
# Linux/macOS
GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "-s -w" -o rrdpd ./cmd/rrdpd
```

Lokal entwickeln (ohne Framebuffer, Vorschau im Browser):

```sh
go run ./cmd/rrdpd -no-fb
# → http://localhost:8080
```

## Pi einrichten

Raspberry Pi OS **Lite** (32-bit). In `/boot/config.txt` (bzw.
`/boot/firmware/config.txt`):

```ini
# Composite NTSC-Timing (60 Hz), 4:3, ohne Farbträger (schärferes Monochrombild!)
# Der DM602 stammt aus der Commodore-PC-Ära (CGA-Composite = 60 Hz) und
# rastet bei 50 Hz PAL vertikal nicht ein (durchlaufendes Bild).
sdtv_mode=0
sdtv_aspect=1
sdtv_disable_colourburst=1
disable_overscan=1
```

Framebuffer ist dann 720×480 (statt 720×576) — `rrdpd` übernimmt die Größe
automatisch aus sysfs. Läuft das Bild weiterhin (nur langsamer), die
**V-HOLD-Regelung** am Monitor nachstellen. Für Monitore, die wirklich 50 Hz
wollen: `sdtv_mode=2` (PAL). `enable_tvout` gibt es erst ab Pi 4 und ist auf
dem Pi 1 wirkungslos.

In `/boot/cmdline.txt` anhängen (Konsolen-Blanking und Cursor aus):

```
consoleblank=0 vt.global_cursor_default=0
```

Login-Konsole auf tty1 abschalten, damit nichts ins Bild schreibt:

```sh
sudo systemctl disable getty@tty1
```

Deployment:

```sh
sudo mkdir -p /opt/rrdpd/fonts
sudo cp rrdpd config.example.json /opt/rrdpd/
sudo mv /opt/rrdpd/config.example.json /opt/rrdpd/config.json
# Wichtig nach scp von Windows: Execute-Bit geht beim Kopieren verloren
sudo chmod +x /opt/rrdpd/rrdpd
# optional schönere Schriften:
cp /usr/share/consolefonts/Lat15-TerminusBold16.psf.gz /opt/rrdpd/fonts/
sudo cp deploy/rrdpd.service /etc/systemd/system/
sudo systemctl enable --now rrdpd
```

Overscan-Ränder im Web-UI justieren (Testbild „Gitter/Geometrie").

## Schnelltest

```sh
# Logs auf den CRT ballern
journalctl -f | nc <pi> 2323

# Text per REST
curl -X POST --data-binary "Hallo DM602!" http://<pi>:8080/api/print

# Bild anzeigen
curl -X POST --data-binary @plot.png "http://<pi>:8080/api/image?show=1"

# Dashboard-Screen
curl -X POST -H "Content-Type: application/json" http://<pi>:8080/api/screen -d '{
  "name": "demo", "activate": true,
  "widgets": [
    {"id":"t","type":"text","x":0,"y":0,"size":3,"text":"WETTER"},
    {"id":"tb","type":"table","x":0,"y":4,"cols":["Sensor","Wert"],
     "rows":[["Temp","12.4 C"],["Wind","3 m/s"]]},
    {"id":"ch","type":"chart","x":0,"y":12,"w":40,"h":8,"kind":"line",
     "series":[1.2,1.4,1.1,1.8],"label":"Temp 24h"}
  ]}'

# Danach nur noch Daten nachschieben:
curl -X POST -H "Content-Type: application/json" http://<pi>:8080/api/screen \
  -d '{"name":"demo","update":{"id":"ch","append":2.1}}'
```

## Lizenz

AGPL-3.0 — siehe [LICENSE](LICENSE).
