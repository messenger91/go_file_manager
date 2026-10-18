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
    filter: document.getElementById("filter"),
    itemCount: document.getElementById("item-count"),
    footer: document.getElementById("footer"),
    diskFill: document.getElementById("disk-fill"),
    diskText: document.getElementById("disk-text"),
    viewToggle: document.getElementById("view-toggle"),
    listing: document.querySelector(".listing"),
    grid: document.getElementById("grid"),
  };

  const sortButtons = Array.prototype.slice.call(document.querySelectorAll(".sort"));

  let currentPath = "/";
  let openEntry = null;
  let editing = false;
  let openFileEditable = false;
  let currentEntries = [];
  let filterText = "";
  let sortKey = "name";
  let sortDir = "asc";
  let viewMode = localStorage.getItem("view") === "grid" ? "grid" : "list";

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

  async function api(endpoint, path) {
    const res = await fetch(endpoint + "?path=" + encodeURIComponent(path), {
      headers: { Accept: "application/json" },
    });
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
    sortButtons.forEach(function (button) {
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
    } catch (_) {
      return; // server info is best-effort; ignore failures
    }
    const disk = info && info.disk;
    if (!disk || !disk.totalBytes) return;

    const total = disk.totalBytes;
    const free = disk.freeBytes || 0;
    const used = disk.usedBytes || 0;
    // Match `df`: the reserved blocks are excluded from both used and free.
    const denom = used + free;
    const percent = denom > 0 ? Math.min(100, Math.round((used / denom) * 100)) : 0;

    els.diskFill.style.width = percent + "%";
    els.diskFill.classList.toggle("warn", percent >= 90);
    els.diskText.textContent =
      formatSize(used) + " used · " + formatSize(free) + " free of " + formatSize(total);
    els.footer.hidden = false;
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

  function renderTable(entries) {
    els.body.textContent = "";

    if (entries.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 4;
      td.className = "empty";
      td.textContent = emptyText();
      tr.appendChild(td);
      els.body.appendChild(tr);
      return;
    }

    entries.forEach(function (entry) {
      const tr = document.createElement("tr");
      tr.className = entry.isDir ? "dir" : "file";

      const nameCell = document.createElement("td");
      nameCell.className = "name";
      const icon = document.createElement("span");
      icon.className = "icon";
      icon.textContent = iconFor(entry);
      const label = document.createElement("span");
      label.textContent = entry.name;
      nameCell.append(icon, label);

      const sizeCell = document.createElement("td");
      sizeCell.className = "col-size";
      sizeCell.textContent = entry.isDir ? "—" : formatSize(entry.size);

      const timeCell = document.createElement("td");
      timeCell.className = "col-time";
      timeCell.textContent = formatTime(entry.modTime);

      const actionsCell = document.createElement("td");
      actionsCell.className = "col-actions";
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
      actionsCell.appendChild(deleteBtn);

      tr.append(nameCell, sizeCell, timeCell, actionsCell);
      tr.addEventListener("click", function () {
        if (entry.isDir) navigate(entry.path);
        else openFile(entry);
      });
      tr.addEventListener("contextmenu", function (e) {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, entry);
      });
      els.body.appendChild(tr);
    });
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
      card.addEventListener("contextmenu", function (e) {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, entry);
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
      loadServerInfo();
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

  async function uploadFiles(fileList) {
    if (!fileList || fileList.length === 0) return;

    const form = new FormData();
    Array.prototype.forEach.call(fileList, function (file) {
      form.append("file", file, file.name);
    });

    try {
      const res = await fetch("/api/upload?path=" + encodeURIComponent(currentPath), {
        method: "POST",
        headers: { Accept: "application/json" },
        body: form,
      });
      const data = await res.json().catch(function () {
        return {};
      });
      if (!res.ok) {
        throw new Error((data && data.error) || res.status + " " + res.statusText);
      }

      const uploaded = data.uploaded || [];
      const failed = data.failed || [];
      await navigate(currentPath);

      if (uploaded.length && failed.length === 0) {
        setStatus("Uploaded " + uploaded.length + " file(s).", false);
      } else if (uploaded.length) {
        setStatus("Uploaded " + uploaded.length + ", failed " + failed.length + ".", true);
      } else {
        const detail = failed.map(function (f) {
          return f.name + ": " + f.error;
        }).join("; ");
        setStatus("Upload failed" + (detail ? " — " + detail : "."), true);
      }
    } catch (err) {
      setStatus("Upload failed: " + err.message, true);
    } finally {
      els.fileInput.value = "";
    }
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
    ].concat(viewMenuItems());
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

  document.addEventListener("click", hideContextMenu);
  document.addEventListener("contextmenu", function (e) {
    // Right-click outside of the file area closes the menu and keeps the native menu.
    if (!e.target.closest(".listing") && !e.target.closest(".grid")) hideContextMenu();
  });
  els.listing.addEventListener("contextmenu", function (e) {
    if (e.target.closest("tr")) return;
    e.preventDefault();
    showViewMenu(e.clientX, e.clientY);
  });
  els.grid.addEventListener("contextmenu", function (e) {
    if (e.target.closest(".card")) return;
    e.preventDefault();
    showViewMenu(e.clientX, e.clientY);
  });
  window.addEventListener("scroll", hideContextMenu, true);
  window.addEventListener("resize", hideContextMenu);

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

  els.viewerClose.addEventListener("click", closeViewer);
  els.viewer.addEventListener("click", function (e) {
    if (e.target === els.viewer && !editing) closeViewer();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && !els.viewer.hidden) {
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

  sortButtons.forEach(function (button) {
    button.addEventListener("click", function () {
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
  });

  applyView(viewMode);
  navigate(currentPath);
})();
