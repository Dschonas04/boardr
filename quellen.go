package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Die Quellen der Widgets. Heraus kommen nur die Werte, die gezeigt werden,
// nie die Antwort selbst und keine Adresse in Fehlertexten.

var widgetClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	// Nicht folgen: das Token ginge sonst mit.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func abruf(ctx context.Context, methode, adresse string, kopf map[string]string, inhalt []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, methode, adresse, bytes.NewReader(inhalt))
	if err != nil {
		return nil, errors.New("Adresse unlesbar")
	}
	for k, v := range kopf {
		req.Header.Set(k, v)
	}
	res, err := widgetClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, errors.New(kurz(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, errors.New("Antwort unvollständig")
	}
	return b, nil
}

func abrufJSON(ctx context.Context, adresse string, kopf map[string]string, ziel any) error {
	b, err := abruf(ctx, http.MethodGet, adresse, kopf, nil)
	if err != nil {
		return err
	}
	if json.Unmarshal(b, ziel) != nil {
		return errors.New("Antwort unlesbar")
	}
	return nil
}

func kommazahl(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.ReplaceAll(strings.TrimSuffix(s, ".0"), ".", ",")
}

func widgetHolen(ctx context.Context, w Widget) ([]Wert, error) {
	switch w.Art {
	case "prometheus":
		return vonPrometheus(ctx, w)
	case "proxmox":
		return vonProxmox(ctx, w)
	case "pihole":
		return vonPihole(ctx, w)
	case "homeassistant":
		return vonHomeAssistant(ctx, w)
	case "wetter":
		return vonWetter(ctx, w)
	case "kalender":
		b, err := abruf(ctx, http.MethodGet, w.Adresse, nil, nil)
		if err != nil {
			return nil, err
		}
		return termine(string(b), time.Now()), nil
	}
	return nil, errors.New("Unbekannte Art")
}

func vonPrometheus(ctx context.Context, w Widget) ([]Wert, error) {
	kopf := map[string]string{}
	if w.Geheimnis != "" {
		g, err := widgetGeheimnis(w.Geheimnis, w.Adresse)
		if err != nil {
			return nil, err
		}
		kopf["Authorization"] = "Bearer " + g
	}
	var a struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := abrufJSON(ctx, w.Adresse+"/api/v1/query?query="+url.QueryEscape(w.Abfrage), kopf, &a); err != nil {
		return nil, err
	}
	var l []Wert
	for _, r := range a.Data.Result {
		v, _ := r.Value[1].(string)
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			v = kommazahl(f)
		}
		l = append(l, Wert{cmp.Or(r.Metric["name"], r.Metric["instance"], r.Metric["job"], "Wert"), v})
		if len(l) == 8 {
			break
		}
	}
	if len(l) == 0 {
		return nil, errors.New("Die Abfrage liefert nichts")
	}
	return l, nil
}

func vonProxmox(ctx context.Context, w Widget) ([]Wert, error) {
	g, err := widgetGeheimnis(w.Geheimnis, w.Adresse)
	if err != nil {
		return nil, err
	}
	var a struct {
		Data []struct {
			Type     string  `json:"type"`
			Node     string  `json:"node"`
			Status   string  `json:"status"`
			CPU      float64 `json:"cpu"`
			Mem      float64 `json:"mem"`
			MaxMem   float64 `json:"maxmem"`
			Template int     `json:"template"`
		} `json:"data"`
	}
	if err := abrufJSON(ctx, w.Adresse+"/api2/json/cluster/resources", map[string]string{"Authorization": "PVEAPIToken=" + g}, &a); err != nil {
		return nil, err
	}
	var l []Wert
	gaeste, laufen := 0, 0
	for _, r := range a.Data {
		switch r.Type {
		case "node":
			v := "aus"
			if r.Status == "online" {
				v = "CPU " + kommazahl(r.CPU*100) + " %"
				if r.MaxMem > 0 {
					v += ", RAM " + kommazahl(r.Mem*100/r.MaxMem) + " %"
				}
			}
			l = append(l, Wert{r.Node, v})
		case "qemu", "lxc":
			if r.Template == 1 {
				continue
			}
			gaeste++
			if r.Status == "running" {
				laufen++
			}
		}
	}
	slices.SortFunc(l, func(x, y Wert) int { return cmp.Compare(x.Name, y.Name) })
	return append(l, Wert{"VMs und Container", fmt.Sprintf("%d von %d laufen", laufen, gaeste)}), nil
}

func vonPihole(ctx context.Context, w Widget) ([]Wert, error) {
	g, err := widgetGeheimnis(w.Geheimnis, w.Adresse)
	if err != nil {
		return nil, err
	}
	anfrage, err := json.Marshal(map[string]string{"password": g})
	if err != nil {
		return nil, err
	}
	b, err := abruf(ctx, http.MethodPost, w.Adresse+"/api/auth", map[string]string{"Content-Type": "application/json"}, anfrage)
	if err != nil {
		return nil, err
	}
	var s struct {
		Session struct {
			SID string `json:"sid"`
		} `json:"session"`
	}
	if json.Unmarshal(b, &s) != nil || s.Session.SID == "" {
		return nil, errors.New("Anmeldung abgelehnt")
	}
	kopf := map[string]string{"X-FTL-SID": s.Session.SID}
	// Pi-hole hat wenige Sitzungsplätze, also jedes Mal wieder abmelden.
	defer func() { _, _ = abruf(context.WithoutCancel(ctx), http.MethodDelete, w.Adresse+"/api/auth", kopf, nil) }()
	var a struct {
		Queries struct {
			Total   float64 `json:"total"`
			Blocked float64 `json:"blocked"`
			Prozent float64 `json:"percent_blocked"`
		} `json:"queries"`
	}
	if err := abrufJSON(ctx, w.Adresse+"/api/stats/summary", kopf, &a); err != nil {
		return nil, err
	}
	return []Wert{{"Anfragen", kommazahl(a.Queries.Total)}, {"Blockiert", kommazahl(a.Queries.Blocked)}, {"Anteil", kommazahl(a.Queries.Prozent) + " %"}}, nil
}

