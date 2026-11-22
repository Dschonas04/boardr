package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Board struct {
	ID    string
	Name  string
	Reihe int
	Fuer  string // Gruppen mit Komma, leer: alle
}

type Kachel struct {
	ID           string
	Gruppe       string
	Titel        string
	Adresse      string
	Beschreibung string
	Symbol       string
	Pruefen      bool
	PruefAdresse string
	Reihe        int
	// Zustand
	Geprueft bool
	OK       bool
	MS       int
	Meldung  string
	Seit     time.Time
	// Anteil erfolgreicher Prüfungen in den letzten 24 Stunden, in Prozent.
	Verfuegbar int
	HatVerlauf bool
	Suche      string // Adresse mit {q}
}

// SymbolBild sagt, ob das Symbol eine Bildadresse ist.
func (k Kachel) SymbolBild() bool {
	return strings.HasPrefix(k.Symbol, "https://") || strings.HasPrefix(k.Symbol, "http://") || strings.HasPrefix(k.Symbol, "/")
}

// Zeichen ist das, was ohne Bild in der Kachel steht.
func (k Kachel) Zeichen() string {
	if k.Symbol != "" && !k.SymbolBild() {
		return k.Symbol
	}
	for _, r := range k.Titel {
		return strings.ToUpper(string(r))
	}
	return "?"
}

type Gruppe struct {
	ID      string
	Board   string
	Name    string
	Reihe   int
	Kacheln []Kachel
}

type navDaten struct {
	Boards []Board
}

