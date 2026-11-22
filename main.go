// Boardr: Startseite fürs Homelab. Alle Dienste als Kacheln in Gruppen und
// Boards, jede mit laufender Statusprüfung (HTTP, TCP), dazu Suche und eine
// Verwaltung im Browser.
package main

import (
	"bytes"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"

	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type App struct {
	staende widgetStaende
	// verwaltung: Änderungen an Konten, Rollen und Anmeldung laufen nacheinander.
	verwaltung Sperre
	admins     []string // Vorgabe des Betreibers
	db         *pgxpool.Pool
	sso        *SSO
	v          Vorlagen
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &App{
		db:  datenbank(ctx, umgebung("BOARDR_DB", "postgres://boardr:boardr@localhost:5432/boardr?sslmode=disable")),
		sso: NeuesSSO("Boardr", umgebung("BOARDR_SSO_DATEI", "./einstellungen/sso.json"), "BOARDR_SSO_AUS"),
		v:   ladeVorlagen(),
	}
	a.admins = adminListe(umgebung("BOARDR_ADMIN", ""))
	a.sso.ErstesKonto = a.ersterAdmin
	a.sso.KontoWeg = a.adminEntziehen
	a.sso.Frei = []string{"/statisch/", "/healthz"}
	if err := a.beispielAnlegen(ctx); err != nil {
		log.Printf("Beispiel: %v", err)
	}
	go a.pruefer(ctx)
	go a.widgetLauf(ctx)

	adresse := ":" + umgebung("BOARDR_PORT", "8390")
	srv := &http.Server{Addr: adresse, Handler: a.wege(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("Boardr %s lauscht auf %s", version, adresse)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	ende, abbruch := context.WithTimeout(context.Background(), 10*time.Second)
	defer abbruch()
	srv.Shutdown(ende)
}

func (a *App) wege() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", gesund)
	m.HandleFunc("GET /favicon.svg", logo)
	m.HandleFunc("GET /favicon.ico", logo)
	m.Handle("GET /statisch/", statisch())
	a.sso.Wege(m, a.angemeldet)

	m.HandleFunc("GET /{$}", a.start)
	m.HandleFunc("GET /b/{id}", a.boardZeigen)
	m.HandleFunc("GET /api/zustand", a.zustandJSON)
	m.HandleFunc("GET /api/widgets", a.widgetsJSON)
	m.HandleFunc("POST /verwalten/ordnen", a.nurAdmin(a.ordnen))
	m.HandleFunc("GET /verwalten/widgets", a.nurAdmin(a.widgetSeite))
	m.HandleFunc("POST /verwalten/widgets", a.nurAdmin(a.widgetSpeichern))
	m.HandleFunc("POST /verwalten/widgets/{id}/loeschen", a.nurAdmin(a.widgetLoeschen))
	m.HandleFunc("GET /verwalten/homarr", a.nurAdmin(a.homarrSeite))
	m.HandleFunc("POST /verwalten/homarr", a.nurAdmin(a.homarrHolen))
	m.HandleFunc("GET /verwalten", a.nurAdmin(a.verwalten))
	m.HandleFunc("POST /verwalten/board", a.nurAdmin(a.boardSpeichern))
	m.HandleFunc("POST /verwalten/board/{id}/loeschen", a.nurAdmin(a.boardLoeschen))
	m.HandleFunc("POST /verwalten/gruppe", a.nurAdmin(a.gruppeSpeichern))
	m.HandleFunc("POST /verwalten/gruppe/{id}/loeschen", a.nurAdmin(a.gruppeLoeschen))
	m.HandleFunc("GET /verwalten/kachel/{id}", a.nurAdmin(a.kachelFormular))
	m.HandleFunc("POST /verwalten/kachel", a.nurAdmin(a.kachelSpeichern))
	m.HandleFunc("POST /verwalten/kachel/{id}/loeschen", a.nurAdmin(a.kachelLoeschen))
	return sicherheitskopf(nurAngemeldet(a.sso, m))
}

func (a *App) angemeldet(h http.HandlerFunc) http.HandlerFunc {
	// Anmeldung und Konten verwalten nur Admins; vor der Einrichtung, wer den
	// Code aus dem Log hat.
	darf := func(w http.ResponseWriter, r *http.Request) bool {
		if !a.sso.Geschuetzt() && a.sso.EinrichtungErlaubt(w, r) {
			return true
		}
		if _, ok := a.sso.Wer(r); !ok {
			http.Error(w, "Bitte anmelden", http.StatusUnauthorized)
			return false
		}
		if !a.istAdmin(r) {
			http.Error(w, "Nur für Admins", http.StatusForbidden)
			return false
		}
		return true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !inhaltZuerst(w, r) {
			return
		}
		// Lesen und Testen ändern nichts und brauchen keine Sperre.
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/api/sso/testen" {
			if darf(w, r) {
				h(w, r)
			}
			return
		}
		// Der Anbieter wird vor der Sperre gefragt, und nur von Admins.
		if r.URL.Path == "/api/sso/einstellungen" {
			if !darf(w, r) {
				return
			}
			roh, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Die Anfrage ist unvollständig", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(roh))
			var ok bool
			if r, ok = a.sso.ausstellerVorab(w, r, roh); !ok {
				return
			}
		}
		// Erst sperren, dann prüfen: bis zum Speichern kann niemand die Rechte
		// entziehen, auf die sich die Prüfung stützt.
		a.verwaltung.fuehreAus(w, r, func(w http.ResponseWriter, r *http.Request) {
			if darf(w, r) {
				h(w, r)
			}
		})
	}
}

func (a *App) daten(r *http.Request, titel, nav string, d any) Daten {
	n, _ := a.sso.Wer(r)
	return Daten{App: "Boardr", Titel: titel, Nav: nav, Nutzer: n, Admin: a.istAdmin(r), D: d}
}
