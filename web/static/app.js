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
    contextMenu: document.getElementById("context-menu"),
    confirmModal: document.getElementById("confirm-modal"),
    confirmTitle: document.getElementById("confirm-title"),
    confirmMessage: document.getElementById("confirm-message"),
    confirmOk: document.getElementById("confirm-ok"),
    confirmCancel: document.getElementById("confirm-cancel"),
  };

  let currentPath = "/";
  let openEntry = null;

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

  async function apiDelete(path, recursive) {
    const url =
      "/api/delete?path=" + encodeURIComponent(path) + (recursive ? "&recursive=true" : "");
    const res = await fetch(url, { method: "DELETE", headers: { Accept: "application/json" } });
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
    els.body.textContent = "";

    if (!entries || entries.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 4;
      td.className = "empty";
      td.textContent = "This folder is empty.";
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

  async function navigate(path) {
    setStatus("");
    closeViewer();
    try {
      const data = await api("/api/list", path);
      currentPath = data.path;
      renderBreadcrumbs(data.path);
      renderEntries(data.entries);
    } catch (err) {
      setStatus("Failed to load " + path + ": " + err.message, true);
      renderEntries([]);
    }
  }

  async function openFile(entry) {
    openEntry = entry;
    els.viewer.hidden = false;
    els.viewerName.textContent = entry.name;
    els.viewerBody.textContent = "Loading…";
    els.viewerDownload.href = "/api/download?path=" + encodeURIComponent(entry.path);
    els.viewerDownload.setAttribute("download", entry.name);

    try {
      const data = await api("/api/read", entry.path);
      els.viewerBody.textContent = data.content;
    } catch (err) {
      els.viewerBody.textContent =
        "Cannot preview this file.\n\n" + err.message + "\n\nUse Download instead.";
    }
  }

  function closeViewer() {
    els.viewer.hidden = true;
    els.viewerBody.textContent = "";
    openEntry = null;
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

  function showContextMenu(x, y, entry) {
    const items = entry.isDir
      ? [
          { label: "Open", action: function () { navigate(entry.path); } },
          { label: "Delete", danger: true, action: function () { deleteEntry(entry); } },
        ]
      : [
          { label: "Preview", action: function () { openFile(entry); } },
          { label: "Download", action: function () { downloadEntry(entry); } },
          { label: "Delete", danger: true, action: function () { deleteEntry(entry); } },
        ];

    els.contextMenu.textContent = "";
    items.forEach(function (item) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "context-item" + (item.danger ? " danger" : "");
      button.setAttribute("role", "menuitem");
      button.textContent = item.label;
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

  document.addEventListener("click", hideContextMenu);
  document.addEventListener("contextmenu", function (e) {
    // Right-click outside of a row closes the menu and keeps default behavior.
    if (!e.target.closest("tr")) hideContextMenu();
  });
  window.addEventListener("scroll", hideContextMenu, true);
  window.addEventListener("resize", hideContextMenu);

  els.viewerDelete.addEventListener("click", function () {
    if (openEntry) deleteEntry(openEntry);
  });

  els.viewerClose.addEventListener("click", closeViewer);
  els.viewer.addEventListener("click", function (e) {
    if (e.target === els.viewer) closeViewer();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") closeViewer();
  });

  navigate(currentPath);
})();
