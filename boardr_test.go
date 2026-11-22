package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSichtUndOrdnen(t *testing.T) {
	adresse := os.Getenv("BOARDR_TEST_DB")
	if adresse == "" {
		t.Skip("BOARDR_TEST_DB ist nicht gesetzt")
	}
	c, err := pgxpool.ParseConfig(adresse)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.ConnConfig.Database, "pruef") {
		t.Fatal("BOARDR_TEST_DB muss auf eine Datenbank pruef* zeigen")
	}
	ctx := context.Background()
	db := datenbank(ctx, adresse)
	defer db.Close()
	if _, err := db.Exec(ctx, `TRUNCATE boards CASCADE`); err != nil {
		t.Fatal(err)
	}
	const iss = "https://anbieter.example/realms/pruefung"
	a := &App{db: db, v: ladeVorlagen(), admins: []string{"oidc:" + iss + "|chefin"},
		sso: NeuesSSO("Boardr", filepath.Join(t.TempDir(), "sso.json"), "BOARDR_TEST_SSO_AUS")}
	a.sso.k = SSOKonfig{Aktiv: true, Aussteller: iss, ClientID: "app"}
	token := strings.Repeat("a", 32)
	ruf := func(sub string, gruppen []string, methode, pfad string, f url.Values) *httptest.ResponseRecorder {
		k := httptest.NewRecorder()
		a.sso.setzen(k, httptest.NewRequest("GET", "/", nil), ssoKeks, ssoSitzung{SSONutzer: SSONutzer{Kennung: sub, Nutzer: sub, Gruppen: gruppen},
			Aussteller: iss, ClientID: "app", Bis: time.Now().Add(time.Hour).Unix()}, time.Hour)
		r := httptest.NewRequest(methode, pfad, strings.NewReader(f.Encode()))
		for _, c := range k.Result().Cookies() {
			r.AddCookie(c)
		}
		if methode != "GET" {
			r.AddCookie(&http.Cookie{Name: "formular", Value: token})
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://"+r.Host)
		}
		w := httptest.NewRecorder()
		a.wege().ServeHTTP(w, r)
		return w
	}
	eins := func(sql string, args ...any) (id string) {
		if err := db.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return
	}
	offen := eins(`INSERT INTO boards (name) VALUES ('Offen') RETURNING id::text`)
	zu := eins(`INSERT INTO boards (name, sichtbar_fuer) VALUES ('Familie', 'familie') RETURNING id::text`)
	g1 := eins(`INSERT INTO gruppen (board, name) VALUES ($1::uuid, 'A') RETURNING id::text`, offen)
	g2 := eins(`INSERT INTO gruppen (board, name) VALUES ($1::uuid, 'B') RETURNING id::text`, offen)
	g3 := eins(`INSERT INTO gruppen (board, name) VALUES ($1::uuid, 'C') RETURNING id::text`, zu)
	k1 := eins(`INSERT INTO kacheln (gruppe, titel, adresse) VALUES ($1::uuid, 'Eins', 'https://eins.example') RETURNING id::text`, g1)
	k2 := eins(`INSERT INTO kacheln (gruppe, titel, adresse) VALUES ($1::uuid, 'GEHEIME-KACHEL', 'https://zwei.example') RETURNING id::text`, g3)
	w1 := eins(`INSERT INTO widgets (board, art, titel) VALUES ($1::uuid, 'wetter', 'Wetter') RETURNING id::text`, zu)
	eins(`INSERT INTO zustand (kachel, ok) VALUES ($1::uuid, true) RETURNING kachel::text`, k2)

	if w := ruf("gast", nil, "GET", "/b/"+zu, nil); w.Code != http.StatusNotFound {
		t.Errorf("fremdes Board sichtbar: %d", w.Code)
	}
	if b := ruf("gast", nil, "GET", "/b/"+offen, nil).Body.String(); strings.Contains(b, "GEHEIME-KACHEL") || strings.Contains(b, "/b/"+zu) {
		t.Error("Suche oder Menü verrät das fremde Board")
	}
	for pfad, id := range map[string]string{"/api/zustand": k2, "/api/widgets": w1} {
		if strings.Contains(ruf("gast", nil, "GET", pfad, nil).Body.String(), id) {
			t.Errorf("%s verrät das fremde Board", pfad)
		}
	}
	if w := ruf("kind", []string{"/Familie"}, "GET", "/b/"+zu, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "GEHEIME-KACHEL") {
		t.Errorf("Gruppe sieht ihr Board nicht: %d", w.Code)
	}

	ordnung := func(g, k string) url.Values {
		return url.Values{"token": {token}, "board": {offen}, "ordnung": {`[{"gruppe":"` + g + `","kacheln":["` + k + `"]}]`}}
	}
	if w := ruf("gast", nil, "POST", "/verwalten/ordnen", ordnung(g2, k1)); w.Code != http.StatusForbidden {
		t.Errorf("Gast ordnet: %d", w.Code)
	}
	if w := ruf("chefin", nil, "POST", "/verwalten/ordnen", ordnung(g3, k1)); w.Code != http.StatusBadRequest {
		t.Errorf("Kachel wechselt das Board: %d", w.Code)
	}
	if w := ruf("chefin", nil, "POST", "/verwalten/ordnen", ordnung(g2, k1)); w.Code != http.StatusOK {
		t.Errorf("Ordnen geht nicht: %d", w.Code)
	}
	if eins(`SELECT gruppe::text FROM kacheln WHERE id::text = $1`, k1) != g2 {
		t.Error("Ordnung nicht gespeichert")
	}
	if n, err := a.importSchreiben(ctx, []importBoard{{name: "Import", gruppen: []importGruppe{{name: "G", kacheln: []importKachel{{titel: "T", adresse: "https://t.example"}}}}}}); err != nil || n != 1 {
		t.Errorf("Import: %d %v", n, err)
	}
}

