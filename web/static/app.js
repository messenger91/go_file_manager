"use strict";

(function () {
  const els = {
    breadcrumbs: document.getElementById("breadcrumbs"),
    body: document.getElementById("listing-body"),
    status: document.getElementById("status"),
    themeToggle: document.getElementById("theme-toggle"),
    viewer: document.getElementById("viewer"),
    viewerName: document.getElementById("viewer-name"),
    viewerBody: document.getElementById("viewer-body"),
    viewerDownload: document.getElementById("viewer-download"),
    viewerClose: document.getElementById("viewer-close"),
    viewerDelete: document.getElementById("viewer-delete"),
    viewerRename: document.getElementById("viewer-rename"),
    viewerEdit: document.getElementById("viewer-edit"),
    viewerSave: document.getElementById("viewer-save"),
    viewerCancel: document.getElementById("viewer-cancel"),
    viewerEditor: document.getElementById("viewer-editor"),
    fileInput: document.getElementById("file-input"),
    contextMenu: document.getElementById("context-menu"),
    confirmModal: document.getElementById("confirm-modal"),
    confirmTitle: document.getElementById("confirm-title"),
    confirmMessage: document.getElementById("confirm-message"),
    confirmOk: document.getElementById("confirm-ok"),
    confirmCancel: document.getElementById("confirm-cancel"),
    promptModal: document.getElementById("prompt-modal"),
    promptTitle: document.getElementById("prompt-title"),
    promptInput: document.getElementById("prompt-input"),
    promptOk: document.getElementById("prompt-ok"),
    promptCancel: document.getElementById("prompt-cancel"),
    uploadModal: document.getElementById("upload-modal"),
    uploadTitle: document.getElementById("upload-title"),
    uploadFiles: document.getElementById("upload-files"),
    uploadProgress: document.getElementById("upload-progress"),
    uploadProgressFill: document.getElementById("upload-progress-fill"),
    uploadStatus: document.getElementById("upload-status"),
    uploadCancel: document.getElementById("upload-cancel"),
    filter: document.getElementById("filter"),
    itemCount: document.getElementById("item-count"),
    head: document.getElementById("listing-head"),
    columnsToggle: document.getElementById("columns-toggle"),
    columnsMenu: document.getElementById("columns-menu"),
    columnsList: document.getElementById("columns-list"),
    columnsReset: document.getElementById("columns-reset"),
    infoToggle: document.getElementById("info-toggle"),
    infoModal: document.getElementById("info-modal"),
    infoClose: document.getElementById("info-close"),
    infoList: document.getElementById("info-list"),
    diskRow: document.getElementById("disk-row"),
    diskFill: document.getElementById("disk-fill"),
    diskText: document.getElementById("disk-text"),
    memRow: document.getElementById("mem-row"),
    memFill: document.getElementById("mem-fill"),
    memText: document.getElementById("mem-text"),
    viewToggle: document.getElementById("view-toggle"),
    grid: document.getElementById("grid"),
  };

  let currentPath = "/";
  let openEntry = null;
  let editing = false;
  let openFileEditable = false;
  let currentEntries = [];
  let filterText = "";
  let sortKey = "name";
  let sortDir = "asc";
  let viewMode = localStorage.getItem("view") === "grid" ? "grid" : "list";

  // --- list columns ---
  const COLUMN_DEFS = [
    { key: "name", label: "Name", sortable: true },
    { key: "size", label: "Size", sortable: true },
    { key: "time", label: "Modified", sortable: true },
  ];
  const ACTIONS_COLUMN = { key: "actions", label: "Actions", sortable: false };
  const COLUMN_CLASS = {
    name: "name",
    size: "col-size",
    time: "col-time",
    actions: "col-actions",
  };

  function columnDef(key) {
    return (
      COLUMN_DEFS.find(function (def) {
        return def.key === key;
      }) || ACTIONS_COLUMN
    );
  }

  // Mobile first: Modified is noisy on a narrow screen, so it starts hidden
  // there and can be switched on from the Columns menu.
  function defaultColumnPrefs() {
    const wide = window.matchMedia("(min-width: 640px)").matches;
    return COLUMN_DEFS.map(function (def) {
      return { key: def.key, visible: def.key !== "time" || wide };
    });
  }

  function loadColumnPrefs() {
    const defaults = defaultColumnPrefs();
    let saved;
    try {
      saved = JSON.parse(localStorage.getItem("columns"));
    } catch (_) {
      saved = null;
    }
    if (!Array.isArray(saved)) return defaults;

    const known = {};
    COLUMN_DEFS.forEach(function (def) {
      known[def.key] = true;
    });
    const prefs = [];
    saved.forEach(function (item) {
      const key = Array.isArray(item) ? item[0] : item && item.key;
      const visible = Array.isArray(item) ? item[1] : item && item.visible;
      const dup = prefs.some(function (p) {
        return p.key === key;
      });
      if (known[key] && !dup) prefs.push({ key: key, visible: visible !== false });
    });
    // Pick up any column that is new or missing from stored preferences.
    defaults.forEach(function (def) {
      const present = prefs.some(function (p) {
        return p.key === def.key;
      });
      if (!present) prefs.push(def);
    });
    return prefs;
  }

  function saveColumnPrefs() {
    localStorage.setItem(
      "columns",
      JSON.stringify(
        columnPrefs.map(function (pref) {
          return [pref.key, pref.visible];
        })
      )
    );
  }

  let columnPrefs = loadColumnPrefs();

  // Visible columns in order; the actions column is always pinned last.
  function activeColumns() {
    return columnPrefs
      .filter(function (pref) {
        return pref.visible;
      })
      .map(function (pref) {
        return columnDef(pref.key);
      })
      .concat([ACTIONS_COLUMN]);
  }

  // --- theme ---
  function applyTheme(theme) {
    document.documentElement.dataset.theme = theme;
    els.themeToggle.textContent = theme === "dark" ? "☀️" : "🌙";
    localStorage.setItem("theme", theme);
  }

  applyTheme(document.documentElement.dataset.theme || "dark");

  els.themeToggle.addEventListener("click", function () {
    applyTheme(document.documentElement.dataset.theme === "dark" ? "light" : "dark");
  });

  // --- view mode ---
  function applyView(mode) {
    viewMode = mode;
    localStorage.setItem("view", mode);
    document.body.dataset.view = mode;
    els.grid.hidden = mode !== "grid";
    els.viewToggle.textContent = mode === "grid" ? "▤" : "▦";
    const label = mode === "grid" ? "Switch to list view" : "Switch to grid view";
    els.viewToggle.title = label;
    els.viewToggle.setAttribute("aria-label", label);
    renderList();
  }

  els.viewToggle.addEventListener("click", function () {
    applyView(viewMode === "grid" ? "list" : "grid");
  });

  // --- helpers ---
  function formatSize(bytes) {
    if (bytes < 1024) return bytes + " B";
    const units = ["KB", "MB", "GB", "TB"];
    let n = bytes / 1024;
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return n.toFixed(1) + " " + units[i];
  }

  function formatTime(iso) {
    return new Date(iso).toLocaleString();
  }

  function setStatus(message, isError) {
    if (!message) {
      els.status.hidden = true;
      els.status.textContent = "";
      return;
    }
    els.status.hidden = false;
    els.status.textContent = message;
    els.status.classList.toggle("error", !!isError);
  }

  function redirectToLogin() {
    const next = window.location.pathname + window.location.search;
    window.location.href = "/login?next=" + encodeURIComponent(next);
  }

  // A 401 means the session expired: bounce to the login form.
  function ensureAuthorized(res) {
    if (res.status === 401) {
      redirectToLogin();
      throw new Error("Not signed in");
    }
  }

  async function api(endpoint, path) {
    const res = await fetch(endpoint + "?path=" + encodeURIComponent(path), {
      headers: { Accept: "application/json" },
    });
    ensureAuthorized(res);
    if (!res.ok) {
      let message = res.status + " " + res.statusText;
      try {
        const data = await res.json();
        if (data && data.error) message = data.error;
      } catch (_) {
        /* not JSON */
      }
      throw new Error(message);
    }
    return res.json();
  }

  async function sendJSON(method, url) {
    const res = await fetch(url, { method: method, headers: { Accept: "application/json" } });
    ensureAuthorized(res);
    if (!res.ok) {
      let message = res.status + " " + res.statusText;
      try {
        const data = await res.json();
        if (data && data.error) message = data.error;
      } catch (_) {
        /* not JSON */
      }
      throw new Error(message);
    }
    return res.json();
  }

  function apiDelete(path, recursive) {
    const url =
      "/api/delete?path=" + encodeURIComponent(path) + (recursive ? "&recursive=true" : "");
    return sendJSON("DELETE", url);
  }

  function apiMkdir(path) {
    return sendJSON("POST", "/api/mkdir?path=" + encodeURIComponent(path));
  }

  function apiRename(path, name) {
    return sendJSON(
      "POST",
      "/api/rename?path=" + encodeURIComponent(path) + "&name=" + encodeURIComponent(name)
    );
  }

  function apiCreate(path) {
    return sendJSON("POST", "/api/create?path=" + encodeURIComponent(path));
  }

  async function apiWrite(path, content) {
    const res = await fetch("/api/write?path=" + encodeURIComponent(path), {
      method: "PUT",
      headers: { "Content-Type": "text/plain; charset=utf-8", Accept: "application/json" },
      body: content,
    });
    ensureAuthorized(res);
    if (!res.ok) {
      let message = res.status + " " + res.statusText;
      try {
        const data = await res.json();
        if (data && data.error) message = data.error;
      } catch (_) {
        /* not JSON */
      }
      throw new Error(message);
    }
    return res.json();
  }

  const ICONS = {
    txt: "📄", md: "📝", log: "📄", json: "🧾", yml: "🧾", yaml: "🧾",
    js: "📜", ts: "📜", go: "🐹", py: "🐍", sh: "🖥️",
    png: "🖼️", jpg: "🖼️", jpeg: "🖼️", gif: "🖼️", svg: "🖼️", webp: "🖼️",
    zip: "🗜️", gz: "🗜️", tar: "🗜️", pdf: "📕",
  };

  function iconFor(entry) {
    if (entry.isDir) return "📁";
    const ext = entry.name.includes(".") ? entry.name.split(".").pop().toLowerCase() : "";
    return ICONS[ext] || "📄";
  }

  // --- rendering ---
  function renderBreadcrumbs(path) {
    els.breadcrumbs.textContent = "";
    const parts = path.split("/").filter(Boolean);
    const root = document.createElement("a");
    root.href = "#";
    root.textContent = "Home";
    root.addEventListener("click", function (e) {
      e.preventDefault();
      navigate("/");
    });
    els.breadcrumbs.appendChild(root);

    let acc = "";
    parts.forEach(function (part) {
      acc += "/" + part;
      const sep = document.createElement("span");
      sep.className = "sep";
      sep.textContent = "/";
      els.breadcrumbs.appendChild(sep);

      const link = document.createElement("a");
      link.href = "#";
      link.textContent = part;
      const target = acc;
      link.addEventListener("click", function (e) {
        e.preventDefault();
        navigate(target);
      });
      els.breadcrumbs.appendChild(link);
    });
  }

  function renderEntries(entries) {
    currentEntries = entries || [];
    renderList();
  }

  function visibleEntries() {
    const query = filterText.trim().toLowerCase();
    let list = currentEntries;
    if (query) {
      list = list.filter(function (e) {
        return e.name.toLowerCase().indexOf(query) !== -1;
      });
    }
    const direction = sortDir === "asc" ? 1 : -1;
    return list.slice().sort(function (a, b) {
      // Directories are always grouped first, regardless of sort key.
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      let cmp;
      if (sortKey === "size") {
        cmp = a.size - b.size;
      } else if (sortKey === "time") {
        cmp = new Date(a.modTime) - new Date(b.modTime);
      } else {
        cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
      }
      if (cmp === 0) cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
      return cmp * direction;
    });
  }

  function updateSortIndicators() {
    const buttons = els.head.querySelectorAll(".sort");
    Array.prototype.forEach.call(buttons, function (button) {
      const active = button.dataset.key === sortKey;
      button.classList.toggle("active", active);
      button.querySelector(".sort-arrow").textContent = active ? (sortDir === "asc" ? "▲" : "▼") : "";
      button.parentElement.setAttribute("aria-sort", active ? (sortDir === "asc" ? "ascending" : "descending") : "none");
    });
  }

  function updateCount(total, shown) {
    if (total === 0) {
      els.itemCount.textContent = "";
      return;
    }
    els.itemCount.textContent =
      shown === total
        ? total + (total === 1 ? " item" : " items")
        : shown + " of " + total + " items";
  }

  async function loadServerInfo() {
    let info;
    try {
      info = await sendJSON("GET", "/api/info");
    } catch (err) {
      els.diskRow.hidden = true;
      els.memRow.hidden = true;
      els.infoList.textContent = "";
      appendInfoRow("Error", err.message);
      return;
    }
    renderServerInfo(info);
  }

  function renderServerInfo(info) {
    const disk = info && info.disk;
    if (disk && disk.totalBytes) {
      const used = disk.usedBytes || 0;
      const free = disk.freeBytes || 0;
      // Match `df`: the reserved blocks are excluded from both used and free.
      const denom = used + free;
      const percent = denom > 0 ? (used / denom) * 100 : 0;
      setMeter(
        els.diskRow,
        els.diskFill,
        els.diskText,
        percent,
        formatSize(used) + " used · " + formatSize(free) + " free of " + formatSize(disk.totalBytes)
      );
    } else {
      els.diskRow.hidden = true;
    }

    const host = info && info.host;
    const memTotal = host && host.memoryTotalBytes;
    if (memTotal) {
      const memFree = host.memoryAvailableBytes || 0;
      const memUsed = Math.max(0, memTotal - memFree);
      setMeter(
        els.memRow,
        els.memFill,
        els.memText,
        (memUsed / memTotal) * 100,
        formatSize(memUsed) + " used · " + formatSize(memFree) + " free of " + formatSize(memTotal)
      );
    } else {
      els.memRow.hidden = true;
    }

    renderInfoRows(host, info && info.root);
  }

  function renderInfoRows(host, root) {
    els.infoList.textContent = "";
    if (root) appendInfoRow("Root", root);
    if (host) {
      if (host.hostname) appendInfoRow("Hostname", host.hostname);
      const os = host.osName || host.os;
      if (os) {
        const platform = [host.os, host.arch].filter(Boolean).join("/");
        appendInfoRow("OS", os + (platform ? " (" + platform + ")" : ""));
      }
      if (host.numCpu) appendInfoRow("CPU", host.numCpu + " cores");
      if (host.uptimeSeconds) appendInfoRow("Uptime", formatDuration(host.uptimeSeconds));
      if (host.load1 || host.load5 || host.load15) {
        appendInfoRow(
          "Load average",
          host.load1.toFixed(2) + " · " + host.load5.toFixed(2) + " · " + host.load15.toFixed(2)
        );
      }
      if (host.goVersion) appendInfoRow("Go", host.goVersion);
    }
  }

  function appendInfoRow(label, value) {
    const dt = document.createElement("dt");
    dt.textContent = label;
    const dd = document.createElement("dd");
    dd.textContent = value;
    els.infoList.append(dt, dd);
  }

  function setMeter(row, fill, text, percent, label) {
    const clamped = Math.max(0, Math.min(100, percent));
    fill.style.width = clamped + "%";
    fill.classList.toggle("warn", clamped >= 90);
    text.textContent = label;
    row.hidden = false;
  }

  function formatDuration(seconds) {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) return days + "d " + hours + "h";
    if (hours > 0) return hours + "h " + minutes + "m";
    return minutes + "m";
  }

  // --- server info popup ---
  function openInfo() {
    els.infoModal.hidden = false;
    els.infoClose.focus();
    loadServerInfo(); // refresh every time it is opened
  }

  function closeInfo() {
    els.infoModal.hidden = true;
  }

  function emptyText() {
    return currentEntries.length === 0 ? "This folder is empty." : "No files match your filter.";
  }

  function renderList() {
    updateSortIndicators();
    const entries = visibleEntries();
    updateCount(currentEntries.length, entries.length);
    if (viewMode === "grid") renderGrid(entries);
    else renderTable(entries);
  }

  function renderTableHead() {
    els.head.textContent = "";
    activeColumns().forEach(function (col) {
      const th = document.createElement("th");
      th.className = COLUMN_CLASS[col.key];
      if (col.sortable) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "sort";
        button.dataset.key = col.key;
        button.textContent = col.label;
        const arrow = document.createElement("span");
        arrow.className = "sort-arrow";
        arrow.setAttribute("aria-hidden", "true");
        button.appendChild(arrow);
        th.appendChild(button);
      } else {
        const label = document.createElement("span");
        label.className = "visually-hidden";
        label.textContent = col.label;
        th.appendChild(label);
      }
      els.head.appendChild(th);
    });
    updateSortIndicators();
  }

  function renderTable(entries) {
    els.body.textContent = "";
    const columns = activeColumns();

    if (entries.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = columns.length;
      td.className = "empty";
      td.textContent = emptyText();
      tr.appendChild(td);
      els.body.appendChild(tr);
      return;
    }

    entries.forEach(function (entry) {
      const tr = document.createElement("tr");
      tr.className = entry.isDir ? "dir" : "file";
      tr.dataset.path = entry.path;

      columns.forEach(function (col) {
        tr.appendChild(renderCell(col, entry));
      });

      tr.addEventListener("click", function () {
        if (entry.isDir) navigate(entry.path);
        else openFile(entry);
      });
      els.body.appendChild(tr);
    });
  }

  function renderCell(col, entry) {
    const td = document.createElement("td");
    td.className = COLUMN_CLASS[col.key];

    if (col.key === "name") {
      const wrap = document.createElement("span");
      wrap.className = "name-inner";
      const icon = document.createElement("span");
      icon.className = "icon";
      icon.textContent = iconFor(entry);
      const label = document.createElement("span");
      label.className = "name-label";
      label.textContent = entry.name;
      wrap.append(icon, label);
      td.appendChild(wrap);
    } else if (col.key === "size") {
      td.textContent = entry.isDir ? "—" : formatSize(entry.size);
    } else if (col.key === "time") {
      td.textContent = formatTime(entry.modTime);
    } else if (col.key === "actions") {
      const deleteBtn = document.createElement("button");
      deleteBtn.type = "button";
      deleteBtn.className = "btn danger row-delete";
      deleteBtn.title = "Delete";
      deleteBtn.setAttribute("aria-label", "Delete " + entry.name);
      deleteBtn.textContent = "🗑";
      deleteBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        deleteEntry(entry);
      });
      td.appendChild(deleteBtn);
    }
    return td;
  }

  // --- column settings ---
  function applyColumnPrefs() {
    renderTableHead();
    renderList();
  }

  function renderColumnsMenu() {
    els.columnsList.textContent = "";
    const visibleCount = columnPrefs.filter(function (pref) {
      return pref.visible;
    }).length;

    columnPrefs.forEach(function (pref, index) {
      const def = columnDef(pref.key);
      const item = document.createElement("li");
      item.className = "column-item";
      item.dataset.key = pref.key;

      const handle = document.createElement("span");
      handle.className = "column-handle";
      handle.textContent = "⠿";
      handle.title = "Drag to reorder";
      handle.setAttribute("aria-hidden", "true");
      // Only start a drag from the handle, so the checkbox stays clickable.
      handle.addEventListener("pointerdown", function () {
        item.draggable = true;
      });

      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.id = "column-vis-" + pref.key;
      checkbox.checked = pref.visible;
      // Never allow every column to be hidden.
      checkbox.disabled = pref.visible && visibleCount === 1;
      checkbox.addEventListener("change", function () {
        pref.visible = checkbox.checked;
        saveColumnPrefs();
        applyColumnPrefs();
        renderColumnsMenu();
      });

      const label = document.createElement("label");
      label.htmlFor = checkbox.id;
      label.textContent = def.label;

      const up = moveButton("▲", "Move up", index === 0, function () {
        moveColumn(index, -1);
      });
      const down = moveButton("▼", "Move down", index === columnPrefs.length - 1, function () {
        moveColumn(index, 1);
      });

      item.addEventListener("dragstart", function (e) {
        item.classList.add("dragging");
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", pref.key);
      });
      item.addEventListener("dragend", function () {
        item.classList.remove("dragging");
        item.draggable = false;
        commitColumnOrder();
      });

      item.append(handle, checkbox, label, up, down);
      els.columnsList.appendChild(item);
    });
  }

  function moveButton(glyph, title, disabled, onClick) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "column-move";
    button.textContent = glyph;
    button.title = title;
    button.setAttribute("aria-label", title);
    button.disabled = disabled;
    button.addEventListener("click", onClick);
    return button;
  }

  function moveColumn(index, delta) {
    const target = index + delta;
    if (target < 0 || target >= columnPrefs.length) return;
    const moved = columnPrefs.splice(index, 1)[0];
    columnPrefs.splice(target, 0, moved);
    saveColumnPrefs();
    applyColumnPrefs();
    renderColumnsMenu();
  }

  // Reads the order the user dragged the rows into and persists it.
  function commitColumnOrder() {
    const byKey = {};
    columnPrefs.forEach(function (pref) {
      byKey[pref.key] = pref;
    });
    const ordered = Array.prototype.slice
      .call(els.columnsList.querySelectorAll(".column-item"))
      .map(function (item) {
        return byKey[item.dataset.key];
      })
      .filter(Boolean);
    if (ordered.length !== columnPrefs.length) return;
    columnPrefs = ordered;
    saveColumnPrefs();
    applyColumnPrefs();
  }

  function columnAfterPoint(y) {
    const items = Array.prototype.slice.call(
      els.columnsList.querySelectorAll(".column-item:not(.dragging)")
    );
    let closest = { offset: Number.NEGATIVE_INFINITY, element: null };
    items.forEach(function (item) {
      const box = item.getBoundingClientRect();
      const offset = y - box.top - box.height / 2;
      if (offset < 0 && offset > closest.offset) closest = { offset: offset, element: item };
    });
    return closest.element;
  }

  els.columnsList.addEventListener("dragover", function (e) {
    const dragging = els.columnsList.querySelector(".column-item.dragging");
    if (!dragging) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    const after = columnAfterPoint(e.clientY);
    if (after === null) els.columnsList.appendChild(dragging);
    else els.columnsList.insertBefore(dragging, after);
  });

  function showColumnsMenu() {
    renderColumnsMenu();
    els.columnsMenu.hidden = false;
    els.columnsToggle.setAttribute("aria-expanded", "true");

    const anchor = els.columnsToggle.getBoundingClientRect();
    const menu = els.columnsMenu.getBoundingClientRect();
    const left = Math.max(8, Math.min(anchor.right - menu.width, window.innerWidth - menu.width - 8));
    let top = anchor.bottom + 6;
    if (top + menu.height > window.innerHeight - 8) {
      top = Math.max(8, anchor.top - menu.height - 6);
    }
    els.columnsMenu.style.left = left + "px";
    els.columnsMenu.style.top = top + "px";
  }

  function hideColumnsMenu() {
    els.columnsMenu.hidden = true;
    els.columnsToggle.setAttribute("aria-expanded", "false");
  }

  function resetColumns() {
    columnPrefs = COLUMN_DEFS.map(function (def) {
      return { key: def.key, visible: true };
    });
    saveColumnPrefs();
    applyColumnPrefs();
    renderColumnsMenu();
  }

  function renderGrid(entries) {
    els.grid.textContent = "";

    if (entries.length === 0) {
      const empty = document.createElement("div");
      empty.className = "empty";
      empty.textContent = emptyText();
      els.grid.appendChild(empty);
      return;
    }

    entries.forEach(function (entry) {
      const card = document.createElement("div");
      card.className = "card " + (entry.isDir ? "dir" : "file");
      card.tabIndex = 0;

      const icon = document.createElement("div");
      icon.className = "card-icon";
      icon.textContent = iconFor(entry);

      const name = document.createElement("div");
      name.className = "card-name";
      name.textContent = entry.name;
      name.title = entry.name;

      const deleteBtn = document.createElement("button");
      deleteBtn.type = "button";
      deleteBtn.className = "btn danger card-delete";
      deleteBtn.title = "Delete";
      deleteBtn.setAttribute("aria-label", "Delete " + entry.name);
      deleteBtn.textContent = "🗑";
      deleteBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        deleteEntry(entry);
      });

      card.append(icon, name);
      if (!entry.isDir) {
        const meta = document.createElement("div");
        meta.className = "card-meta";
        meta.textContent = formatSize(entry.size);
        card.appendChild(meta);
      }
      card.appendChild(deleteBtn);
      card.dataset.path = entry.path;

      card.addEventListener("click", function () {
        if (entry.isDir) navigate(entry.path);
        else openFile(entry);
      });
      card.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          if (entry.isDir) navigate(entry.path);
          else openFile(entry);
        }
      });
      els.grid.appendChild(card);
    });
  }

  async function navigate(path) {
    setStatus("");
    closeViewer();
    try {
      const data = await api("/api/list", path);
      if (data.path !== currentPath) {
        filterText = "";
        els.filter.value = "";
      }
      currentPath = data.path;
      renderBreadcrumbs(data.path);
      renderEntries(data.entries);
    } catch (err) {
      setStatus("Failed to load " + path + ": " + err.message, true);
      renderEntries([]);
    }
  }

  async function openFile(entry) {
    cancelEdit();
    openEntry = entry;
    openFileEditable = false;
    els.viewer.hidden = false;
    els.viewerName.textContent = entry.name;
    els.viewerBody.textContent = "Loading…";
    els.viewerDownload.href = "/api/download?path=" + encodeURIComponent(entry.path);
    els.viewerDownload.setAttribute("download", entry.name);
    els.viewerEdit.hidden = true;

    try {
      const data = await api("/api/read", entry.path);
      els.viewerBody.textContent = data.content;
      // Binary files (containing NUL bytes) are not safe to edit as text.
      openFileEditable = data.content.indexOf("\u0000") === -1;
    } catch (err) {
      els.viewerBody.textContent =
        "Cannot preview this file.\n\n" + err.message + "\n\nUse Download instead.";
      openFileEditable = false;
    }
    els.viewerEdit.hidden = !openFileEditable;
  }

  function closeViewer() {
    cancelEdit();
    els.viewer.hidden = true;
    els.viewerBody.textContent = "";
    openEntry = null;
  }

  function startEdit() {
    if (!openEntry || !openFileEditable) return;
    editing = true;
    els.viewer.classList.add("editing");
    els.viewerBody.hidden = true;
    els.viewerEditor.hidden = false;
    els.viewerEditor.value = els.viewerBody.textContent;
    els.viewerEditor.focus();
  }

  function cancelEdit() {
    editing = false;
    els.viewer.classList.remove("editing");
    els.viewerBody.hidden = false;
    els.viewerEditor.hidden = true;
    els.viewerEditor.value = "";
  }

  async function saveEdit() {
    if (!openEntry) return;
    const entry = openEntry;
    try {
      await apiWrite(entry.path, els.viewerEditor.value);
      els.viewerBody.textContent = els.viewerEditor.value;
      cancelEdit();
      setStatus('Saved "' + entry.name + '".', false);
    } catch (err) {
      setStatus("Failed to save " + entry.name + ": " + err.message, true);
    }
  }

  // --- confirmation dialog (replaces window.confirm) ---
  function confirmDialog(opts) {
    return new Promise(function (resolve) {
      els.confirmTitle.textContent = opts.title;
      els.confirmMessage.textContent = opts.message;
      els.confirmOk.textContent = opts.confirmLabel || "Delete";
      els.confirmModal.hidden = false;
      els.confirmOk.focus();

      function finish(result) {
        els.confirmModal.hidden = true;
        els.confirmOk.removeEventListener("click", onConfirm);
        els.confirmCancel.removeEventListener("click", onCancel);
        els.confirmModal.removeEventListener("click", onBackdrop);
        document.removeEventListener("keydown", onKey);
        resolve(result);
      }
      function onConfirm() {
        finish(true);
      }
      function onCancel() {
        finish(false);
      }
      function onBackdrop(e) {
        if (e.target === els.confirmModal) finish(false);
      }
      function onKey(e) {
        if (e.key === "Escape") finish(false);
      }

      els.confirmOk.addEventListener("click", onConfirm);
      els.confirmCancel.addEventListener("click", onCancel);
      els.confirmModal.addEventListener("click", onBackdrop);
      document.addEventListener("keydown", onKey);
    });
  }

  // --- text prompt dialog (replaces window.prompt) ---
  function promptDialog(opts) {
    return new Promise(function (resolve) {
      els.promptTitle.textContent = opts.title;
      els.promptOk.textContent = opts.confirmLabel || "Create";
      els.promptInput.value = opts.value || "";
      els.promptModal.hidden = false;
      els.promptInput.focus();
      els.promptInput.select();

      function finish(result) {
        els.promptModal.hidden = true;
        els.promptOk.removeEventListener("click", onOk);
        els.promptCancel.removeEventListener("click", onCancel);
        els.promptModal.removeEventListener("click", onBackdrop);
        els.promptInput.removeEventListener("keydown", onKey);
        document.removeEventListener("keydown", onEsc);
        resolve(result);
      }
      function onOk() {
        finish(els.promptInput.value.trim());
      }
      function onCancel() {
        finish(null);
      }
      function onBackdrop(e) {
        if (e.target === els.promptModal) finish(null);
      }
      function onKey(e) {
        if (e.key === "Enter") {
          e.preventDefault();
          onOk();
        }
      }
      function onEsc(e) {
        if (e.key === "Escape") finish(null);
      }

      els.promptOk.addEventListener("click", onOk);
      els.promptCancel.addEventListener("click", onCancel);
      els.promptModal.addEventListener("click", onBackdrop);
      els.promptInput.addEventListener("keydown", onKey);
      document.addEventListener("keydown", onEsc);
    });
  }

  function invalidSegment(name) {
    return name === "." || name === ".." || name.indexOf("/") !== -1;
  }

  async function createFolder() {
    const name = await promptDialog({ title: "New folder", value: "", confirmLabel: "Create" });
    if (name === null) return; // cancelled
    if (name === "") {
      setStatus("Folder name cannot be empty.", true);
      return;
    }
    if (invalidSegment(name)) {
      setStatus('Invalid folder name "' + name + '".', true);
      return;
    }

    const base = currentPath === "/" ? "" : currentPath;
    try {
      await apiMkdir(base + "/" + name);
      await navigate(currentPath);
      setStatus('Created folder "' + name + '".', false);
    } catch (err) {
      setStatus("Failed to create folder: " + err.message, true);
    }
  }

  async function renameEntry(entry) {
    const name = await promptDialog({ title: "Rename", value: entry.name, confirmLabel: "Rename" });
    if (name === null) return; // cancelled
    if (name === entry.name) return; // unchanged
    if (name === "") {
      setStatus("Name cannot be empty.", true);
      return;
    }
    if (invalidSegment(name)) {
      setStatus('Invalid name "' + name + '".', true);
      return;
    }

    try {
      await apiRename(entry.path, name);
      closeViewer();
      await navigate(currentPath);
      setStatus('Renamed "' + entry.name + '" to "' + name + '".', false);
    } catch (err) {
      setStatus("Failed to rename: " + err.message, true);
    }
  }

  async function createFile() {
    const name = await promptDialog({ title: "New file", value: "", confirmLabel: "Create" });
    if (name === null) return; // cancelled
    if (name === "") {
      setStatus("File name cannot be empty.", true);
      return;
    }
    if (invalidSegment(name)) {
      setStatus('Invalid file name "' + name + '".', true);
      return;
    }

    const base = currentPath === "/" ? "" : currentPath;
    try {
      await apiCreate(base + "/" + name);
      await navigate(currentPath);
      setStatus('Created file "' + name + '".', false);
    } catch (err) {
      setStatus("Failed to create file: " + err.message, true);
    }
  }

  // --- upload with progress ---
  // The size limit mirrors files.MaxUploadSize on the server; it is enforced
  // here too so an oversized batch fails before sending anything.
  const MAX_UPLOAD_SIZE = 50 << 20; // 50 MiB

  let uploadRequest = null;

  function openUploadModal(files) {
    const total = files.reduce(function (sum, file) {
      return sum + file.size;
    }, 0);

    els.uploadTitle.textContent = "Uploading";
    els.uploadFiles.textContent =
      (files.length === 1 ? files[0].name : files.length + " files") +
      " · " +
      formatSize(total);
    els.uploadStatus.textContent = "";
    els.uploadStatus.classList.remove("error");
    els.uploadProgressFill.style.width = "0%";
    els.uploadProgressFill.classList.remove("done", "error");
    els.uploadProgress.setAttribute("aria-valuenow", "0");
    els.uploadCancel.hidden = false;
    els.uploadCancel.disabled = false;
    els.uploadModal.hidden = false;
  }

  function setUploadProgress(percent) {
    const clamped = Math.max(0, Math.min(100, percent));
    els.uploadProgressFill.style.width = clamped + "%";
    els.uploadProgress.setAttribute("aria-valuenow", String(Math.round(clamped)));
  }

  function finishUpload() {
    els.uploadCancel.hidden = true;
    uploadRequest = null;
  }

  function closeUploadModal() {
    els.uploadModal.hidden = true;
  }

  function uploadFiles(fileList) {
    const files = Array.prototype.slice.call(fileList || []);
    if (files.length === 0) return;

    const total = files.reduce(function (sum, file) {
      return sum + file.size;
    }, 0);
    if (total > MAX_UPLOAD_SIZE) {
      setStatus(
        "Upload is " +
          formatSize(total) +
          ", larger than the " +
          formatSize(MAX_UPLOAD_SIZE) +
          " limit.",
        true
      );
      els.fileInput.value = "";
      return;
    }

    const form = new FormData();
    files.forEach(function (file) {
      form.append("file", file, file.name);
    });

    openUploadModal(files);

    // fetch() cannot report upload progress, so the upload uses XHR.
    const xhr = new XMLHttpRequest();
    uploadRequest = xhr;
    xhr.open("POST", "/api/upload?path=" + encodeURIComponent(currentPath));
    xhr.setRequestHeader("Accept", "application/json");

    xhr.upload.addEventListener("progress", function (e) {
      if (!e.lengthComputable) return;
      const percent = (e.loaded / e.total) * 100;
      setUploadProgress(percent);
      els.uploadStatus.textContent =
        formatSize(e.loaded) + " of " + formatSize(e.total) + " (" + Math.round(percent) + "%)";
    });

    xhr.upload.addEventListener("load", function () {
      // The body is sent; the server now writes the files.
      setUploadProgress(100);
      els.uploadStatus.textContent = "Processing on the server…";
    });

    xhr.addEventListener("error", function () {
      uploadFailed("Network error while uploading.");
    });

    xhr.addEventListener("abort", function () {
      els.uploadProgressFill.classList.add("error");
      els.uploadStatus.textContent = "Upload cancelled.";
      els.uploadStatus.classList.add("error");
      finishUpload();
      els.fileInput.value = "";
      setStatus("Upload cancelled.", true);
    });

    xhr.addEventListener("load", function () {
      if (uploadRequest !== xhr) return; // already handled (aborted)
      let data = {};
      try {
        data = JSON.parse(xhr.responseText);
      } catch (_) {
        data = {};
      }

      if (xhr.status === 401) {
        finishUpload();
        els.fileInput.value = "";
        redirectToLogin();
        return;
      }
      if (xhr.status < 200 || xhr.status >= 300) {
        uploadFailed((data && data.error) || xhr.status + " " + xhr.statusText);
        return;
      }

      const uploaded = data.uploaded || [];
      const failed = data.failed || [];
      els.uploadProgressFill.classList.add("done");
      finishUpload();
      els.fileInput.value = "";

      let message = "";
      let isError = false;
      if (uploaded.length && failed.length === 0) {
        closeUploadModal();
        message = "Uploaded " + uploaded.length + " file(s).";
      } else if (uploaded.length) {
        closeUploadModal();
        message = "Uploaded " + uploaded.length + ", failed " + failed.length + ".";
        isError = true;
      } else {
        const detail = failed
          .map(function (f) {
            return f.name + ": " + f.error;
          })
          .join("; ");
        uploadFailed("Upload failed" + (detail ? " — " + detail : "."));
        return;
      }
      // Refresh the listing first: navigate() clears the status line, so the
      // message must be set after it finishes.
      navigate(currentPath).then(function () {
        setStatus(message, isError);
      });
    });

    xhr.send(form);
  }

  function uploadFailed(message) {
    setUploadProgress(100);
    els.uploadProgressFill.classList.add("error");
    els.uploadStatus.textContent = message;
    els.uploadStatus.classList.add("error");
    finishUpload();
    els.fileInput.value = "";
    setStatus(message, true);
  }

  async function deleteEntry(entry) {
    const kind = entry.isDir ? "folder" : "file";
    const extra = entry.isDir ? " and everything inside it" : "";
    const ok = await confirmDialog({
      title: "Delete " + kind,
      message: 'Delete "' + entry.name + '"' + extra + "? This cannot be undone.",
      confirmLabel: "Delete",
    });
    if (!ok) return;

    try {
      await apiDelete(entry.path, entry.isDir);
      closeViewer();
      await navigate(currentPath);
      setStatus("Deleted " + kind + ' "' + entry.name + '".', false);
    } catch (err) {
      setStatus("Failed to delete " + entry.name + ": " + err.message, true);
    }
  }

  function downloadEntry(entry) {
    const a = document.createElement("a");
    a.href = "/api/download?path=" + encodeURIComponent(entry.path);
    a.download = entry.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  // --- right-click context menu ---
  function hideContextMenu() {
    els.contextMenu.hidden = true;
    els.contextMenu.textContent = "";
  }

  function viewMenuItems() {
    return [
      { label: "List view", checked: viewMode === "list", action: function () { applyView("list"); } },
      { label: "Grid view", checked: viewMode === "grid", action: function () { applyView("grid"); } },
    ];
  }

  function globalMenuItems() {
    return [
      { label: "New folder", action: function () { createFolder(); } },
      { label: "New file", action: function () { createFile(); } },
      { label: "Upload files…", action: function () { els.fileInput.click(); } },
      { separator: true },
    ]
      .concat(viewMenuItems())
      .concat([
        { separator: true },
        { label: "Server info", action: function () { openInfo(); } },
      ]);
  }

  function buildMenu(x, y, items) {
    els.contextMenu.textContent = "";
    items.forEach(function (item) {
      if (item.separator) {
        const sep = document.createElement("div");
        sep.className = "context-sep";
        els.contextMenu.appendChild(sep);
        return;
      }
      const button = document.createElement("button");
      button.type = "button";
      button.className =
        "context-item" + (item.danger ? " danger" : "") + (item.checked ? " checked" : "");
      button.setAttribute("role", "menuitem");
      button.textContent = (item.checked ? "✓ " : "") + item.label;
      button.addEventListener("click", function () {
        hideContextMenu();
        item.action();
      });
      els.contextMenu.appendChild(button);
    });

    els.contextMenu.hidden = false;
    const rect = els.contextMenu.getBoundingClientRect();
    els.contextMenu.style.left = Math.min(x, window.innerWidth - rect.width - 8) + "px";
    els.contextMenu.style.top = Math.min(y, window.innerHeight - rect.height - 8) + "px";
  }

  function showContextMenu(x, y, entry) {
    const items = entry.isDir
      ? [
          { label: "Open", action: function () { navigate(entry.path); } },
          { label: "Rename", action: function () { renameEntry(entry); } },
          { label: "Delete", danger: true, action: function () { deleteEntry(entry); } },
        ]
      : [
          { label: "Preview", action: function () { openFile(entry); } },
          { label: "Rename", action: function () { renameEntry(entry); } },
          { label: "Download", action: function () { downloadEntry(entry); } },
          { label: "Delete", danger: true, action: function () { deleteEntry(entry); } },
        ];
    items.push({ separator: true });
    items.push.apply(items, globalMenuItems());
    buildMenu(x, y, items);
  }

  function showViewMenu(x, y) {
    buildMenu(x, y, globalMenuItems());
  }

  document.addEventListener("click", function (e) {
    hideContextMenu();
    if (!e.target.closest("#columns-menu") && !e.target.closest("#columns-toggle")) {
      hideColumnsMenu();
    }
  });

  // The app always shows its own context menu and suppresses the browser's,
  // except inside text fields, where the native menu (copy/paste) is useful.
  document.addEventListener("contextmenu", function (e) {
    if (e.target.closest("input, textarea, [contenteditable='true']")) return;
    e.preventDefault();

    const target = e.target.closest("[data-path]");
    const entry =
      target &&
      currentEntries.find(function (item) {
        return item.path === target.dataset.path;
      });
    if (entry) showContextMenu(e.clientX, e.clientY, entry);
    else showViewMenu(e.clientX, e.clientY);
  });

  window.addEventListener(
    "scroll",
    function () {
      hideContextMenu();
      hideColumnsMenu();
    },
    true
  );
  window.addEventListener("resize", function () {
    hideContextMenu();
    hideColumnsMenu();
  });

  els.viewerDelete.addEventListener("click", function () {
    if (openEntry) deleteEntry(openEntry);
  });

  els.viewerRename.addEventListener("click", function () {
    if (openEntry) renameEntry(openEntry);
  });

  els.viewerEdit.addEventListener("click", startEdit);
  els.viewerSave.addEventListener("click", saveEdit);
  els.viewerCancel.addEventListener("click", cancelEdit);

  els.fileInput.addEventListener("change", function () {
    uploadFiles(els.fileInput.files);
  });

  els.infoToggle.addEventListener("click", openInfo);
  els.infoClose.addEventListener("click", closeInfo);
  els.infoModal.addEventListener("click", function (e) {
    if (e.target === els.infoModal) closeInfo();
  });

  els.columnsToggle.addEventListener("click", function () {
    if (els.columnsMenu.hidden) showColumnsMenu();
    else hideColumnsMenu();
  });
  els.columnsReset.addEventListener("click", resetColumns);

  els.uploadCancel.addEventListener("click", function () {
    if (uploadRequest) uploadRequest.abort();
  });
  // Clicking the backdrop must not hide the dialog mid-upload; only the
  // Cancel button (or Escape) stops it, and those abort the request.
  els.uploadModal.addEventListener("click", function (e) {
    if (e.target === els.uploadModal && !uploadRequest) closeUploadModal();
  });

  els.viewerClose.addEventListener("click", closeViewer);
  els.viewer.addEventListener("click", function (e) {
    if (e.target === els.viewer && !editing) closeViewer();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && !els.uploadModal.hidden) {
      if (uploadRequest) uploadRequest.abort();
      else closeUploadModal();
    } else if (e.key === "Escape" && !els.columnsMenu.hidden) {
      hideColumnsMenu();
    } else if (e.key === "Escape" && !els.infoModal.hidden) {
      closeInfo();
    } else if (e.key === "Escape" && !els.viewer.hidden) {
      if (editing) cancelEdit();
      else closeViewer();
    } else if ((e.ctrlKey || e.metaKey) && e.key === "s" && editing) {
      e.preventDefault();
      saveEdit();
    }
  });

  // --- filter and sort ---
  els.filter.addEventListener("input", function () {
    filterText = els.filter.value;
    renderList();
  });

  els.filter.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && els.filter.value !== "") {
      els.filter.value = "";
      filterText = "";
      renderList();
      e.stopPropagation();
    }
  });

  // Sorting is delegated so the header can be rebuilt when columns change.
  els.head.addEventListener("click", function (e) {
    const button = e.target.closest("button.sort");
    if (!button) return;
    const key = button.dataset.key;
    if (sortKey === key) {
      sortDir = sortDir === "asc" ? "desc" : "asc";
    } else {
      sortKey = key;
      // Size and Modified are most useful largest/newest first.
      sortDir = key === "name" ? "asc" : "desc";
    }
    renderList();
  });

  renderTableHead();
  applyView(viewMode);
  navigate(currentPath);
})();
