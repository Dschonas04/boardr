-- Datenbankschema von Boardr (Entwurf).
CREATE TABLE IF NOT EXISTS boards (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name      text NOT NULL,
	gruppen   text[] NOT NULL DEFAULT '{}',       -- Keycloak-Gruppen, leer = alle
	reihe     int NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS kacheln (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	board     uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	gruppe    text NOT NULL DEFAULT '',
	titel     text NOT NULL,
	adresse   text NOT NULL,
	symbol    text NOT NULL DEFAULT '',
	art       text NOT NULL DEFAULT 'link',       -- link | widget
	einstellungen jsonb NOT NULL DEFAULT '{}',
	x int NOT NULL DEFAULT 0, y int NOT NULL DEFAULT 0,
	breite int NOT NULL DEFAULT 1, hoehe int NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS pruefungen (
	kachel    uuid NOT NULL REFERENCES kacheln(id) ON DELETE CASCADE,
	zeit      timestamptz NOT NULL DEFAULT now(),
	ok        boolean NOT NULL,
	ms        int
);
CREATE INDEX IF NOT EXISTS pruefungen_kachel_zeit ON pruefungen(kachel, zeit DESC);
