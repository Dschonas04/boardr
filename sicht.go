package main

import (
	"net/http"
	"slices"
	"strings"
)

// Ein Board ohne Gruppen sehen alle Angemeldeten, eins mit Gruppen nur deren
// Mitglieder (Claim "groups" des Anbieters) und Admins.

func gruppenListe(s string) []string {
	var l []string
	for _, e := range strings.Split(s, ",") {
		if e = gruppenName(e); e != "" {
			l = append(l, e)
		}
	}
	return l
}

// gruppenName: Keycloak schreibt /familie, andere familie.
func gruppenName(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "/"))
}

func (b Board) sichtbar(admin bool, gruppen []string) bool {
	soll := gruppenListe(b.Fuer)
	if admin || len(soll) == 0 {
		return true
	}
	for _, g := range gruppen {
		if slices.Contains(soll, gruppenName(g)) {
			return true
		}
	}
	return false
}

func (a *App) meineBoards(r *http.Request) []Board {
	admin := a.istAdmin(r)
	n, _ := a.sso.Wer(r)
	var l []Board
	for _, b := range a.boards(r.Context()) {
		if b.sichtbar(admin, n.Gruppen) {
			l = append(l, b)
		}
	}
	return l
}

func boardIDs(l []Board) []string {
	ids := []string{}
	for _, b := range l {
		ids = append(ids, b.ID)
	}
	return ids
}