func TestWidgetGeheimnisNurFuerSeinenHost(t *testing.T) {
	t.Setenv("BOARDR_WIDGET_PVE", "testwert")
	t.Setenv("BOARDR_WIDGET_PVE_ZIEL", "https://pve.example:8006")
	if g, err := widgetGeheimnis("PVE", "https://pve.example:8006/unter"); err != nil || g != "testwert" {
		t.Error("eigener Host bekommt es nicht")
	}
	for _, z := range []string{"https://fremd.example:8006", "http://pve.example:8006", "https://pve.example:8007", "https://pve.example:8006@fremd.example"} {
		if _, err := widgetGeheimnis("PVE", z); err == nil {
			t.Errorf("Geheimnis geht an %s", z)
		}
	}
	if _, err := widgetGeheimnis("PVE_ZIEL", "https://pve.example:8006"); err == nil {
		t.Error("ZIEL als Geheimnis benutzbar")
	}
}

func TestProxmoxWidget(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=testwert" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[{"type":"node","node":"pve1","status":"online","cpu":0.25,"mem":4,"maxmem":8},{"type":"qemu","status":"running"},{"type":"lxc","status":"stopped"}]}`))
	}))
	defer s.Close()
	t.Setenv("BOARDR_WIDGET_PVE", "testwert")
	t.Setenv("BOARDR_WIDGET_PVE_ZIEL", s.URL)
	l, err := widgetHolen(t.Context(), Widget{Art: "proxmox", Adresse: s.URL, Geheimnis: "PVE"})
	if err != nil || len(l) != 2 || l[0].Wert != "CPU 25 %, RAM 50 %" || l[1].Wert != "1 von 2 laufen" {
		t.Errorf("%v %v", l, err)
	}
}

func TestTermine(t *testing.T) {
	ics := "BEGIN:VEVENT\r\nSUMMARY:vorbei\r\nDTSTART:20200101T100000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Zahn\r\n arzt\r\nDTSTART;TZID=Europe/Berlin:20261123T100000\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Frei\r\nDTSTART;VALUE=DATE:20261122\r\nEND:VEVENT\r\n"
	l := termine(ics, time.Date(2026, 11, 22, 15, 0, 0, 0, time.Local))
	if len(l) != 2 || l[0].Wert != "Frei" || l[1].Wert != "Zahnarzt" {
		t.Errorf("%v", l)
	}
}

func TestHomarrLesen(t *testing.T) {
	pfad := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := sql.Open("sqlite", pfad)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE app (id text, name text, description text, icon_url text, href text, ping_url text)`,
		`CREATE TABLE board (id text, name text)`,
		`CREATE TABLE layout (id text, board_id text, column_count integer)`,
		`CREATE TABLE section (id text, board_id text, name text, options text, x_offset integer, y_offset integer)`,
		`CREATE TABLE section_layout (section_id text, layout_id text, x_offset integer, y_offset integer)`,
		`CREATE TABLE item (id text, board_id text, kind text, options text)`,
		`CREATE TABLE item_layout (item_id text, section_id text, layout_id text, x_offset integer, y_offset integer)`,
		`INSERT INTO app VALUES ('a', 'Grafana', '', 'https://bild.example/g.svg', 'https://grafana.example', ''), ('b', 'Wiki', NULL, '/lokal.svg', 'https://wiki.example', NULL), ('c', 'Böse', '', '', 'javascript:alert(1)', '')`,
		`INSERT INTO board VALUES ('1', 'Zuhause')`,
		`INSERT INTO layout VALUES ('breit', '1', 12), ('schmal', '1', 4)`,
		`INSERT INTO section VALUES ('s', '1', NULL, '{"json":{"title":"Dienste"}}', NULL, NULL)`,
		`INSERT INTO section_layout VALUES ('s', 'breit', 0, 0), ('s', 'schmal', 0, 0)`,
		`INSERT INTO item VALUES ('i', '1', 'app', '{"json":{"appId":"a"}}'), ('j', '1', 'app', '{"json":{"appId":"b"}}'), ('k', '1', 'app', '{"json":{"appId":"c"}}')`,
		`INSERT INTO item_layout VALUES ('i', 's', 'breit', 1, 0), ('j', 's', 'breit', 0, 0), ('k', 's', 'breit', 2, 0), ('i', 's', 'schmal', 0, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := homarrLesen(t.Context(), pfad)
	if err != nil || len(l) != 1 || len(l[0].gruppen) != 1 {
		t.Fatalf("%v %v", l, err)
	}
	g := l[0].gruppen[0]
	if g.name != "Dienste" || len(g.kacheln) != 2 || g.kacheln[0].titel != "Wiki" || g.kacheln[0].symbol != "" || g.kacheln[1].symbol == "" {
		t.Errorf("%+v", g)
	}
}
