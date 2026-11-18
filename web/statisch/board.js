// Suche über alle Kacheln und Nachladen des Zustands, ohne die Seite neu zu laden.
(() => {
  const suche = document.getElementById("suche");
  const nichts = document.getElementById("nichts");
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
      g.hidden = sichtbar === 0;
      treffer += sichtbar;
    });
    nichts.hidden = treffer > 0;
  };
  if (suche) {
    suche.addEventListener("input", filtern);
    suche.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        const erste = document.querySelector(".kachel:not([hidden])");
        if (erste) window.open(erste.href, "_blank", "noopener");
      }
      if (e.key === "Escape") { suche.value = ""; filtern(); suche.blur(); }
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "/" && document.activeElement !== suche) { e.preventDefault(); suche.focus(); }
    });
  }
  const laden = async () => {
    try {
      const r = await fetch("/api/zustand", { credentials: "same-origin" });
      if (!r.ok) return;
      const z = await r.json();
      let gesamt = 0, gut = 0;
      document.querySelectorAll(".kachel").forEach((k) => {
        const p = k.querySelector(".punkt");
        const x = z[k.dataset.id];
        if (!p || !x) return;
        gesamt++; if (x.ok) gut++;
        p.className = "punkt " + (x.ok ? "gut" : "schlecht");
        p.title = `${x.meldung}, ${x.ms} ms`;
      });
      const s = document.getElementById("summe");
      if (s && gesamt) { s.textContent = `${gut} von ${gesamt} erreichbar`; s.className = "pille " + (gut === gesamt ? "gut" : "warn"); }
    } catch {}
  };
  setInterval(laden, 30000);
})();
