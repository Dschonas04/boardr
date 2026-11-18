package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Jede Seite muss sich mit typischen Daten rendern lassen: ein Fehler in einer
// Vorlage fiele sonst erst im Betrieb auf.
func TestSeitenRendern(t *testing.T) {
	v := ladeVorlagen()
	nav := navDaten{[]Board{{ID: "b1", Name: "Start"}}}
	k := Kachel{ID: "k1", Gruppe: "g1", Titel: "Grafana", Adresse: "https://10.0.2.7:3030", Pruefen: true,
		Geprueft: true, OK: true, MS: 12, Meldung: "HTTP 200", Seit: time.Now(), Symbol: "https://x/y.svg"}
	g := []Gruppe{{ID: "g1", Board: "b1", Name: "Monitoring", Kacheln: []Kachel{k}}}
	faelle := map[string]any{
		"board": struct {
			navDaten
			Name        string
			Gruppen     []Gruppe
			Gesamt, Gut int
		}{nav, "Start", g, 1, 1},
		"verwalten": struct {
			navDaten
			Liste []struct {
				Board
				Gruppen []Gruppe
			}
		}{nav, []struct {
			Board
			Gruppen []Gruppe
		}{{Board{ID: "b1", Name: "Start"}, g}}},
		"kachel": struct {
			navDaten
			K       Kachel
			Gruppen []struct{ ID, Name string }
		}{nav, k, []struct{ ID, Name string }{{"g1", "Start / Monitoring"}}},
	}
	for name, d := range faelle {
		rec := httptest.NewRecorder()
		v.zeigen(rec, httptest.NewRequest("GET", "/", nil), name, Daten{App: "Boardr", D: d})
		if !strings.Contains(rec.Body.String(), "</html>") || strings.Contains(rec.Body.String(), "Vorlage") {
			t.Errorf("%s: %s", name, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Grafana") && name != "verwalten" {
			t.Errorf("%s: Kachel fehlt", name)
		}
	}
}

func TestPruefeTCPundKurz(t *testing.T) {
	if ok, _, _ := pruefe(t.Context(), "tcp://127.0.0.1:1"); ok {
		t.Error("Port 1 sollte zu sein")
	}
	if kurz(errString("dial tcp: connection refused")) != "Verbindung abgelehnt" {
		t.Error("kurz")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
