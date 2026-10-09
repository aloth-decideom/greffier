// Moteur de prise de notes : état, photos, exports (Markdown, ZIP, JSON).
// Script classique : expose globalThis.NotesCore. Aucune dépendance, fonctionne en file://.
(function () {
  "use strict";

  const TAGS = [
    { id: "decision", label: "Décision", icon: "✔" },
    { id: "action", label: "Action", icon: "➜" },
    { id: "risque", label: "Risque", icon: "⚠" },
    { id: "creuser", label: "À creuser", icon: "?" },
    { id: "hors", label: "Hors périmètre", icon: "⌀" },
  ];

  const pad = (n) => String(n).padStart(2, "0");
  const today = () => { const d = new Date(); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`; };
  const EMPTY_ANSWER = () => ({ text: "", tags: [], who: "", due: "", status: "", photos: [] });

  // Liste à plat des questions, dans l'ordre, avec leur section et leur partie.
  function flatten(data) {
    const out = [];
    data.parts.forEach((part, pi) =>
      part.sections.forEach((section) =>
        section.questions.forEach((q) => out.push({ ...q, section, part, partIndex: pi, index: out.length }))
      )
    );
    return out;
  }

  function emptyState() {
    return { meta: { date: today(), participants: "" }, answers: {}, updatedAt: null };
  }

  // ---------------------------------------------------------------- État (localStorage)
  function createStore(key) {
    let state = emptyState();
    const listeners = new Set();

    try {
      const raw = localStorage.getItem(key);
      if (raw) state = Object.assign(emptyState(), JSON.parse(raw));
    } catch (e) {
      console.warn("[notes] lecture localStorage impossible", e);
    }

    // Synchronisation entre onglets ou fenêtres
    window.addEventListener("storage", (e) => {
      if (e.key !== key || !e.newValue) return;
      try {
        state = Object.assign(emptyState(), JSON.parse(e.newValue));
        listeners.forEach((fn) => fn(state, true));
      } catch (err) { /* valeur illisible : on garde l'état courant */ }
    });

    function persist() {
      state.updatedAt = new Date().toISOString();
      try {
        localStorage.setItem(key, JSON.stringify(state));
      } catch (e) {
        console.warn("[notes] écriture localStorage impossible", e);
      }
      listeners.forEach((fn) => fn(state, false));
    }

    return {
      key,
      get state() { return state; },
      get(id) { return Object.assign(EMPTY_ANSWER(), state.answers[id]); },
      set(id, patch) {
        state.answers[id] = Object.assign(this.get(id), patch);
        persist();
      },
      toggleTag(id, tag) {
        const a = this.get(id);
        this.set(id, { tags: a.tags.includes(tag) ? a.tags.filter((t) => t !== tag) : [...a.tags, tag] });
      },
      setMeta(patch) { Object.assign(state.meta, patch); persist(); },
      reset() { state = emptyState(); persist(); },
      load(obj) { state = Object.assign(emptyState(), obj); persist(); },
      onChange(fn) { listeners.add(fn); return () => listeners.delete(fn); },
    };
  }

  // ---------------------------------------------------------------- Base IndexedDB commune
  // « photos » : images de l'atelier ; « reglages » : dossier de sauvegarde choisi (handle).
  let dbPromise = null;
  function openDB() {
    if (!dbPromise) {
      dbPromise = new Promise((resolve, reject) => {
        try {
          const req = indexedDB.open("decideom-atelier", 2);
          req.onupgradeneeded = () => {
            const db = req.result;
            if (!db.objectStoreNames.contains("photos")) db.createObjectStore("photos");
            if (!db.objectStoreNames.contains("reglages")) db.createObjectStore("reglages");
          };
          req.onsuccess = () => {
            const db = req.result;
            // Une version plus récente de la page (autre onglet) veut mettre à jour la base : on la laisse faire.
            db.onversionchange = () => { db.close(); dbPromise = null; };
            resolve(db);
          };
          req.onerror = () => reject(req.error);
          req.onblocked = () => console.warn("[notes] base bloquée par un autre onglet ouvert sur une ancienne version");
        } catch (e) { reject(e); }
      }).catch((e) => { console.warn("[notes] IndexedDB indisponible", e); return null; });
    }
    return dbPromise;
  }
  async function idb(storeName, mode, fn) {
    const d = await openDB();
    if (!d) return null;
    return new Promise((resolve, reject) => {
      const t = d.transaction(storeName, mode);
      const r = fn(t.objectStore(storeName));
      t.oncomplete = () => resolve(r && r.result);
      t.onerror = () => reject(t.error);
    });
  }

  // ---------------------------------------------------------------- Photos
  // Les images sont trop lourdes pour localStorage : elles vont dans IndexedDB.
  // Si IndexedDB est indisponible, on garde les photos en mémoire (sauvegarde disque recommandée).
  function createPhotoStore(prefix) {
    const memory = new Map();
    return {
      get persistent() { return openDB().then((d) => !!d); },
      async put(id, blob) { memory.set(id, blob); await idb("photos", "readwrite", (s) => s.put(blob, prefix + ":" + id)); },
      async get(id) {
        if (memory.has(id)) return memory.get(id);
        const b = await idb("photos", "readonly", (s) => s.get(prefix + ":" + id));
        if (b) memory.set(id, b);
        return b || null;
      },
      async del(id) { memory.delete(id); await idb("photos", "readwrite", (s) => s.delete(prefix + ":" + id)); },
    };
  }

  // Réduit une photo (2000 px max, JPEG) : ~200-500 Ko au lieu de plusieurs Mo.
  async function resizeImage(file, max = 2000, quality = 0.85) {
    const bmp = await createImageBitmap(file, { imageOrientation: "from-image" });
    const scale = Math.min(1, max / Math.max(bmp.width, bmp.height));
    const c = document.createElement("canvas");
    c.width = Math.round(bmp.width * scale);
    c.height = Math.round(bmp.height * scale);
    c.getContext("2d").drawImage(bmp, 0, 0, c.width, c.height);
    return new Promise((resolve) => c.toBlob(resolve, "image/jpeg", quality));
  }

  const blobToDataURL = (blob) => new Promise((resolve) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.readAsDataURL(blob);
  });
  const dataURLToBlob = (url) => fetch(url).then((r) => r.blob());

  // ---------------------------------------------------------------- ZIP (sans compression)
  const CRC_TABLE = (() => {
    const t = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      t[n] = c >>> 0;
    }
    return t;
  })();
  function crc32(bytes) {
    let c = 0xffffffff;
    for (let i = 0; i < bytes.length; i++) c = CRC_TABLE[(c ^ bytes[i]) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  }
  // files : [{ name, data: Uint8Array | string }]
  function makeZip(files) {
    const enc = new TextEncoder();
    const parts = [], central = [];
    let offset = 0;
    const d = new Date();
    const dosTime = (d.getHours() << 11) | (d.getMinutes() << 5) | (d.getSeconds() >> 1);
    const dosDate = ((d.getFullYear() - 1980) << 9) | ((d.getMonth() + 1) << 5) | d.getDate();
    files.forEach((f) => {
      const name = enc.encode(f.name);
      const data = typeof f.data === "string" ? enc.encode(f.data) : f.data;
      const crc = crc32(data);
      const local = new DataView(new ArrayBuffer(30));
      local.setUint32(0, 0x04034b50, true); local.setUint16(4, 20, true); local.setUint16(6, 0x0800, true);
      local.setUint16(8, 0, true); local.setUint16(10, dosTime, true); local.setUint16(12, dosDate, true);
      local.setUint32(14, crc, true); local.setUint32(18, data.length, true); local.setUint32(22, data.length, true);
      local.setUint16(26, name.length, true); local.setUint16(28, 0, true);
      parts.push(new Uint8Array(local.buffer), name, data);
      const cen = new DataView(new ArrayBuffer(46));
      cen.setUint32(0, 0x02014b50, true); cen.setUint16(4, 20, true); cen.setUint16(6, 20, true); cen.setUint16(8, 0x0800, true);
      cen.setUint16(10, 0, true); cen.setUint16(12, dosTime, true); cen.setUint16(14, dosDate, true);
      cen.setUint32(16, crc, true); cen.setUint32(20, data.length, true); cen.setUint32(24, data.length, true);
      cen.setUint16(28, name.length, true); cen.setUint32(42, offset, true);
      central.push(new Uint8Array(cen.buffer), name);
      offset += 30 + name.length + data.length;
    });
    const cenSize = central.reduce((s, p) => s + p.length, 0);
    const end = new DataView(new ArrayBuffer(22));
    end.setUint32(0, 0x06054b50, true); end.setUint16(8, files.length, true); end.setUint16(10, files.length, true);
    end.setUint32(12, cenSize, true); end.setUint32(16, offset, true);
    return new Blob([...parts, ...central, new Uint8Array(end.buffer)], { type: "application/zip" });
  }

  // ---------------------------------------------------------------- Export Markdown
  const isAnswered = (a) => !!(a && ((a.text && a.text.trim()) || (a.photos && a.photos.length)));
  const cell = (s) => String(s || "").replace(/\|/g, "\\|").replace(/\s*\n\s*/g, " / ").trim();

  function exportMarkdown(data, state, opts = {}) {
    const photoDir = opts.photoDir || "photos";
    const qs = flatten(data);
    const A = (q) => Object.assign(EMPTY_ANSWER(), state.answers[q.id]);
    const withTag = (t) => qs.filter((q) => A(q).tags.includes(t));
    const m = data.meta || {};
    const L = [];

    L.push(`# Export des notes : ${m.atelier || "Atelier"}`, "");
    L.push(`**Client** : ${m.client || ""}  **Mission** : ${m.mission || ""}`);
    L.push(`**Date** : ${state.meta.date || ""}  **Participants** : ${state.meta.participants || ""}`);
    L.push(`**Animation** : ${m.animateurs || ""}`);
    const done = qs.filter((q) => isAnswered(A(q))).length;
    const nPhotos = qs.reduce((s, q) => s + A(q).photos.length, 0);
    L.push(`**Avancement** : ${done} / ${qs.length} questions renseignées · ${nPhotos} photo(s)`, "");

    const table = (title, tag, header, row) => {
      const list = withTag(tag);
      L.push(`## ${title}`, "");
      if (!list.length) { L.push("_Aucun élément._", ""); return; }
      L.push(header, header.replace(/[^|]+/g, "---"));
      list.forEach((q, i) => L.push(row(q, A(q), i + 1)));
      L.push("");
    };
    table("Décisions", "decision", "| # | Décision | Question |",
      (q, a, i) => `| ${i} | ${cell(a.text)} | ${q.id} · ${cell(q.text)} |`);
    table("Actions", "action", "| # | Action | Responsable | Échéance | Question |",
      (q, a, i) => `| ${i} | ${cell(a.text)} | ${cell(a.who)} | ${cell(a.due)} | ${q.id} |`);
    table("Risques et points de vigilance", "risque", "| # | Risque | Question |",
      (q, a, i) => `| ${i} | ${cell(a.text)} | ${q.id} · ${cell(q.text)} |`);
    table("Points ouverts (à creuser)", "creuser", "| # | Sujet | Note |",
      (q, a, i) => `| ${i} | ${q.id} · ${cell(q.text)} | ${cell(a.text)} |`);
    table("Parking (hors périmètre)", "hors", "| # | Sujet | Note |",
      (q, a, i) => `| ${i} | ${q.id} · ${cell(q.text)} | ${cell(a.text)} |`);

    L.push("## Réponses détaillées", "");
    data.parts.forEach((part) => {
      L.push(`### ${part.title}`, "");
      part.sections.forEach((s) => {
        L.push(`#### ${s.code}. ${s.title}`, "");
        s.questions.forEach((q) => {
          const a = A(q);
          const tags = a.tags.map((t) => (TAGS.find((x) => x.id === t) || {}).label).filter(Boolean);
          const flags = [q.star ? "★" : "", a.status === "skip" ? "(sautée)" : "", a.status === "review" ? "(à revoir)" : ""]
            .filter(Boolean).join(" ");
          L.push(`**[${q.id}] ${q.text}** ${flags}`.trim());
          if (tags.length) L.push(`_Tags : ${tags.join(", ")}${a.who ? ` · Qui : ${a.who}` : ""}${a.due ? ` · Échéance : ${a.due}` : ""}_`);
          L.push("");
          L.push(a.text && a.text.trim() ? a.text.trim() : "_Non renseignée._");
          L.push("");
          a.photos.forEach((p) => { L.push(`![${p.caption || "Photo " + q.id}](${photoDir}/${p.file})`, ""); });
        });
      });
    });

    const missing = qs.filter((q) => !isAnswered(A(q)));
    L.push("## Questions non traitées", "");
    if (!missing.length) L.push("_Toutes les questions ont été renseignées._");
    missing.forEach((q) => L.push(`- [${q.id}] ${q.star ? "★ " : ""}${q.text}`));
    L.push("");
    return L.join("\n");
  }

  // ---------------------------------------------------------------- Sauvegarde sur disque
  // Deux moyens, choisis automatiquement :
  //  - « serveur » : page ouverte via Greffier → chaque modification lui est envoyée ;
  //  - « dossier » : page ouverte en double-clic dans Chrome / Edge → écriture dans un dossier choisi.
  // Dans les deux cas : session.json, notes.md, photos/ et historique/ (copie toutes les 10 min).
  // opts : { key, data, store, photos, markdown: () => string, onStatus: (status) => void }
  function createDiskSaver(opts) {
    const { key, store, photos } = opts;
    const HISTORY_MS = 10 * 60 * 1000;
    const status = { mode: "aucun", label: "", lastSave: null, error: null, pending: false };
    let dir = null;              // FileSystemDirectoryHandle (mode dossier)
    let timer = null, writing = null, lastHistory = Date.now();
    const notify = () => opts.onStatus && opts.onStatus({ ...status });
    const fail = (e) => { status.error = String((e && e.message) || e); notify(); };
    const allPhotos = () => Object.values(store.state.answers).flatMap((a) => (a && a.photos) || []);

    const canFolder = typeof window !== "undefined" && "showDirectoryPicker" in window;
    const onServer = typeof location !== "undefined" && /^https?:$/.test(location.protocol);

    // ---- Écritures ----
    async function api(method, path, body, type) {
      const r = await fetch(path, { method, body, headers: type ? { "Content-Type": type } : {} });
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).erreur || `HTTP ${r.status}`);
      return r;
    }
    async function writeFile(d, name, data) {
      const fh = await d.getFileHandle(name, { create: true });
      const w = await fh.createWritable();      // écrit dans un fichier temporaire, remplacé à la fermeture
      await w.write(data);
      await w.close();
    }
    async function subdir(name) { return dir.getDirectoryHandle(name, { create: true }); }
    async function exists(d, name) { try { await d.getFileHandle(name); return true; } catch (e) { return false; } }

    async function writeSession(historyLabel) {
      const json = JSON.stringify(store.state, null, 1);
      const md = opts.markdown();
      if (status.mode === "serveur") {
        await api("POST", "api/save", JSON.stringify({ session: store.state, markdown: md, historique: historyLabel || null }), "application/json");
      } else if (status.mode === "dossier") {
        await writeFile(dir, "session.json", json);
        await writeFile(dir, "notes.md", md);
        if (historyLabel) await writeFile(await subdir("historique"), `session_${stamp()}_${historyLabel}.json`, json);
      }
    }
    async function writePhoto(file, blob) {
      if (status.mode === "serveur") await api("PUT", "api/photo/" + encodeURIComponent(file), blob, "image/jpeg");
      else if (status.mode === "dossier") await writeFile(await subdir("photos"), file, blob);
    }

    async function flush(historyLabel) {
      clearTimeout(timer);
      if (status.mode !== "serveur" && status.mode !== "dossier") return;
      if (writing) await writing;               // une écriture à la fois
      const label = historyLabel || (Date.now() - lastHistory > HISTORY_MS ? "auto" : null);
      writing = writeSession(label).then(() => {
        if (label) lastHistory = Date.now();
        status.lastSave = new Date(); status.error = null; status.pending = false; notify();
      }).catch(fail).finally(() => { writing = null; });
      return writing;
    }
    function schedule() {
      if (status.mode !== "serveur" && status.mode !== "dossier") return;
      status.pending = true; notify();
      clearTimeout(timer);
      timer = setTimeout(() => flush(), 1500);
    }

    // Copie sur disque des photos qui n'y sont pas encore (ex. dossier lié en cours de séance)
    async function syncPhotos() {
      let onDisk = new Set();
      if (status.mode === "serveur") onDisk = new Set((await (await api("GET", "api/session")).json()).photos);
      for (const p of allPhotos()) {
        if (onDisk.has(p.file)) continue;
        if (status.mode === "dossier" && await exists(await subdir("photos"), p.file)) continue;
        const b = await photos.get(p.id);
        if (b) await writePhoto(p.file, b);
      }
    }

    // ---- Lecture d'une sauvegarde existante (restauration) ----
    async function readBackup() {
      try {
        if (status.mode === "serveur") {
          const r = await (await api("GET", "api/session")).json();
          return r.session ? { session: r.session, photoFiles: r.photos } : null;
        }
        if (status.mode === "dossier") {
          const fh = await dir.getFileHandle("session.json").catch(() => null);
          return fh ? { session: JSON.parse(await (await fh.getFile()).text()), photoFiles: null } : null;
        }
      } catch (e) { fail(e); }
      return null;
    }
    async function restore(backup) {
      for (const p of Object.values(backup.session.answers || {}).flatMap((a) => (a && a.photos) || [])) {
        try {
          let blob = null;
          if (status.mode === "serveur") blob = await (await api("GET", "api/photo/" + encodeURIComponent(p.file))).blob();
          else blob = await (await (await (await subdir("photos")).getFileHandle(p.file)).getFile());
          if (blob) await photos.put(p.id, blob);
        } catch (e) { console.warn("[sauvegarde] photo non restaurée", p.file, e); }
      }
      store.load(backup.session);
    }
    const countOf = (session) => Object.values(session.answers || {}).filter((a) => a && ((a.text || "").trim() || (a.photos || []).length)).length;

    // ---- Mode dossier : choix, mémorisation, reprise ----
    async function activateFolder(handle) {
      dir = handle;
      status.mode = "dossier"; status.label = handle.name; status.error = null;
      await idb("reglages", "readwrite", (s) => s.put(handle, key + ":dossier"));
      notify();
    }
    async function linkFolder() {              // à appeler depuis un clic (exigence du navigateur)
      const handle = await window.showDirectoryPicker({ id: "atelier-sauvegarde", mode: "readwrite", startIn: "documents" });
      await activateFolder(handle);
      return afterActivation();
    }
    async function resumeFolder() {            // idem : un clic suffit pour réautoriser
      const handle = await idb("reglages", "readonly", (s) => s.get(key + ":dossier"));
      if (!handle) return null;
      if ((await handle.requestPermission({ mode: "readwrite" })) !== "granted") return null;
      await activateFolder(handle);
      return afterActivation();
    }
    // Après activation : si le navigateur est vide mais le disque non, on propose la restauration.
    async function afterActivation() {
      const backup = await readBackup();
      const localCount = countOf(store.state);
      if (backup && countOf(backup.session) > localCount) return { backup, count: countOf(backup.session), localCount };
      await syncPhotos().catch(fail);
      await flush();
      return null;
    }

    // ---- Démarrage ----
    async function init() {
      try { if (navigator.storage && navigator.storage.persist) await navigator.storage.persist(); } catch (e) { /* facultatif */ }
      if (onServer) {
        try {
          const r = await (await fetch("api/ping")).json();
          if (r.app === "decideom-atelier") {
            status.mode = "serveur"; status.label = r.dossier; notify();
            return afterActivation();
          }
        } catch (e) { /* pas de serveur d'atelier : on continue */ }
      }
      if (canFolder) {
        const handle = await idb("reglages", "readonly", (s) => s.get(key + ":dossier")).catch(() => null);
        if (handle && (await handle.queryPermission({ mode: "readwrite" })) === "granted") {
          await activateFolder(handle);
          return afterActivation();
        }
        status.mode = handle ? "dossier-a-reprendre" : "dossier-possible";
        status.label = handle ? handle.name : "";
      } else {
        status.mode = "indisponible";
      }
      notify();
      return null;
    }

    return {
      get status() { return { ...status }; },
      init, schedule, flush, linkFolder, resumeFolder, restore, syncPhotos,
      active: () => status.mode === "serveur" || status.mode === "dossier",
      async putPhoto(file, blob) { try { await writePhoto(file, blob); } catch (e) { fail(e); } },
      async delPhoto(file) {
        try {
          if (status.mode === "serveur") await api("DELETE", "api/photo/" + encodeURIComponent(file));
          else if (status.mode === "dossier") {          // pas de suppression définitive : déplacée dans supprimees/
            const ph = await subdir("photos");
            const fh = await ph.getFileHandle(file).catch(() => null);
            if (fh) {
              await writeFile(await ph.getDirectoryHandle("supprimees", { create: true }), file, await fh.getFile());
              await ph.removeEntry(file);
            }
          }
        } catch (e) { fail(e); }
      },
      archive: (label) => flush(label),
      // Copie d'une session quelconque (ex. version du disque qu'on va écraser) dans historique/
      async archiveSession(session, label) {
        try {
          if (status.mode === "serveur") {
            await api("POST", "api/save", JSON.stringify({ session, historique: label, historique_seul: true }), "application/json");
          } else if (status.mode === "dossier") {
            await writeFile(await subdir("historique"), `session_${stamp()}_${label}.json`, JSON.stringify(session, null, 1));
          }
        } catch (e) { fail(e); }
      },
    };
  }

  // ---------------------------------------------------------------- Fichiers
  function download(filename, content, type) {
    const blob = content instanceof Blob ? content : new Blob([content], { type: type || "text/plain;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
  }

  function pickFile(accept, multiple) {
    return new Promise((resolve) => {
      const input = document.createElement("input");
      input.type = "file";
      input.accept = accept;
      input.multiple = !!multiple;
      input.onchange = () => resolve([...input.files]);
      input.click();
    });
  }

  const stamp = () => { const d = new Date(); return `${today()}_${pad(d.getHours())}h${pad(d.getMinutes())}`; };

  globalThis.NotesCore = {
    TAGS, flatten, createStore, createPhotoStore, createDiskSaver, resizeImage, blobToDataURL, dataURLToBlob,
    makeZip, exportMarkdown, download, pickFile, isAnswered, stamp, EMPTY_ANSWER,
  };
})();
