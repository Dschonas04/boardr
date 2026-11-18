package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Die Statusprüfung läuft im Hintergrund, jede Minute für alle Kacheln mit
// Prüfung. Eine Kachel gilt als erreichbar, wenn die Adresse mit einem Status
// unter 500 antwortet (auch 401/403: der Dienst lebt, er will nur eine
// Anmeldung) oder, bei tcp://host:port, die Verbindung zustande kommt.

var anstoss = make(chan struct{}, 1)

func (a *App) anstossen() {
	select {
	case anstoss <- struct{}{}:
	default:
	}
}

var pruefClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConnsPerHost: 2,
		DisableKeepAlives:   true,
	},
	// Weiterleitungen zur Anmeldung zählen als Antwort, nicht folgen.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (a *App) pruefer(ctx context.Context) {
	takt := time.NewTicker(time.Minute)
	defer takt.Stop()
	aufraeumen := time.NewTicker(time.Hour)
	defer aufraeumen.Stop()
	a.allePruefen(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-takt.C:
		case <-anstoss:
		case <-aufraeumen.C:
			a.db.Exec(ctx, `DELETE FROM pruefungen WHERE zeit < now() - interval '7 days'`)
			continue
		}
		a.allePruefen(ctx)
	}
}

func (a *App) allePruefen(ctx context.Context) {
	rows, err := a.db.Query(ctx, `SELECT id::text, adresse, pruef_adresse FROM kacheln WHERE pruefen`)
	if err != nil {
		log.Printf("Prüfung: %v", err)
		return
	}
	type ziel struct{ id, adresse string }
	var l []ziel
	for rows.Next() {
		var id, adr, padr string
		if rows.Scan(&id, &adr, &padr) == nil {
			if padr != "" {
				adr = padr
			}
			l = append(l, ziel{id, adr})
		}
	}
	rows.Close()

	var wg sync.WaitGroup
	frei := make(chan struct{}, 16)
	for _, z := range l {
		wg.Add(1)
		frei <- struct{}{}
		go func(z ziel) {
			defer wg.Done()
			defer func() { <-frei }()
			ok, ms, meldung := pruefe(ctx, z.adresse)
			a.db.Exec(ctx, `INSERT INTO zustand (kachel, ok, ms, meldung, geprueft) VALUES ($1::uuid, $2, $3, $4, now())
				ON CONFLICT (kachel) DO UPDATE SET ok=$2, ms=$3, meldung=$4, geprueft=now()`, z.id, ok, ms, meldung)
			a.db.Exec(ctx, `INSERT INTO pruefungen (kachel, ok, ms) VALUES ($1::uuid, $2, $3)`, z.id, ok, ms)
		}(z)
	}
	wg.Wait()
}

func pruefe(ctx context.Context, adresse string) (bool, int, string) {
	beginn := time.Now()
	ms := func() int { return int(time.Since(beginn).Milliseconds()) }
	u, err := url.Parse(adresse)
	if err != nil {
		return false, 0, "Adresse unlesbar"
	}
	if u.Scheme == "tcp" {
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", u.Host)
		if err != nil {
			return false, ms(), kurz(err)
		}
		c.Close()
		return true, ms(), "verbunden"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresse, nil)
	if err != nil {
		return false, 0, "Adresse unlesbar"
	}
	req.Header.Set("User-Agent", "Boardr-Statuspruefung")
	resp, err := pruefClient.Do(req)
	if err != nil {
		return false, ms(), kurz(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false, ms(), fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return true, ms(), fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func kurz(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "certificate"):
		return "Zertifikat ungültig"
	case strings.Contains(s, "connection refused"):
		return "Verbindung abgelehnt"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		return "Zeitüberschreitung"
	case strings.Contains(s, "no such host"):
		return "Name unbekannt"
	case strings.Contains(s, "no route"), strings.Contains(s, "unreachable"):
		return "nicht erreichbar"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