func vonHomeAssistant(ctx context.Context, w Widget) ([]Wert, error) {
	g, err := widgetGeheimnis(w.Geheimnis, w.Adresse)
	if err != nil {
		return nil, err
	}
	var l []Wert
	for _, e := range strings.Split(w.Abfrage, ",") {
		if e = strings.TrimSpace(e); e == "" || len(l) == 10 {
			continue
		}
		var s struct {
			State      string `json:"state"`
			Attributes struct {
				Name    string `json:"friendly_name"`
				Einheit string `json:"unit_of_measurement"`
			} `json:"attributes"`
		}
		if err := abrufJSON(ctx, w.Adresse+"/api/states/"+url.PathEscape(e), map[string]string{"Authorization": "Bearer " + g}, &s); err != nil {
			return nil, err
		}
		l = append(l, Wert{cmp.Or(s.Attributes.Name, e), strings.TrimSpace(s.State + " " + s.Attributes.Einheit)})
	}
	return l, nil
}

func wetterText(code int) string {
	switch {
	case code == 0:
		return "klar"
	case code <= 2:
		return "leicht bewölkt"
	case code == 3:
		return "bedeckt"
	case code <= 48:
		return "Nebel"
	case code <= 57:
		return "Niesel"
	case code <= 67:
		return "Regen"
	case code <= 77:
		return "Schnee"
	case code <= 82:
		return "Schauer"
	case code <= 86:
		return "Schneeschauer"
	}
	return "Gewitter"
}

func vonWetter(ctx context.Context, w Widget) ([]Wert, error) {
	b, l, ok := strings.Cut(w.Abfrage, ",")
	breite, err1 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	laenge, err2 := strconv.ParseFloat(strings.TrimSpace(l), 64)
	if !ok || err1 != nil || err2 != nil || breite < -90 || breite > 90 || laenge < -180 || laenge > 180 {
		return nil, errors.New("Abfrage: Breite,Länge")
	}
	var a struct {
		Jetzt struct {
			T    float64 `json:"temperature_2m"`
			Code int     `json:"weather_code"`
		} `json:"current"`
		Tag struct {
			Max []float64 `json:"temperature_2m_max"`
			Min []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}
	// Auf zwei Stellen gerundet: der Ort, nicht das Haus.
	if err := abrufJSON(ctx, fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.2f&longitude=%.2f&current=temperature_2m,weather_code&daily=temperature_2m_max,temperature_2m_min&forecast_days=1&timezone=auto", breite, laenge), nil, &a); err != nil {
		return nil, err
	}
	raus := []Wert{{"Jetzt", kommazahl(a.Jetzt.T) + " °C, " + wetterText(a.Jetzt.Code)}}
	if len(a.Tag.Max) > 0 && len(a.Tag.Min) > 0 {
		raus = append(raus, Wert{"Heute", kommazahl(a.Tag.Min[0]) + " bis " + kommazahl(a.Tag.Max[0]) + " °C"})
	}
	return raus, nil
}

// termine: die nächsten fünf einzelnen Termine einer ICS-Datei.
func termine(ics string, jetzt time.Time) []Wert {
	ics = strings.ReplaceAll(ics, "\r\n", "\n")
	ics = strings.NewReplacer("\n ", "", "\n\t", "").Replace(ics)
	type termin struct {
		wann      time.Time
		ganzerTag bool
		was       string
	}
	var l []termin
	var t termin
	drin, gueltig := false, false
	for _, z := range strings.Split(ics, "\n") {
		kopf, wert, _ := strings.Cut(z, ":")
		name, mehr, _ := strings.Cut(kopf, ";")
		switch {
		case z == "BEGIN:VEVENT":
			drin, gueltig, t = true, false, termin{}
		case z == "END:VEVENT":
			if drin && gueltig {
				ende := t.wann
				if t.ganzerTag {
					ende = ende.AddDate(0, 0, 1)
				}
				if ende.After(jetzt) {
					l = append(l, t)
				}
			}
			drin = false
		case drin && name == "SUMMARY":
			t.was = strings.NewReplacer(`\,`, ",", `\;`, ";", `\n`, " ", `\\`, `\`).Replace(wert)
		case drin && name == "DTSTART":
			ort, muster := time.Local, "20060102T150405"
			for _, p := range strings.Split(mehr, ";") {
				if id, ok := strings.CutPrefix(p, "TZID="); ok {
					if o, err := time.LoadLocation(strings.Trim(id, `"`)); err == nil {
						ort = o
					}
				}
			}
			if strings.HasSuffix(wert, "Z") {
				ort, muster = time.UTC, "20060102T150405Z"
			} else if len(wert) == 8 {
				muster, t.ganzerTag = "20060102", true
			}
			if w, err := time.ParseInLocation(muster, wert, ort); err == nil {
				t.wann, gueltig = w, true
			}
		}
	}
	slices.SortStableFunc(l, func(x, y termin) int { return x.wann.Compare(y.wann) })
	raus := []Wert{}
	for _, x := range l[:min(5, len(l))] {
		wann := x.wann.Format("02.01.")
		if !x.ganzerTag {
			wann = x.wann.Local().Format("02.01. 15:04")
		}
		raus = append(raus, Wert{wann, x.was})
	}
	if len(raus) == 0 {
		raus = append(raus, Wert{"Termine", "keine"})
	}
	return raus
}