func (a *App) boards(ctx context.Context) []Board {
	rows, err := a.db.Query(ctx, `SELECT id::text, name, reihe, sichtbar_fuer FROM boards ORDER BY reihe, name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var l []Board
	for rows.Next() {
		var b Board
		if rows.Scan(&b.ID, &b.Name, &b.Reihe, &b.Fuer) == nil {
			l = append(l, b)
		}
	}
	return l
}

// gruppen lädt die Gruppen eines Boards mit Kacheln und Zustand; leer heißt alle.
func (a *App) gruppen(ctx context.Context, board string) ([]Gruppe, error) {
	rows, err := a.db.Query(ctx, `
		SELECT g.id::text, g.board::text, g.name, g.reihe,
		       k.id::text, k.titel, k.adresse, k.beschreibung, k.symbol, k.pruefen, k.pruef_adresse, k.reihe,
		       z.ok, z.ms, z.meldung, z.geprueft,
		       (SELECT round(100.0 * count(*) FILTER (WHERE p.ok) / nullif(count(*), 0))::int
		          FROM pruefungen p WHERE p.kachel = k.id AND p.zeit > now() - interval '24 hours'),
		       COALESCE(k.suche, '')
		  FROM gruppen g
		  LEFT JOIN kacheln k ON k.gruppe = g.id
		  LEFT JOIN zustand z ON z.kachel = k.id
		 WHERE ($1 = '' OR g.board::text = $1)
		 ORDER BY g.reihe, g.name, k.reihe, k.titel`, board)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []Gruppe
	for rows.Next() {
		var g Gruppe
		var kid, titel, adr, beschr, sym, padr *string
		var pruefen *bool
		var reihe *int
		var ok *bool
		var ms *int
		var meldung *string
		var seit *time.Time
		var verf *int
		var suche string
		if err := rows.Scan(&g.ID, &g.Board, &g.Name, &g.Reihe, &kid, &titel, &adr, &beschr, &sym, &pruefen, &padr, &reihe,
			&ok, &ms, &meldung, &seit, &verf, &suche); err != nil {
			return nil, err
		}
		if len(l) == 0 || l[len(l)-1].ID != g.ID {
			l = append(l, g)
		}
		if kid == nil {
			continue
		}
		k := Kachel{ID: *kid, Gruppe: g.ID, Titel: *titel, Adresse: *adr, Beschreibung: *beschr, Symbol: *sym,
			Pruefen: *pruefen, PruefAdresse: *padr, Reihe: *reihe, Suche: suche}
		if ok != nil {
			k.Geprueft, k.OK, k.MS, k.Meldung, k.Seit = true, *ok, *ms, *meldung, *seit
		}
		if verf != nil {
			k.Verfuegbar, k.HatVerlauf = *verf, true
		}
		l[len(l)-1].Kacheln = append(l[len(l)-1].Kacheln, k)
	}
	return l, rows.Err()
}

// ---------- Ansicht ----------

type boardSeite struct {
	navDaten
	ID          string
	Name        string
	Gruppen     []Gruppe
	Gesamt, Gut int
	Kacheln     int
	Andere      []Kachel // von den anderen Boards, nur für die Suche
	Suchen      []Kachel // Dienste, in denen sich suchen lässt
	Widgets     []Widget
}

func (a *App) start(w http.ResponseWriter, r *http.Request) {
	b := a.meineBoards(r)
	if len(b) > 0 {
		http.Redirect(w, r, "/b/"+b[0].ID, http.StatusFound)
		return
	}
	if a.istAdmin(r) {
		http.Redirect(w, r, "/verwalten", http.StatusFound)
		return
	}
	a.v.zeigen(w, r, "board", a.daten(r, "Boardr", "", boardSeite{Name: "Boardr"}))
}

func (a *App) boardZeigen(w http.ResponseWriter, r *http.Request) {
	s := boardSeite{ID: r.PathValue("id")}
	s.Boards = a.meineBoards(r)
	darf := map[string]bool{}
	for _, b := range s.Boards {
		darf[b.ID] = true
		if b.ID == s.ID {
			s.Name = b.Name
		}
	}
	if !darf[s.ID] {
		http.NotFound(w, r)
		return
	}
	g, err := a.gruppen(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, x := range g {
		if !darf[x.Board] {
			continue
		}
		hier := x.Board == s.ID
		if hier {
			s.Gruppen = append(s.Gruppen, x)
		}
		for _, k := range x.Kacheln {
			if k.Suche != "" {
				s.Suchen = append(s.Suchen, k)
			}
			if !hier {
				s.Andere = append(s.Andere, k)
				continue
			}
			s.Kacheln++
			if k.Pruefen && k.Geprueft {
				s.Gesamt++
				if k.OK {
					s.Gut++
				}
			}
		}
	}
	if s.Widgets, err = a.widgetsVon(r.Context(), []string{s.ID}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.v.zeigen(w, r, "board", a.daten(r, s.Name, s.ID, s))
}

// zustandJSON: für das Nachladen im Browser, nur von Boards, die man sehen darf.
func (a *App) zustandJSON(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `SELECT z.kachel::text, z.ok, z.ms, z.meldung,
		(SELECT round(100.0 * count(*) FILTER (WHERE p.ok) / nullif(count(*), 0))::int
		   FROM pruefungen p WHERE p.kachel = z.kachel AND p.zeit > now() - interval '24 hours')
		FROM zustand z JOIN kacheln k ON k.id = z.kachel JOIN gruppen g ON g.id = k.gruppe
		WHERE g.board::text = ANY($1::text[])`, boardIDs(a.meineBoards(r)))
	if err != nil {
		alsJSON(w, 500, map[string]string{"fehler": "Datenbank"})
		return
	}
	defer rows.Close()
	type z struct {
		OK      bool   `json:"ok"`
		MS      int    `json:"ms"`
		Meldung string `json:"meldung"`
		Anteil  *int   `json:"anteil"`
	}
	raus := map[string]z{}
	for rows.Next() {
		var id string
		var x z
		if rows.Scan(&id, &x.OK, &x.MS, &x.Meldung, &x.Anteil) == nil {
			raus[id] = x
		}
	}
	alsJSON(w, 200, raus)
}

// ---------- Verwalten ----------

func (a *App) verwalten(w http.ResponseWriter, r *http.Request) {
	boards := a.boards(r.Context())
	g, err := a.gruppen(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type boardMit struct {
		Board
		Gruppen []Gruppe
	}
	var l []boardMit
	for _, b := range boards {
		bm := boardMit{Board: b}
		for _, x := range g {
			if x.Board == b.ID {
				bm.Gruppen = append(bm.Gruppen, x)
			}
		}
		l = append(l, bm)
	}
	a.v.zeigen(w, r, "verwalten", a.daten(r, "Verwalten", "verwalten", struct {
		navDaten
		Liste []boardMit
	}{navDaten{boards}, l}))
}

func zahl(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func (a *App) boardSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		zurueck(w, r, "/verwalten", "Name fehlt")
		return
	}
	var err error
	if id := r.FormValue("id"); id != "" {
		_, err = a.db.Exec(r.Context(), `UPDATE boards SET name=$2, reihe=$3, sichtbar_fuer=$4 WHERE id::text=$1`, id, name, zahl(r.FormValue("reihe")), strings.Join(gruppenListe(r.FormValue("fuer")), ", "))
	} else {
		_, err = a.db.Exec(r.Context(), `INSERT INTO boards (name, reihe, sichtbar_fuer) VALUES ($1, $2, $3)`, name, zahl(r.FormValue("reihe")), strings.Join(gruppenListe(r.FormValue("fuer")), ", "))
	}
	if err != nil {
		zurueck(w, r, "/verwalten", "Fehler: "+err.Error())
		return
	}
	zurueck(w, r, "/verwalten", "Board gespeichert")
}

func (a *App) boardLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM boards WHERE id::text=$1`, r.PathValue("id")); err != nil {
		zurueck(w, r, "/verwalten", "Board nicht gelöscht")
		return
	}
	zurueck(w, r, "/verwalten", "Board gelöscht")
}

func (a *App) gruppeSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		zurueck(w, r, "/verwalten", "Name fehlt")
		return
	}
	var err error
	if id := r.FormValue("id"); id != "" {
		_, err = a.db.Exec(r.Context(), `UPDATE gruppen SET name=$2, reihe=$3 WHERE id::text=$1`, id, name, zahl(r.FormValue("reihe")))
	} else {
		_, err = a.db.Exec(r.Context(), `INSERT INTO gruppen (board, name, reihe) VALUES ($1::uuid, $2, $3)`,
			r.FormValue("board"), name, zahl(r.FormValue("reihe")))
	}
	if err != nil {
		zurueck(w, r, "/verwalten", "Fehler: "+err.Error())
		return
	}
	zurueck(w, r, "/verwalten", "Gruppe gespeichert")
}

