package main

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Homarr-Dateien lesen, ohne cgo
)

// Übernahme aus Homarr: Boards, Abschnitte als Gruppen, App-Kacheln in ihrer
// Reihenfolge aus dem breitesten Layout. Benutzer, Integrationen und deren
// Geheimnisse werden nicht gelesen.

type importKachel struct {
	titel, adresse, beschreibung, symbol, pruefung string
}

type importGruppe struct {
	name    string
	kacheln []importKachel
}

type importBoard struct {
	name    string
	gruppen []importGruppe
}

func webAdresse(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

func homarrLesen(ctx context.Context, pfad string) ([]importBoard, error) {
	q := url.Values{"mode": {"ro"}, "immutable": {"1"}, "_pragma": {"query_only(1)", "trusted_schema(0)"}}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: pfad, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, ende := context.WithTimeout(ctx, 20*time.Second)
	defer ende()

	type app struct{ name, text, bild, href, ping string }
	apps := map[string]app{}
	lesen := func(frage string, je func(*sql.Rows) error, args ...any) error {
		r, err := db.QueryContext(ctx, frage, args...)
		if err != nil {
			return err
		}
		defer r.Close()
		n := 0
		for r.Next() {
			if n++; n > 10000 {
				return errors.New("zu viele Zeilen")
			}
			if err := je(r); err != nil {
				return err
			}
		}
		return r.Err()
	}
	if err := lesen(`SELECT id, name, COALESCE(description, ''), icon_url, COALESCE(href, ''), COALESCE(ping_url, '') FROM app`, func(r *sql.Rows) error {
		var id string
		var a app
		if err := r.Scan(&id, &a.name, &a.text, &a.bild, &a.href, &a.ping); err != nil {
			return err
		}
		apps[id] = a
		return nil
	}); err != nil {
		return nil, err
	}

	var ids []string
	var boards []importBoard
	if err := lesen(`SELECT id, name FROM board ORDER BY name`, func(r *sql.Rows) error {
		var id, name string
		if err := r.Scan(&id, &name); err != nil {
			return err
		}
		ids = append(ids, id)
		boards = append(boards, importBoard{name: name})
		return nil
	}); err != nil {
		return nil, err
	}

	for i, board := range ids {
		var layout string
		if err := db.QueryRowContext(ctx, `SELECT id FROM layout WHERE board_id = ? ORDER BY column_count DESC, id LIMIT 1`, board).Scan(&layout); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		type abschnitt struct {
			y, x int
			g    importGruppe
		}
		var l []*abschnitt
		je := map[string]*abschnitt{}
		if err := lesen(`SELECT s.id, COALESCE(s.name, ''), COALESCE(s.options, '{}'),
				COALESCE(sl.y_offset, s.y_offset, 0), COALESCE(sl.x_offset, s.x_offset, 0)
			FROM section s LEFT JOIN section_layout sl ON sl.section_id = s.id AND sl.layout_id = ?
			WHERE s.board_id = ?`, func(r *sql.Rows) error {
			var id, name, roh string
			var s abschnitt
			if err := r.Scan(&id, &name, &roh, &s.y, &s.x); err != nil {
				return err
			}
			var o struct {
				JSON struct {
					Title string `json:"title"`
				} `json:"json"`
			}
			if json.Unmarshal([]byte(roh), &o) == nil {
				name = cmp.Or(name, o.JSON.Title)
			}
			s.g.name = cmp.Or(strings.TrimSpace(name), "Allgemein")
			je[id] = &s
			l = append(l, &s)
			return nil
		}, layout, board); err != nil {
			return nil, err
		}
		if err := lesen(`SELECT il.section_id, i.options FROM item i JOIN item_layout il ON il.item_id = i.id AND il.layout_id = ?
			WHERE i.board_id = ? AND i.kind = 'app' ORDER BY il.y_offset, il.x_offset, i.id`, func(r *sql.Rows) error {
			var sec, roh string
			if err := r.Scan(&sec, &roh); err != nil {
				return err
			}
			var o struct {
				JSON struct {
					App string `json:"appId"`
				} `json:"json"`
			}
			if json.Unmarshal([]byte(roh), &o) != nil {
				return nil
			}
			a, da := apps[o.JSON.App]
			s := je[sec]
			if !da || s == nil || !webAdresse(a.href) {
				return nil
			}
			k := importKachel{titel: cmp.Or(a.name, a.href), adresse: a.href, beschreibung: a.text}
			if strings.HasPrefix(a.bild, "https://") && webAdresse(a.bild) {
				k.symbol = a.bild
			}
			if webAdresse(a.ping) {
				k.pruefung = a.ping
			}
			s.g.kacheln = append(s.g.kacheln, k)
			return nil
		}, layout, board); err != nil {
			return nil, err
		}
		slices.SortStableFunc(l, func(a, b *abschnitt) int { return cmp.Or(a.y-b.y, a.x-b.x) })
		for _, s := range l {
			if len(s.g.kacheln) > 0 {
				boards[i].gruppen = append(boards[i].gruppen, s.g)
			}
		}
	}
	return boards, nil
}

func (a *App) importSchreiben(ctx context.Context, boards []importBoard) (int, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rein := func(s string) string { return strings.ReplaceAll(strings.ToValidUTF8(s, "?"), "\x00", "") }
	n := 0
	for _, b := range boards {
		if len(b.gruppen) == 0 {
			continue
		}
		var bid string
		if err := tx.QueryRow(ctx, `INSERT INTO boards (name, reihe) VALUES ($1, (SELECT COALESCE(max(reihe), 0) + 1 FROM boards)) RETURNING id::text`, rein(b.name)).Scan(&bid); err != nil {
			return 0, err
		}
		for gi, g := range b.gruppen {
			var gid string
			if err := tx.QueryRow(ctx, `INSERT INTO gruppen (board, name, reihe) VALUES ($1::uuid, $2, $3) RETURNING id::text`, bid, rein(g.name), gi).Scan(&gid); err != nil {
				return 0, err
			}
			for ki, k := range g.kacheln {
				if _, err := tx.Exec(ctx, `INSERT INTO kacheln (gruppe, titel, adresse, beschreibung, symbol, pruef_adresse, reihe) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7)`,
					gid, rein(k.titel), rein(k.adresse), rein(k.beschreibung), rein(k.symbol), rein(k.pruefung), ki); err != nil {
					return 0, err
				}
				n++
			}
		}
	}
	return n, tx.Commit(ctx)
}

func (a *App) homarrSeite(w http.ResponseWriter, r *http.Request) {
	a.v.zeigen(w, r, "homarr", a.daten(r, "Aus Homarr übernehmen", "verwalten", navDaten{a.boards(r.Context())}))
}

func (a *App) homarrHolen(w http.ResponseWriter, r *http.Request) {
	const hier = "/verwalten/homarr"
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		zurueck(w, r, hier, "Datei zu groß (höchstens 64 MB)")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if !pruefeFormular(w, r) {
		return
	}
	f, _, err := r.FormFile("datei")
	if err != nil {
		zurueck(w, r, hier, "Keine Datei gewählt")
		return
	}
	defer f.Close()
	// Die Datei enthält Geheimnisse von Homarr: eigener Ordner, danach sofort weg.
	ordner, err := os.MkdirTemp("", "boardr-homarr-")
	if err != nil {
		zurueck(w, r, hier, "Kein Platz zum Zwischenspeichern")
		return
	}
	defer os.RemoveAll(ordner)
	pfad := filepath.Join(ordner, "db.sqlite")
	z, err := os.OpenFile(pfad, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		zurueck(w, r, hier, "Kein Platz zum Zwischenspeichern")
		return
	}
	_, err = io.Copy(z, f)
	if e := z.Close(); err == nil {
		err = e
	}
	if err != nil {
		zurueck(w, r, hier, "Datei unvollständig")
		return
	}
	boards, err := homarrLesen(r.Context(), pfad)
	if err != nil {
		log.Printf("homarr: %v", err)
		zurueck(w, r, hier, "Das ist keine lesbare Homarr-Datenbank (Version 1)")
		return
	}
	n, err := a.importSchreiben(r.Context(), boards)
	if err != nil {
		log.Printf("homarr: %v", err)
		zurueck(w, r, hier, "Übernehmen ging nicht")
		return
	}
	a.anstossen()
	zurueck(w, r, "/verwalten", fmt.Sprintf("%d Kacheln übernommen", n))
}
