# RRDP — Remote Retro Display Protocol (v0)

Transport: **HTTP/REST** auf Port `:8080` (konfigurierbar) und ein **roher
TCP-Textstream** auf Port `:2323`. Alle JSON-Bodies sind UTF-8.
Antworten: `200` + `{"ok":true,...}` oder `4xx/5xx` + `{"ok":false,"error":"..."}`.

Koordinaten in Widgets sind **Zeichenzellen** (Spalte/Zeile) der globalen
Schrift, nicht Pixel. `size` ist ein ganzzahliger Skalierungsfaktor der Schrift.
Helligkeit/„Farbe" ist ein Grauwert `0–255` (der Monitor macht daraus Grün).

---

## Ebene 0 — Rohbild

```
POST /api/image            Body: PNG/JPEG/GIF (Content-Type egal)
POST /api/image?show=1     … und Viewport 0 auf die Quelle "image" schalten
```

Das Bild wird nach Graustufen gewandelt und proportional auf das Viewport
skaliert (Nearest-Neighbor). Damit funktioniert jede Quelle, die ein Bild
erzeugen kann (matplotlib, Grafana-Renderer, Screenshots, …).

## Ebene 1 — Text

```
POST /api/print            Body: text/plain → wird an die Stream-Ansicht angehängt
TCP  :2323                 nc <pi> 2323, telnet <pi> 2323, Syslog-Pipe, …
```

Beides landet in derselben Terminal-Ansicht (Quelle `stream`): Scrolling,
Zeilenumbruch, C0-Steuerzeichen, ANSI-Subset — alles zuschaltbar (siehe
`stream`-Konfiguration). Telnet-IAC-Negotiation wird gefiltert.

Interpretierte Steuerzeichen (bei `controls:true`): `LF` `CR` `TAB` (8er-Stopps)
`BS` `FF` (löschen) `BEL` (ignoriert). LF/CR wirken immer.
ANSI-Subset (bei `ansi:true`): Cursor `CUP/CUU/CUD/CUF/CUB` (`ESC[H`, `A–D`),
Löschen `ED`/`EL` (`J`/`K`), `SGR` (`m`: Farben→Helligkeit, bold, dim, invers,
reset), `ESC c` (Reset). Unbekannte Sequenzen werden verschluckt.

## Ebene 2 — Screens & Widgets

### Screen definieren / ersetzen

```
POST /api/screen
{
  "name": "wetter",              // Default: "default"
  "activate": true,              // diesen Screen anzeigen
  "widgets": [ ... ]             // ersetzt den Screen-Inhalt komplett
}
```

Nur `{"name":"wetter","activate":true}` (ohne `widgets`) wechselt den aktiven
Screen. `DELETE /api/screen?name=wetter` löscht einen Screen.

### Partielles Update (kleine Payload, wenig Pi-Last)

```
POST /api/screen
{ "name": "wetter", "update": { "id": "verlauf", "append": 2.1 } }
```

`update` adressiert ein Widget per `id`; mitgeschickte Felder werden gemerged.
Spezialfeld `append` (Zahl oder Array) hängt an `series` an und kappt auf
`maxPoints` (Default 200).

### Widget-Katalog

Gemeinsame Felder: `id`, `type`, `x`, `y` (Zellen), optional `w`, `h` (Zellen),
`size` (Font-Skalierung, Default global), `brightness` (0–255, Default 255),
`invert` (bool).

| type       | Felder | Darstellung |
|------------|--------|-------------|
| `text`     | `text` (mehrzeilig ok), `align` (`left/center/right`, braucht `w`) | Textblock |
| `table`    | `cols` `[..]`, `rows` `[[..],..]` | Kopfzeile invertiert, Spaltenbreiten automatisch |
| `chart`    | `kind` (`line`/`bar`), `series` `[..]`, `min`, `max`, `label`, `w`, `h` | Rahmen, Min/Max-Beschriftung |
| `progress` | `value` (0–100), `label`, `w` | Balken mit Prozentangabe |
| `box`      | `w`, `h`, `fill` (bool) | Rahmen/Fläche, 2 px Linien |

Beispiel:

```json
{
  "name": "wetter", "activate": true,
  "widgets": [
    { "id": "t",  "type": "text", "x": 0, "y": 0, "size": 3, "text": "WETTER" },
    { "id": "tb", "type": "table", "x": 0, "y": 4,
      "cols": ["Sensor", "Wert"], "rows": [["Temp", "12.4 C"], ["Wind", "3 m/s"]] },
    { "id": "ch", "type": "chart", "x": 0, "y": 12, "w": 44, "h": 8,
      "kind": "line", "series": [1.2, 1.4, 1.1, 1.8], "label": "Temp 24h" }
  ]
}
```

---

## Steuerung

### Layout / Split-Screen

```
POST /api/layout
{ "mode": "vsplit", "viewports": ["net", "stream"] }   // mode: single | hsplit | vsplit
```

`viewports[i]` = Quellen-ID für Viewport i (`single` nutzt nur Index 0).
Quellen-IDs: `net`, `image`, `stream`, `test`, `serial:<port>`.

### Serielle Schnittstellen

```
GET  /api/serial/ports      → {"ports":["COM3","/dev/ttyUSB0",...]}
POST /api/serial            → Quelle anlegen/ändern (und bei enabled:true öffnen)
{ "port": "/dev/ttyUSB0", "baud": 115200, "databits": 8,
  "parity": "none|even|odd", "stopbits": 1, "enabled": true }
```

### Testbilder

```
POST /api/test              { "pattern": "grid" | "bars" | "font" }
```

### Konfiguration & Status

```
GET  /api/state             → Gesamtzustand (Layout, Quellen, Screens, Fonts, Config)
GET  /api/fonts             → verfügbare Schriften
POST /api/config            → Teil-Update, wird in config.json persistiert
{ "font": {"name": "ter-v16b", "scale": 2},
  "insets": {"left":24,"right":24,"top":20,"bottom":20},
  "maxFPS": 10,
  "stream": {"wrap":true,"ansi":true,"controls":true,"charset":"utf8"} }
```

### Vorschau

```
GET /api/preview.png        → aktueller Frame als PNG (einmalig)
GET /preview                → Multipart-PNG-Stream (für <img>, ~1 fps)
```

---

## Geplant (nach v0)

- WebSocket (`/ws`) für Push-Updates und Events
- MQTT-Bridge (Topics → Widgets/Screens)
- Screen-Rotation (`{"rotate":{"screens":[...],"interval":10}}`)
- `hello`/Capabilities-Endpoint für selbstanpassende Clients
- Mehr als 2 Viewports, freie Grid-Layouts
