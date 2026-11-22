// Boardr: Startseite fürs Homelab. Alle Dienste als Kacheln in Gruppen und
// Boards, jede mit laufender Statusprüfung (HTTP, TCP), dazu Suche und eine
// Verwaltung im Browser.
package main

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type App struct {
	// verwaltung: Änderungen an Konten, Rollen und Anmeldung laufen nacheinander.
	verwaltung sync.Mutex
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
	return func(w http.ResponseWriter, r *http.Request) {
		if !inhaltZuerst(w, r) {
			return
		}
		// Erst sperren, dann prüfen: bis zum Speichern kann niemand die Rechte
		// entziehen, auf die sich die Prüfung stützt.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			a.verwaltung.Lock()
			defer a.verwaltung.Unlock()
		}
		// Anmeldung und Konten verwalten nur Admins; vor der Einrichtung, wer
		// den Code aus dem Log hat.
		if !a.sso.Geschuetzt() && a.sso.EinrichtungErlaubt(w, r) {
			h(w, r)
			return
		}
		if _, ok := a.sso.Wer(r); !ok {
			http.Error(w, "Bitte anmelden", http.StatusUnauthorized)
			return
		}
		if !a.istAdmin(r) {
			http.Error(w, "Nur für Admins", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func (a *App) daten(r *http.Request, titel, nav string, d any) Daten {
	n, _ := a.sso.Wer(r)
	return Daten{App: "Boardr", Titel: titel, Nav: nav, Nutzer: n, Admin: a.istAdmin(r), D: d}
}
