/* TranslAI — frontend logic */

(function () {
  "use strict";

  // ── Drop zone ──────────────────────────────────────────────────────────────
  const dropZone = document.getElementById("drop-zone");
  const fileInput = document.getElementById("file-input");
  const fileList = document.getElementById("file-list");
  const convertBtn = document.getElementById("convert-btn");
  const detectLangEl = document.getElementById("source-lang");
  const progressSection = document.getElementById("progress-section");
  const progressBar = document.getElementById("progress-bar");
  const progressStatus = document.getElementById("progress-status");
  const resultsSection = document.getElementById("results-section");
  const resultsList = document.getElementById("results-list");
  const downloadAllBtn = document.getElementById("download-all-btn");

  let selectedFiles = [];
  let currentJobId = null;
  let eventSource = null;
  let fileResults = {};

  if (!dropZone) return; // not on convert page

  dropZone.addEventListener("dragover", (e) => {
    e.preventDefault();
    dropZone.classList.add("drag-over");
  });
  dropZone.addEventListener("dragleave", () => dropZone.classList.remove("drag-over"));
  dropZone.addEventListener("drop", (e) => {
    e.preventDefault();
    dropZone.classList.remove("drag-over");
    handleFiles(Array.from(e.dataTransfer.files).filter((f) => f.name.endsWith(".srt")));
  });
  if (fileInput) {
    fileInput.addEventListener("change", () => {
      handleFiles(Array.from(fileInput.files));
      fileInput.value = "";
    });
  }

  function handleFiles(files) {
    files.forEach((f) => {
      if (!selectedFiles.find((x) => x.name === f.name)) {
        selectedFiles.push(f);
      }
    });
    renderFileList();
    if (selectedFiles.length > 0 && convertBtn) convertBtn.disabled = false;
    // Auto-detect language from first file
    if (selectedFiles.length === 1) detectLang(selectedFiles[0]);
  }

  function renderFileList() {
    if (!fileList) return;
    fileList.innerHTML = "";
    selectedFiles.forEach((f, i) => {
      const li = document.createElement("li");
      li.innerHTML = `<span>${f.name}</span><span class="badge">${formatBytes(f.size)}</span>
        <button class="btn btn-ghost btn-sm" data-idx="${i}" title="Retirer">✕</button>`;
      li.querySelector("button").addEventListener("click", () => {
        selectedFiles.splice(i, 1);
        renderFileList();
        if (selectedFiles.length === 0 && convertBtn) convertBtn.disabled = true;
      });
      fileList.appendChild(li);
    });
  }

  async function detectLang(file) {
    if (!detectLangEl) return;
    const fd = new FormData();
    fd.append("file", file);
    try {
      const r = await fetch("/api/detect", { method: "POST", body: fd });
      if (r.ok) {
        const data = await r.json();
        if (data.detected_lang && detectLangEl.querySelector(`option[value="${data.detected_lang}"]`)) {
          detectLangEl.value = data.detected_lang;
        }
      }
    } catch (_) {}
  }

  function formatBytes(b) {
    if (b < 1024) return b + " B";
    return (b / 1024).toFixed(1) + " KB";
  }

  // ── Convert ────────────────────────────────────────────────────────────────
  if (convertBtn) {
    convertBtn.addEventListener("click", startConvert);
  }

  async function startConvert() {
    if (selectedFiles.length === 0) return;
    const targetLang = document.getElementById("target-lang")?.value || "fr";
    const sourceLang = detectLangEl?.value || "auto";

    const fd = new FormData();
    selectedFiles.forEach((f) => fd.append("files", f));
    fd.append("target", targetLang);
    fd.append("source", sourceLang);

    convertBtn.disabled = true;
    progressSection.hidden = false;
    resultsSection.hidden = true;
    fileResults = {};
    progressBar.style.width = "0%";
    progressStatus.textContent = "Démarrage…";

    try {
      const r = await fetch("/api/convert", { method: "POST", body: fd });
      if (!r.ok) {
        const t = await r.text();
        alert("Erreur: " + t);
        convertBtn.disabled = false;
        return;
      }
      const data = await r.json();
      currentJobId = data.job_id;
      openSSE(currentJobId);
    } catch (e) {
      alert("Erreur réseau: " + e);
      convertBtn.disabled = false;
    }
  }

  function openSSE(jobId) {
    if (eventSource) eventSource.close();
    eventSource = new EventSource("/api/convert/stream?job_id=" + encodeURIComponent(jobId));

    eventSource.addEventListener("progress", (e) => {
      const d = JSON.parse(e.data);
      const pct = d.Total > 0 ? Math.round((d.Done / d.Total) * 100) : 0;
      progressBar.style.width = pct + "%";
      progressStatus.textContent = d.Stage + (d.File ? " — " + d.File : "");
    });

    eventSource.addEventListener("result", (e) => {
      const d = JSON.parse(e.data);
      fileResults[d.File] = d;
      renderResults();
    });

    eventSource.addEventListener("error", (e) => {
      const d = JSON.parse(e.data || "{}");
      if (d.File) {
        fileResults[d.File] = { ...d, isError: true };
        renderResults();
      }
    });

    eventSource.onerror = () => {
      eventSource.close();
      convertBtn.disabled = false;
      if (progressBar.style.width === "100%") return;
      progressStatus.textContent = "Connexion perdue";
    };

    // Detect completion: all files received
    const checkDone = setInterval(() => {
      if (Object.keys(fileResults).length >= selectedFiles.length) {
        clearInterval(checkDone);
        eventSource.close();
        progressBar.style.width = "100%";
        progressStatus.textContent = "Terminé";
        convertBtn.disabled = false;
      }
    }, 500);
  }

  function renderResults() {
    if (!resultsList) return;
    resultsSection.hidden = false;
    resultsList.innerHTML = "";
    Object.entries(fileResults).forEach(([name, d]) => {
      const row = document.createElement("div");
      row.className = "result-row";
      if (d.Payload) {
        row.innerHTML = `<span class="filename">${name}</span>
          <span class="status-ok">OK</span>
          <a class="btn btn-sm btn-ghost" href="/api/download?job_id=${currentJobId}&file=${encodeURIComponent(name)}">Télécharger</a>`;
      } else {
        row.innerHTML = `<span class="filename">${name}</span>
          <span class="status-err">${d.Stage || "erreur"}</span>`;
      }
      resultsList.appendChild(row);
    });
    if (downloadAllBtn && Object.keys(fileResults).length > 1) {
      downloadAllBtn.hidden = false;
      downloadAllBtn.href = "/api/download/all?job_id=" + encodeURIComponent(currentJobId);
    }
  }

  if (downloadAllBtn) {
    downloadAllBtn.addEventListener("click", (e) => {
      if (!currentJobId) { e.preventDefault(); return; }
      downloadAllBtn.href = "/api/download/all?job_id=" + encodeURIComponent(currentJobId);
    });
  }

  // ── Admin — test provider ──────────────────────────────────────────────────
  document.querySelectorAll(".test-provider-btn").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const card = btn.closest(".provider-card");
      const fb = card.querySelector(".test-feedback");
      const providerName = btn.dataset.provider;
      fb.textContent = "Test en cours…";
      fb.className = "feedback";
      try {
        const r = await fetch("/api/test-provider", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ provider: providerName }),
        });
        const data = await r.json();
        if (r.ok && data.ok) {
          fb.textContent = "Connexion OK" + (data.message ? " — " + data.message : "");
          fb.className = "feedback ok";
        } else {
          fb.textContent = "Erreur: " + (data.error || r.status);
          fb.className = "feedback err";
        }
      } catch (e) {
        fb.textContent = "Erreur réseau: " + e;
        fb.className = "feedback err";
      }
    });
  });

  // ── Admin — save config ────────────────────────────────────────────────────
  const configForm = document.getElementById("config-form");
  if (configForm) {
    configForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const fb = document.getElementById("save-feedback");
      const fd = new FormData(configForm);
      const obj = buildConfigFromForm(fd);
      try {
        const r = await fetch("/api/config", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(obj),
        });
        if (r.ok) {
          fb.textContent = "Configuration sauvegardée";
          fb.className = "alert alert-ok";
        } else {
          const t = await r.text();
          fb.textContent = "Erreur: " + t;
          fb.className = "alert alert-err";
        }
        fb.hidden = false;
        setTimeout(() => { fb.hidden = true; }, 3000);
      } catch (err) {
        fb.textContent = "Erreur réseau: " + err;
        fb.className = "alert alert-err";
        fb.hidden = false;
      }
    });
  }

  function buildConfigFromForm(fd) {
    const config = {
      active_provider: fd.get("active_provider") || "",
      default_target: fd.get("default_target") || "fr",
      batch_size: parseInt(fd.get("batch_size") || "25"),
      concurrency: parseInt(fd.get("concurrency") || "2"),
      providers: {},
    };
    // Parse provider fields: provider[name][field]
    for (const [key, val] of fd.entries()) {
      const m = key.match(/^provider\[([^\]]+)\]\[([^\]]+)\]$/);
      if (m) {
        const [, provName, field] = m;
        if (!config.providers[provName]) config.providers[provName] = {};
        if (field === "temperature") config.providers[provName][field] = parseFloat(val) || 0.2;
        else config.providers[provName][field] = val;
      }
    }
    return config;
  }
})();
