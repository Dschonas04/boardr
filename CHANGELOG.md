# Änderungen

## Unveröffentlicht

- Oberfläche überarbeitet: Kopfzeile, Tabellen und Felder am Handy, Anmeldeseite in den Farben der App.
- Logo auf der Anmeldeseite wird wieder ausgeliefert, der Hinweis zur Ersteinrichtung stimmt.
- Sicherheit: Anmeldung, Konten und Verwaltung nur noch für Admins. Admin ist, wer sich zuerst anmeldet.
- Boards, Gruppen und Kacheln aus der Datenbank, Verwaltung im Browser (anlegen, umbenennen, ordnen, löschen).
- Statusprüfung im Hintergrund jede Minute: HTTP (unter 500 gilt als erreichbar) oder tcp://host:port, Antwortzeit, Verfügbarkeit über 24 Stunden, Verlauf 7 Tage. Die Ansicht lädt den Zustand alle 30 Sekunden nach.
- Suche über alle Kacheln, Taste `/`, Enter öffnet den ersten Treffer.
- Anmeldung über Keycloak (OIDC) oder eigene Konten; ohne Einrichtung führt jeder Weg zum ersten Konto, die App ist nie offen.
- Grundgerüst: Server mit Startseite, /healthz und /api/version, Schemaentwurf, Docker, Prüfung in GitHub Actions.
