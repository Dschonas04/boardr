package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Wert struct {
	Name string `json:"name"`
	Wert string `json:"wert"`
}

type WidgetStand struct {
	Werte  []Wert `json:"werte"`
	Fehler string `json:"fehler,omitempty"`
}

type Widget struct {
	ID, Board, Art, Titel, Adresse, Abfrage, Geheimnis string
	Reihe                                              int
	Stand                                              WidgetStand
}

var WidgetArten = []struct{ Art, Name, Hilfe string }{
	{"prometheus", "Prometheus", "Adresse des Servers; Abfrage: PromQL"},
	{"proxmox", "Proxmox", "Adresse mit Port 8006; Geheimnis: API-Token (benutzer@realm!name=wert)"},
	{"pihole", "Pi-hole 6", "Adresse des Pi-hole; Geheimnis: App-Passwort"},
	{"homeassistant", "Home Assistant", "Adresse; Abfrage: Entitäten mit Komma; Geheimnis: Token"},
	{"wetter", "Wetter", "Abfrage: Breite,Länge (geht an open-meteo.com)"},
	{"kalender", "Kalender", "Adresse einer ICS-Datei, einzelne Termine (keine Serien)"},
}

func widgetArt(art string) bool {
	for _, a := range WidgetArten {
		if a.Art == art {
			return true
		}
	}
	return false
}

