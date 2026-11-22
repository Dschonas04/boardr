// Suche, Anordnen und Nachladen, ohne die Seite neu zu laden.
(() => {
  const $ = (id) => document.getElementById(id);
  const suche = $("suche"), nichts = $("nichts"), andere = $("andere"), ziele = $("suchziele"), gruppen = $("gruppen");
  let ordnet = false;

  // ---------- Suche
  const filtern = () => {
    const q = suche.value.trim().toLowerCase();
    let treffer = 0;
    document.querySelectorAll(".gruppe").forEach((g) => {
      let sichtbar = 0;
      g.querySelectorAll(".kachel").forEach((k) => {
        const passt = !q || k.dataset.suche.toLowerCase().includes(q);
        k.hidden = !passt;
        if (passt) sichtbar++;
      });
      const weg = g === andere ? !q || !sichtbar : q ? !sichtbar : false;
      g.hidden = weg;
      if (!weg) treffer += sichtbar;
    });
    if (ziele) {
      ziele.hidden = !q;
      ziele.querySelectorAll("a").forEach((a) => {
        a.href = a.dataset.suchziel.replaceAll("{q}", () => encodeURIComponent(suche.value.trim()));
      });
    }
    nichts.hidden = !q || treffer > 0 || !!ziele;
  };
  if (suche) {
    suche.addEventListener("input", filtern);
    suche.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && suche.value.trim()) {
        const erste = [...document.querySelectorAll(".kachel")].find((k) => !k.hidden && !k.closest("[hidden]"));
        const ziel = erste || ziele?.querySelector("a");
        if (ziel) window.open(ziel.href, "_blank", "noopener,noreferrer");
      }
      if (e.key === "Escape") { suche.value = ""; filtern(); suche.blur(); }
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "/" && !e.target.closest("input, textarea, select")) { e.preventDefault(); suche.focus(); }
    });
  }

  // ---------- Anordnen
  const knopf = $("ordnen"), hilfe = $("ordnen-hilfe");
  if (knopf && gruppen) {
    let gezogen = null, kette = Promise.resolve();
    const text = hilfe.textContent;
    const sichern = () => {
      const ordnung = [...gruppen.querySelectorAll("[data-gruppe]")].map((g) => ({
        gruppe: g.dataset.gruppe, kacheln: [...g.querySelectorAll(".kachel")].map((k) => k.dataset.id),
      }));
      const daten = new URLSearchParams({ token: gruppen.dataset.token, board: gruppen.dataset.board, ordnung: JSON.stringify(ordnung) });
      // Nacheinander, sonst gewinnt womöglich ein älterer Stand.
      kette = kette.then(() => fetch("/verwalten/ordnen", { method: "POST", body: daten }))
        .then((r) => { hilfe.textContent = r.ok ? text : "Nicht gespeichert, bitte die Seite neu laden."; })
        .catch(() => { hilfe.textContent = "Nicht gespeichert, bitte die Seite neu laden."; });
    };
    knopf.addEventListener("click", () => {
      ordnet = !ordnet;
      if (ordnet && suche.value) { suche.value = ""; filtern(); }
      document.body.classList.toggle("ordnet", ordnet);
      knopf.setAttribute("aria-pressed", String(ordnet));
      knopf.textContent = ordnet ? "Fertig" : "Anordnen";
      hilfe.hidden = !ordnet;
      suche.disabled = ordnet;
      gruppen.querySelectorAll(".kachel").forEach((k) => { k.draggable = ordnet; });
    });
    gruppen.addEventListener("click", (e) => {
      const k = e.target.closest(".kachel");
      if (ordnet && k) { e.preventDefault(); k.focus(); }
    });
    gruppen.addEventListener("dragstart", (e) => {
      if (!ordnet) return;
      gezogen = e.target.closest(".kachel");
      e.dataTransfer.effectAllowed = "move";
    });
    gruppen.addEventListener("dragover", (e) => {
      if (!gezogen) return;
      const feld = e.target.closest("[data-gruppe]")?.querySelector(".kacheln");
      if (!feld) return;
      e.preventDefault();
      const ueber = e.target.closest(".kachel");
      if (ueber === gezogen) return;
      if (ueber) {
        const m = ueber.getBoundingClientRect();
        feld.insertBefore(gezogen, e.clientX > m.x + m.width / 2 ? ueber.nextElementSibling : ueber);
      } else if (gezogen.parentElement !== feld) feld.append(gezogen);
    });
    gruppen.addEventListener("drop", (e) => e.preventDefault());
    gruppen.addEventListener("dragend", () => { if (gezogen) sichern(); gezogen = null; });
    gruppen.addEventListener("keydown", (e) => {
      const k = e.target.closest(".kachel");
      if (!ordnet || !k) return;
      const felder = [...gruppen.querySelectorAll("[data-gruppe] .kacheln")];
      const i = felder.indexOf(k.parentElement);
      if (e.key === "ArrowLeft" && k.previousElementSibling) k.previousElementSibling.before(k);
      else if (e.key === "ArrowRight" && k.nextElementSibling) k.nextElementSibling.after(k);
      else if (e.key === "ArrowUp" && i > 0) felder[i - 1].append(k);
      else if (e.key === "ArrowDown" && i < felder.length - 1) felder[i + 1].prepend(k);
      else return;
      e.preventDefault();
      k.focus();
      sichern();
    });
  }

  // ---------- Nachladen
  const holen = async (pfad) => {
    try {
      const r = await fetch(pfad, { credentials: "same-origin" });
      return r.ok ? await r.json() : null;
    } catch {
      return null;
    }
  };
  const element = (art, text, klasse) => {
    const e = document.createElement(art);
    e.textContent = text;
    if (klasse) e.className = klasse;
    return e;
  };
  const laden = async () => {
    if (document.hidden) return;
    const z = await holen("/api/zustand");
    if (z) {
      let gesamt = 0, gut = 0;
      document.querySelectorAll(".kachel").forEach((k) => {
        const p = k.querySelector(".punkt"), x = z[k.dataset.id];
        if (!p || !x) return;
        if (!andere?.contains(k)) { gesamt++; if (x.ok) gut++; }
        p.className = "punkt " + (x.ok ? "gut" : "schlecht");
        const m = k.querySelector(".messung");
        if (m) m.textContent = x.ok ? `${x.ms} ms` + (x.anteil == null ? "" : ` · ${x.anteil} % in 24 h`) : x.meldung;
      });
      const s = $("summe");
      if (s && gesamt) { s.textContent = `${gut} von ${gesamt} erreichbar`; s.className = "pille " + (gut === gesamt ? "gut" : "warn"); }
    }
    if ($("widgets")) {
      const w = await holen("/api/widgets");
      if (!w) return;
      document.querySelectorAll("[data-widget]").forEach((el) => {
        const s = w[el.dataset.widget];
        if (!s || (!s.fehler && !s.werte?.length)) return;
        el.querySelector("dl").replaceChildren(...(s.fehler ? [element("dd", s.fehler, "w-fehler")] : s.werte.map((v) => {
          const zeile = document.createElement("div");
          zeile.append(element("dt", v.name), element("dd", v.wert));
          return zeile;
        })));
      });
    }
  };
  setInterval(laden, 30000);
  if ($("widgets")) setTimeout(laden, 3000);
})();