func (a *App) gruppeLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM gruppen WHERE id::text=$1`, r.PathValue("id")); err != nil {
		zurueck(w, r, "/verwalten", "Gruppe nicht gelöscht")
		return
	}
	zurueck(w, r, "/verwalten", "Gruppe gelöscht")
}

func (a *App) kachelFormular(w http.ResponseWriter, r *http.Request) {
	boards := a.boards(r.Context())
	g, _ := a.gruppen(r.Context(), "")
	k := Kachel{Pruefen: true, Gruppe: r.URL.Query().Get("gruppe")}
	if id := r.PathValue("id"); id != "neu" {
		found := false
		for _, x := range g {
			for _, y := range x.Kacheln {
				if y.ID == id {
					k, found = y, true
				}
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
	}
	namen := map[string]string{}
	for _, b := range boards {
		namen[b.ID] = b.Name
	}
	type wahl struct{ ID, Name string }
	var gruppen []wahl
	for _, x := range g {
		gruppen = append(gruppen, wahl{x.ID, namen[x.Board] + " / " + x.Name})
	}
	a.v.zeigen(w, r, "kachel", a.daten(r, "Kachel", "verwalten", struct {
		navDaten
		K       Kachel
		Gruppen []wahl
	}{navDaten{boards}, k, gruppen}))
}

func (a *App) kachelSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	titel := strings.TrimSpace(r.FormValue("titel"))
	adresse := strings.TrimSpace(r.FormValue("adresse"))
	if titel == "" || adresse == "" {
		zurueck(w, r, "/verwalten", "Titel und Adresse sind Pflicht")
		return
	}
	if !strings.HasPrefix(adresse, "http://") && !strings.HasPrefix(adresse, "https://") {
		zurueck(w, r, "/verwalten", "Die Adresse muss mit http:// oder https:// beginnen")
		return
	}
	suche := strings.TrimSpace(r.FormValue("suche"))
	if suche != "" && (!strings.Contains(suche, "{q}") || !webAdresse(strings.ReplaceAll(suche, "{q}", "x"))) {
		zurueck(w, r, "/verwalten", "Die Suchadresse braucht http(s) und {q}")
		return
	}
	args := []any{r.FormValue("gruppe"), titel, adresse, strings.TrimSpace(r.FormValue("beschreibung")),
		strings.TrimSpace(r.FormValue("symbol")), r.FormValue("pruefen") == "ja",
		strings.TrimSpace(r.FormValue("pruef_adresse")), zahl(r.FormValue("reihe")), suche}
	var err error
	if id := r.FormValue("id"); id != "" {
		_, err = a.db.Exec(r.Context(), `UPDATE kacheln SET gruppe=$1::uuid, titel=$2, adresse=$3, beschreibung=$4, symbol=$5,
			pruefen=$6, pruef_adresse=$7, reihe=$8, suche=$9 WHERE id::text=$10`, append(args, id)...)
		if err == nil {
			_, err = a.db.Exec(r.Context(), `DELETE FROM zustand WHERE kachel::text=$1`, id)
		}
	} else {
		_, err = a.db.Exec(r.Context(), `INSERT INTO kacheln (gruppe, titel, adresse, beschreibung, symbol, pruefen, pruef_adresse, reihe, suche)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`, args...)
	}
	if err != nil {
		zurueck(w, r, "/verwalten", "Fehler: "+err.Error())
		return
	}
	a.anstossen()
	zurueck(w, r, "/verwalten", "Kachel gespeichert")
}

func (a *App) kachelLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM kacheln WHERE id::text=$1`, r.PathValue("id")); err != nil {
		zurueck(w, r, "/verwalten", "Kachel nicht gelöscht")
		return
	}
	zurueck(w, r, "/verwalten", "Kachel gelöscht")
}

// beispielAnlegen füllt eine leere Datenbank mit einem ersten Board.
func (a *App) beispielAnlegen(ctx context.Context) error {
	var n int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM boards`).Scan(&n); err != nil || n > 0 {
		return err
	}
	var b, g string
	if err := a.db.QueryRow(ctx, `INSERT INTO boards (name) VALUES ('Start') RETURNING id::text`).Scan(&b); err != nil {
		return err
	}
	if err := a.db.QueryRow(ctx, `INSERT INTO gruppen (board, name) VALUES ($1::uuid, 'Allgemein') RETURNING id::text`, b).Scan(&g); err != nil {
		return err
	}
	_, err := a.db.Exec(ctx, `INSERT INTO kacheln (gruppe, titel, adresse, beschreibung, pruefen) VALUES ($1::uuid, 'Boardr', 'https://github.com/Dschonas04/boardr', 'Diese Startseite', false)`, g)
	return err
}