var geheimnisName = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_]{0,39}$`)

func herkunft(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return u.Scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// widgetGeheimnis liest BOARDR_WIDGET_<NAME> und gibt es nur für den Host aus
// BOARDR_WIDGET_<NAME>_ZIEL heraus: wer Boards verwaltet, kann ein Token so
// nicht an eine andere Adresse schicken lassen.
func widgetGeheimnis(name, adresse string) (string, error) {
	if !geheimnisName.MatchString(name) || strings.HasSuffix(name, "_ZIEL") {
		return "", errors.New("Geheimnis fehlt")
	}
	v := "BOARDR_WIDGET_" + name
	wert, ziel := os.Getenv(v), os.Getenv(v+"_ZIEL")
	if wert == "" || ziel == "" {
		return "", errors.New(v + " oder " + v + "_ZIEL fehlt")
	}
	soll, err1 := url.Parse(ziel)
	ist, err2 := url.Parse(adresse)
	if err1 != nil || err2 != nil || ist.Host == "" || ist.User != nil || herkunft(soll) != herkunft(ist) {
		return "", errors.New("Das Geheimnis gilt nur für seinen Host")
	}
	return wert, nil
}

// widgetStaende hält den letzten Stand je Widget.
type widgetStaende struct {
	mu sync.RWMutex
	m  map[string]WidgetStand
}

func (s *widgetStaende) lesen(id string) WidgetStand {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if x, ok := s.m[id]; ok {
		return x
	}
	return WidgetStand{Werte: []Wert{}}
}

func (a *App) widgetsVon(ctx context.Context, boards []string) ([]Widget, error) {
	rows, err := a.db.Query(ctx, `SELECT id::text, board::text, art, titel, adresse, abfrage, geheimnis, reihe
		FROM widgets WHERE board::text = ANY($1::text[]) ORDER BY reihe, titel`, boards)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []Widget
	for rows.Next() {
		var w Widget
		if err := rows.Scan(&w.ID, &w.Board, &w.Art, &w.Titel, &w.Adresse, &w.Abfrage, &w.Geheimnis, &w.Reihe); err != nil {
			return nil, err
		}
		w.Stand = a.staende.lesen(w.ID)
		l = append(l, w)
	}
	return l, rows.Err()
}

var widgetAnstoss = make(chan struct{}, 1)

func (a *App) widgetLauf(ctx context.Context) {
	takt := time.NewTicker(time.Minute)
	defer takt.Stop()
	for {
		a.widgetsHolen(ctx)
		select {
		case <-ctx.Done():
			return
		case <-takt.C:
		case <-widgetAnstoss:
		}
	}
}

func (a *App) widgetsHolen(ctx context.Context) {
	l, err := a.widgetsVon(ctx, boardIDs(a.boards(ctx)))
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("Widgets: %v", err)
		}
		return
	}
	neu := make(map[string]WidgetStand, len(l))
	var mu sync.Mutex
	var wg sync.WaitGroup
	frei := make(chan struct{}, 6)
	for _, w := range l {
		frei <- struct{}{}
		wg.Go(func() {
			defer func() { <-frei }()
			einmal, ende := context.WithTimeout(ctx, 12*time.Second)
			defer ende()
			s := WidgetStand{Werte: []Wert{}}
			if werte, err := widgetHolen(einmal, w); err != nil {
				s.Fehler = err.Error()
			} else if werte != nil {
				s.Werte = werte
			}
			mu.Lock()
			neu[w.ID] = s
			mu.Unlock()
		})
	}
	wg.Wait()
	a.staende.mu.Lock()
	a.staende.m = neu
	a.staende.mu.Unlock()
}

func (a *App) widgetsJSON(w http.ResponseWriter, r *http.Request) {
	l, err := a.widgetsVon(r.Context(), boardIDs(a.meineBoards(r)))
	if err != nil {
		alsJSON(w, http.StatusInternalServerError, map[string]string{"fehler": "Datenbank"})
		return
	}
	raus := map[string]WidgetStand{}
	for _, x := range l {
		raus[x.ID] = x.Stand
	}
	alsJSON(w, http.StatusOK, raus)
}

func (a *App) widgetSeite(w http.ResponseWriter, r *http.Request) {
	boards := a.boards(r.Context())
	l, err := a.widgetsVon(r.Context(), boardIDs(boards))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	neu := Widget{Board: r.URL.Query().Get("board"), Art: "prometheus"}
	for _, x := range l {
		if x.ID == r.URL.Query().Get("id") {
			neu = x
		}
	}
	a.v.zeigen(w, r, "widgets", a.daten(r, "Widgets", "verwalten", struct {
		navDaten
		Liste []Widget
		W     Widget
		Arten any
	}{navDaten{boards}, l, neu, WidgetArten}))
}

func (a *App) widgetSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	const hier = "/verwalten/widgets"
	feld := func(n string) string { return strings.TrimSpace(r.FormValue(n)) }
	art, titel, adresse, geheim := feld("art"), feld("titel"), strings.TrimRight(feld("adresse"), "/"), strings.ToUpper(feld("geheimnis"))
	if !widgetArt(art) || titel == "" {
		zurueck(w, r, hier, "Art und Titel gehören dazu")
		return
	}
	if art == "wetter" {
		adresse, geheim = "", ""
	} else if u, err := url.Parse(adresse); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		zurueck(w, r, hier, "Die Adresse muss mit http:// oder https:// beginnen")
		return
	}
	// Nur ein Name: wer hier ein Token hineinkopiert, speichert es nicht.
	if geheim != "" && !geheimnisName.MatchString(geheim) {
		zurueck(w, r, hier, "Beim Geheimnis steht nur der Name der Variable, nicht das Token")
		return
	}
	var err error
	if id := feld("id"); id != "" {
		_, err = a.db.Exec(r.Context(), `UPDATE widgets SET board=$2::uuid, art=$3, titel=$4, adresse=$5, abfrage=$6, geheimnis=$7, reihe=$8 WHERE id::text=$1`,
			id, feld("board"), art, titel, adresse, feld("abfrage"), geheim, zahl(feld("reihe")))
	} else {
		_, err = a.db.Exec(r.Context(), `INSERT INTO widgets (board, art, titel, adresse, abfrage, geheimnis, reihe) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7)`,
			feld("board"), art, titel, adresse, feld("abfrage"), geheim, zahl(feld("reihe")))
	}
	if err != nil {
		zurueck(w, r, hier, "Nicht gespeichert")
		return
	}
	select {
	case widgetAnstoss <- struct{}{}:
	default:
	}
	zurueck(w, r, hier, "Widget gespeichert")
}

func (a *App) widgetLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM widgets WHERE id::text=$1`, r.PathValue("id")); err != nil {
		zurueck(w, r, "/verwalten/widgets", "Nicht gelöscht")
		return
	}
	zurueck(w, r, "/verwalten/widgets", "Widget gelöscht")
}
