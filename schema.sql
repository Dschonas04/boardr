-- Datenbankschema von Boardr. Idempotent: wird bei jedem Start eingespielt.
CREATE TABLE IF NOT EXISTS boards (
	id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name    text NOT NULL,
	reihe   int NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS gruppen (
	id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	board   uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	name    text NOT NULL,
	reihe   int NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS kacheln (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	gruppe        uuid NOT NULL REFERENCES gruppen(id) ON DELETE CASCADE,
	titel         text NOT NULL,
	adresse       text NOT NULL,
	beschreibung  text NOT NULL DEFAULT '',
	symbol        text NOT NULL DEFAULT '',   -- Bildadresse oder ein Zeichen
	pruefen       boolean NOT NULL DEFAULT true,
	pruef_adresse text NOT NULL DEFAULT '',   -- leer: die Adresse selbst
	reihe         int NOT NULL DEFAULT 0
);

-- Letzter Stand je Kachel und ein kurzer Verlauf für die Verfügbarkeit.
CREATE TABLE IF NOT EXISTS zustand (
	kachel   uuid PRIMARY KEY REFERENCES kacheln(id) ON DELETE CASCADE,
	ok       boolean NOT NULL,
	ms       int NOT NULL DEFAULT 0,
	meldung  text NOT NULL DEFAULT '',
	geprueft timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pruefungen (
	kachel  uuid NOT NULL REFERENCES kacheln(id) ON DELETE CASCADE,
	zeit    timestamptz NOT NULL DEFAULT now(),
	ok      boolean NOT NULL,
	ms      int NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS pruefungen_kachel_zeit ON pruefungen(kachel, zeit DESC);

-- Wer die App eingerichtet hat und verwaltet. Dazu kommt BOARDR_ADMIN.
CREATE TABLE IF NOT EXISTS admins (
	kennung text PRIMARY KEY,
	seit    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE boards ADD COLUMN IF NOT EXISTS sichtbar_fuer text NOT NULL DEFAULT '';
ALTER TABLE kacheln ADD COLUMN IF NOT EXISTS suche text NOT NULL DEFAULT '';

-- Widgets eines Boards. Geheimnisse stehen nie hier, nur der Name der Variable.
CREATE TABLE IF NOT EXISTS widgets (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	board     uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	art       text NOT NULL,
	titel     text NOT NULL,
	adresse   text NOT NULL DEFAULT '',
	abfrage   text NOT NULL DEFAULT '',
	geheimnis text NOT NULL DEFAULT '',
	reihe     int NOT NULL DEFAULT 0
);
