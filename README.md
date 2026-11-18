# Boardr

Startseite fürs Homelab: alle Dienste als Kacheln, mit Status, Widgets und Suche.

**Stand: Grundgerüst.** Der Server startet und zeigt eine Startseite, die
Funktionen unten sind geplant.

## Geplant

* **Kacheln** für jeden Dienst, in Gruppen und Tabs, per Ziehen anordnen
* **Statusprüfung** je Kachel (HTTP, TCP, Ping) mit Antwortzeit und Verlauf
* **Widgets**: Proxmox (Knoten, VMs), Prometheus-Abfragen, Pi-hole, Home Assistant, Wetter, Kalender
* **Suche** über alle Kacheln und direkt in Diensten (Nexora, Paperless, Jellyfin)
* **Mehrere Boards**, sichtbar je nach Keycloak-Gruppe
* **Homelab-CA** wird vertraut, interne Dienste per IP:Port
* **Übernahme aus Homarr**: Kacheln und Anordnung aus der bestehenden SQLite einlesen
* Schnell und ohne schweres Frontend-Framework

## Schritte

1. Board mit Kacheln aus der Datenbank, Bearbeiten im Browser
2. Statusprüfung im Hintergrund
3. Gruppen, Tabs, Anordnen per Ziehen
4. Keycloak und Sichtbarkeit je Gruppe
5. Widgets (Proxmox, Prometheus, Pi-hole, Home Assistant)
6. Übernahme aus Homarr

## Starten

```bash
cp .env.beispiel .env            # Kennwort für die Datenbank eintragen
docker compose up -d --build
```

Danach unter `http://<host>:8390`. Im Homelab kommt TLS über Caddy mit
der Homelab-CA davor, der Dienst selbst lauscht dann auf `127.0.0.1:28390`.

## Entwickeln

```bash
go run .
go test ./...
```

Go 1.27.4, Datenbank PostgreSQL. Das Schema steht in `schema.sql`.

## Lizenz

MIT, siehe [LICENSE](LICENSE).
