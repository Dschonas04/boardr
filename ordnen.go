package main

import (
	"encoding/json"
	"net/http"
)

// ordnen speichert, was auf dem Board gezogen wurde. Kacheln bleiben dabei auf
// ihrem Board, auch wenn die Anfrage etwas anderes behauptet.
func (a *App) ordnen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !pruefeFormular(w, r) {
		return
	}
	var l []struct {
		Gruppe  string   `json:"gruppe"`
		Kacheln []string `json:"kacheln"`
	}
	if json.Unmarshal([]byte(r.PostFormValue("ordnung")), &l) != nil {
		alsJSON(w, http.StatusBadRequest, map[string]string{"fehler": "Ordnung unlesbar"})
		return
	}
	board := r.PostFormValue("board")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		alsJSON(w, http.StatusInternalServerError, map[string]string{"fehler": "Datenbank"})
		return
	}
	defer tx.Rollback(r.Context())
	for _, g := range l {
		for i, k := range g.Kacheln {
			e, err := tx.Exec(r.Context(), `UPDATE kacheln k SET gruppe = ziel.id, reihe = $4
				FROM gruppen ziel, gruppen jetzt
				WHERE k.id::text = $1 AND jetzt.id = k.gruppe AND ziel.id::text = $2
				  AND jetzt.board::text = $3 AND ziel.board::text = $3`, k, g.Gruppe, board, i)
			if err != nil || e.RowsAffected() != 1 {
				alsJSON(w, http.StatusBadRequest, map[string]string{"fehler": "Kachel oder Gruppe passt nicht zum Board"})
				return
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		alsJSON(w, http.StatusInternalServerError, map[string]string{"fehler": "Nicht gespeichert"})
		return
	}
	alsJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
