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
	"syscall"
	"time"
)

type App struct {
	db  *pgxpool.Pool
	sso *SSO
	v   Vorlagen
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &App{
		db:  datenbank(ctx, umgebung("BOARDR_DB", "postgres://boardr:boardr@localhost:5432/boardr?sslmode=disable")),
		sso: NeuesSSO("Boardr", umgebung("BOARDR_SSO_DATEI", "./einstellungen/sso.json"), "BOARDR_SSO_AUS"),
		v:   ladeVorlagen(),
	}
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
	m.Handle("GET /statisch/", statisch())
	a.sso.Wege(m, a.angemeldet)

	m.HandleFunc("GET /{$}", a.start)
	m.HandleFunc("GET /b/{id}", a.boardZeigen)
	m.HandleFunc("GET /api/zustand", a.zustandJSON)
	m.HandleFunc("GET /verwalten", a.verwalten)
	m.HandleFunc("POST /verwalten/board", a.boardSpeichern)
	m.HandleFunc("POST /verwalten/board/{id}/loeschen", a.boardLoeschen)
	m.HandleFunc("POST /verwalten/gruppe", a.gruppeSpeichern)
	m.HandleFunc("POST /verwalten/gruppe/{id}/loeschen", a.gruppeLoeschen)
	m.HandleFunc("GET /verwalten/kachel/{id}", a.kachelFormular)
	m.HandleFunc("POST /verwalten/kachel", a.kachelSpeichern)
	m.HandleFunc("POST /verwalten/kachel/{id}/loeschen", a.kachelLoeschen)
	return sicherheitskopf(nurAngemeldet(a.sso, m))
}

// angemeldet schützt die SSO-Einstellungen: nur wer angemeldet ist, oder
// solange noch gar nichts eingerichtet ist.
func (a *App) angemeldet(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.sso.Wer(r); ok || (!a.sso.Geschuetzt() && a.sso.EinrichtungErlaubt(w, r)) {
			h(w, r)
			return
		}
		http.Error(w, "Bitte anmelden", http.StatusUnauthorized)
	}
}

func (a *App) daten(r *http.Request, titel, nav string, d any) Daten {
	n, _ := a.sso.Wer(r)
	return Daten{App: "Boardr", Titel: titel, Nav: nav, Nutzer: n, D: d}
}
