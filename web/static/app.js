"use strict";

(function () {
  const els = {
    breadcrumbs: document.getElementById("breadcrumbs"),
    navBack: document.getElementById("nav-back"),
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
    infoVersion: document.getElementById("info-version"),
    infoList: document.getElementById("info-list"),
    propertiesModal: document.getElementById("properties-modal"),
    propertiesName: document.getElementById("properties-name"),
    propertiesList: document.getElementById("properties-list"),
    propertiesClose: document.getElementById("properties-close"),
    diskRow: document.getElementById("disk-row"),
    diskFill: document.getElementById("disk-fill"),
    diskText: document.getElementById("disk-text"),
    memRow: document.getElementById("mem-row"),
    memFill: document.getElementById("mem-fill"),
    memText: document.getElementById("mem-text"),
    viewToggle: document.getElementById("view-toggle"),
    uploadToggle: document.getElementById("upload-toggle"),
    favoritesToggle: document.getElementById("favorites-toggle"),
    recentToggle: document.getElementById("recent-toggle"),
    sizeToggle: document.getElementById("size-toggle"),
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

  // --- grid card size ---
  const GRID_SIZES = ["s", "m", "l"];

  function loadGridSize() {
    const saved = localStorage.getItem("gridSize");
    return GRID_SIZES.indexOf(saved) !== -1 ? saved : "m";
  }

  let gridSize = loadGridSize();

  function applyGridSize(size) {
    if (GRID_SIZES.indexOf(size) === -1) size = "m";
    gridSize = size;
    localStorage.setItem("gridSize", size);
    els.grid.classList.remove("size-s", "size-m", "size-l");
    els.grid.classList.add("size-" + size);
    Array.prototype.forEach.call(els.sizeToggle.querySelectorAll(".size-btn"), function (button) {
      const active = button.dataset.size === size;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
  }

  els.sizeToggle.addEventListener("click", function (e) {
    const button = e.target.closest(".size-btn");
    if (button) applyGridSize(button.dataset.size);
  });

  // --- folder preferences: pinned folders + colors (per browser) ---
  const FOLDER_COLORS = ["red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"];

  function loadStored(key, fallback) {
    try {
      const value = JSON.parse(localStorage.getItem(key));
      return value === null || value === undefined ? fallback : value;
    } catch (_) {
      return fallback;
    }
  }

  function saveStored(key, value) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch (_) {
      /* storage unavailable (e.g. private mode): keep the in-memory state */
    }
  }

  let pinnedFolders = loadStored("pinnedFolders", []);
  if (!Array.isArray(pinnedFolders)) pinnedFolders = [];
  pinnedFolders = pinnedFolders
    .filter(function (item) {
      return item && typeof item.path === "string" && item.path !== "" && item.path !== "/";
    })
    .map(function (item) {
      return {
        path: item.path,
        name: typeof item.name === "string" && item.name ? item.name : item.path.split("/").pop(),
      };
    });

  let folderColors = loadStored("folderColors", {});
  if (!folderColors || typeof folderColors !== "object") folderColors = {};

  // Favorites (folders and files) are listed in the header for quick access.
  let favorites = loadStored("favorites", []);
  if (!Array.isArray(favorites)) favorites = [];
  favorites = favorites
    .filter(function (item) {
      return item && typeof item.path === "string" && item.path !== "" && item.path !== "/";
    })
    .map(function (item) {
      return {
        path: item.path,
        name: typeof item.name === "string" && item.name ? item.name : item.path.split("/").pop(),
        isDir: !!item.isDir,
      };
    });

  function isFavorite(path) {
    return favorites.some(function (item) {
      return item.path === path;
    });
  }

  function updateFavoritesButton() {
    els.favoritesToggle.classList.toggle("has-items", favorites.length > 0);
    const label = favorites.length
      ? "Favorites (" + favorites.length + ")"
      : "Favorites (empty)";
    els.favoritesToggle.setAttribute("aria-label", label);
    els.favoritesToggle.title = label;
  }

  function toggleFavorite(entry) {
    if (isFavorite(entry.path)) {
      favorites = favorites.filter(function (item) {
        return item.path !== entry.path;
      });
    } else {
      favorites = favorites.concat([
        { path: entry.path, name: entry.name, isDir: !!entry.isDir },
      ]);
    }
    saveStored("favorites", favorites);
    updateFavoritesButton();
    renderList();
    setStatus(
      (isFavorite(entry.path) ? "Added " : "Removed ") + '"' + entry.name + '"' +
        (isFavorite(entry.path) ? " to favorites." : " from favorites."),
      false
    );
  }

  // Recent folders and files, newest first and capped at RECENT_LIMIT.
  const RECENT_LIMIT = 20;

  let recent = loadStored("recent", []);
  if (!Array.isArray(recent)) recent = [];
  recent = recent
    .filter(function (item) {
      return item && typeof item.path === "string" && item.path !== "";
    })
    .map(function (item) {
      return {
        path: item.path,
        name: typeof item.name === "string" && item.name ? item.name : item.path.split("/").pop(),
        isDir: !!item.isDir,
      };
    })
    .slice(0, RECENT_LIMIT);

  function pathName(path) {
    return path === "/" ? "Home" : path.split("/").pop();
  }

  function updateRecentButton() {
    els.recentToggle.classList.toggle("has-items", recent.length > 0);
    const label = recent.length ? "Recent (" + recent.length + ")" : "Recent (empty)";
    els.recentToggle.setAttribute("aria-label", label);
    els.recentToggle.title = label;
  }

  // Visiting a folder or opening a file moves it to the top; the list never
  // grows past RECENT_LIMIT and never holds the same path twice.
  function recordRecent(entry) {
    recent = recent.filter(function (item) {
      return item.path !== entry.path;
    });
    recent.unshift({
      path: entry.path,
      name: entry.name || pathName(entry.path),
      isDir: !!entry.isDir,
    });
    if (recent.length > RECENT_LIMIT) recent = recent.slice(0, RECENT_LIMIT);
    saveStored("recent", recent);
    updateRecentButton();
  }

  function removeRecent(path) {
    const item = recent.find(function (entry) {
      return entry.path === path;
    });
    if (!item) return;
    recent = recent.filter(function (entry) {
      return entry.path !== path;
    });
    saveStored("recent", recent);
    updateRecentButton();
    setStatus('Removed "' + item.name + '" from recent.', false);
  }

  function clearRecent() {
    recent = [];
    saveStored("recent", recent);
    updateRecentButton();
    setStatus("Cleared recent items.", false);
  }

  function isPinned(path) {
    return pinnedFolders.some(function (item) {
      return item.path === path;
    });
  }

  function togglePin(entry) {
    if (isPinned(entry.path)) {
      pinnedFolders = pinnedFolders.filter(function (item) {
        return item.path !== entry.path;
      });
    } else {
      pinnedFolders = pinnedFolders.concat([{ path: entry.path, name: entry.name }]);
    }
    saveStored("pinnedFolders", pinnedFolders);
    renderList();
    setStatus(
      (isPinned(entry.path) ? "Pinned " : "Unpinned ") + '"' + entry.name + '".',
      false
    );
  }

  // Only palette values ever reach the DOM, so a tampered localStorage entry
  // cannot inject CSS.
  function folderColor(path) {
    const color = folderColors[path];
    return FOLDER_COLORS.indexOf(color) !== -1 ? color : "";
  }

  function setFolderColor(entry, color) {
    const next = FOLDER_COLORS.indexOf(color) !== -1 ? color : "";
    if (next) folderColors[entry.path] = next;
    else delete folderColors[entry.path];
    saveStored("folderColors", folderColors);
    renderList();
  }

  function removeFavorite(path) {
    const item = favorites.find(function (entry) {
      return entry.path === path;
    });
    if (!item) return;
    favorites = favorites.filter(function (entry) {
      return entry.path !== path;
    });
    saveStored("favorites", favorites);
    updateFavoritesButton();
    renderList();
    setStatus('Removed "' + item.name + '" from favorites.', false);
  }

  function remapPath(path, oldPath, newPath) {
    if (path === oldPath) return newPath;
    if (path.indexOf(oldPath + "/") === 0) return newPath + path.slice(oldPath.length);
    return path;
  }

  // Renaming an entry moves it (and, for folders, its whole subtree), so pinned
  // shortcuts, favorites and stored colours have to follow it.
  function rekeyFolderPaths(oldPath, newPath) {
    let pinsChanged = false;
    pinnedFolders = pinnedFolders.map(function (item) {
      const mapped = remapPath(item.path, oldPath, newPath);
      if (mapped === item.path) return item;
      pinsChanged = true;
      return { path: mapped, name: mapped.split("/").pop() };
    });
    if (pinsChanged) saveStored("pinnedFolders", pinnedFolders);

    let favoritesChanged = false;
    favorites = favorites.map(function (item) {
      const mapped = remapPath(item.path, oldPath, newPath);
      if (mapped === item.path) return item;
      favoritesChanged = true;
      return { path: mapped, name: mapped.split("/").pop(), isDir: item.isDir };
    });
    if (favoritesChanged) {
      saveStored("favorites", favorites);
      updateFavoritesButton();
    }

    let recentChanged = false;
    recent = recent.map(function (item) {
      const mapped = remapPath(item.path, oldPath, newPath);
      if (mapped === item.path) return item;
      recentChanged = true;
      return { path: mapped, name: mapped.split("/").pop(), isDir: item.isDir };
    });
    if (recentChanged) {
      saveStored("recent", recent);
      updateRecentButton();
    }

    let colorsChanged = false;
    const colors = {};
    Object.keys(folderColors).forEach(function (path) {
      const mapped = remapPath(path, oldPath, newPath);
      if (mapped !== path) colorsChanged = true;
      colors[mapped] = folderColors[path];
    });
    if (colorsChanged) {
      folderColors = colors;
      saveStored("folderColors", folderColors);
    }
  }

  // Deleting an entry drops shortcuts, favorites and colours for it and, when it
  // is a folder, for its subtree.
  function forgetFolderPaths(path) {
    const keep = function (candidate) {
      return candidate !== path && candidate.indexOf(path + "/") !== 0;
    };

    const before = pinnedFolders.length;
    pinnedFolders = pinnedFolders.filter(function (item) {
      return keep(item.path);
    });
    if (pinnedFolders.length !== before) saveStored("pinnedFolders", pinnedFolders);

    const favoritesBefore = favorites.length;
    favorites = favorites.filter(function (item) {
      return keep(item.path);
    });
    if (favorites.length !== favoritesBefore) {
      saveStored("favorites", favorites);
      updateFavoritesButton();
    }

    const recentBefore = recent.length;
    recent = recent.filter(function (item) {
      return keep(item.path);
    });
    if (recent.length !== recentBefore) {
      saveStored("recent", recent);
      updateRecentButton();
    }

    let colorsChanged = false;
    Object.keys(folderColors).forEach(function (candidate) {
      if (!keep(candidate)) {
        delete folderColors[candidate];
        colorsChanged = true;
      }
    });
    if (colorsChanged) saveStored("folderColors", folderColors);
  }

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

  // Folders use a filled SVG instead of the 📁 emoji so a chosen colour can be
  // applied; files keep their emoji icons.
  const FOLDER_SVG =
    '<svg class="folder-svg" viewBox="0 0 24 24" aria-hidden="true">' +
    '<path d="M3 7a2 2 0 0 1 2-2h3.6a2 2 0 0 1 1.6.8l1 1.2H19a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>' +
    "</svg>";

  function renderEntryIcon(el, entry) {
    if (!entry.isDir) {
      el.textContent = iconFor(entry);
      return;
    }
    const color = folderColor(entry.path);
    el.classList.add("folder-icon");
    if (color) el.dataset.color = color;
    // The colour itself comes from `.folder-icon[data-color="…"] .folder-svg` in
    // the stylesheet: a fill set on this wrapper would lose to the `fill`
    // declared by `.folder-svg` on the <svg>.
    el.innerHTML = FOLDER_SVG;
  }

  // --- rendering ---
  function updateBreadcrumbFade() {
    const el = els.breadcrumbs;
    const max = el.scrollWidth - el.clientWidth;
    el.classList.toggle("fade-left", el.scrollLeft > 1);
    el.classList.toggle("fade-right", max > 1 && el.scrollLeft < max - 1);
  }

  function renderBreadcrumbs(path) {
    els.breadcrumbs.textContent = "";
    const parts = path.split("/").filter(Boolean);

    function addCrumb(label, target, current) {
      if (current) {
        const span = document.createElement("span");
        span.className = "crumb current";
        span.setAttribute("aria-current", "page");
        span.textContent = label;
        span.title = label;
        els.breadcrumbs.appendChild(span);
        return;
      }

      const link = document.createElement("a");
      link.className = "crumb";
      link.href = "#";
      link.textContent = label;
      link.title = label;
      link.addEventListener("click", function (e) {
        e.preventDefault();
        navigate(target);
      });
      els.breadcrumbs.appendChild(link);

      const sep = document.createElement("span");
      sep.className = "sep";
      sep.setAttribute("aria-hidden", "true");
      sep.textContent = "›";
      els.breadcrumbs.appendChild(sep);
    }

    addCrumb("Home", "/", parts.length === 0);
    let acc = "";
    parts.forEach(function (part, i) {
      acc += "/" + part;
      addCrumb(part, acc, i === parts.length - 1);
    });

    // Long paths scroll sideways: keep the folder you are in visible instead
    // of showing the start of the trail.
    setTimeout(function () {
      els.breadcrumbs.scrollLeft = els.breadcrumbs.scrollWidth;
      updateBreadcrumbFade();
    }, 0);
  }

  // Back goes to the folder you came from, or to the parent when there is no
  // history yet (for example after a direct load or a refresh).
  let navHistory = [];

  function parentPath(path) {
    if (path === "/") return "/";
    const cut = path.lastIndexOf("/");
    return cut <= 0 ? "/" : path.slice(0, cut);
  }

  function backTarget() {
    if (navHistory.length > 0) return navHistory[navHistory.length - 1];
    return currentPath === "/" ? null : parentPath(currentPath);
  }

  function updateBackButton() {
    const target = backTarget();
    const label = target === null ? "Back" : "Back to " + target;
    els.navBack.disabled = target === null;
    els.navBack.title = label;
    els.navBack.setAttribute("aria-label", label);
  }

  function goBack() {
    const target = backTarget();
    if (target === null) return;
    const fromHistory = navHistory.length > 0;
    if (fromHistory) navHistory.pop();
    navigate(target, { recordHistory: !fromHistory });
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
      els.infoVersion.hidden = true;
      els.infoVersion.textContent = "";
      appendInfoRow("Error", err.message);
      return;
    }
    renderServerInfo(info);
  }

  function renderServerInfo(info) {
    const label = info && info.version ? "Go File Manager " + info.version : "";
    els.infoVersion.textContent = label;
    els.infoVersion.hidden = !label;

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

  // --- file properties ---
  const FILE_TYPES = {
    png: "Image", jpg: "Image", jpeg: "Image", gif: "Image", svg: "Image",
    webp: "Image", bmp: "Image", ico: "Image",
    mp4: "Video", mkv: "Video", webm: "Video", mov: "Video", avi: "Video",
    mp3: "Audio", wav: "Audio", flac: "Audio", ogg: "Audio", m4a: "Audio",
    zip: "Archive", gz: "Archive", tar: "Archive", bz2: "Archive", xz: "Archive", "7z": "Archive",
    pdf: "PDF document",
    txt: "Text", md: "Text", log: "Text", json: "Text", yml: "Text", yaml: "Text",
    js: "Text", ts: "Text", go: "Text", py: "Text", sh: "Text", css: "Text", html: "Text",
  };

  function entryType(entry) {
    if (entry.isDir) return "Folder";
    const ext = entry.name.includes(".") ? entry.name.split(".").pop().toLowerCase() : "";
    if (!ext) return "File";
    const kind = FILE_TYPES[ext] || "File";
    return kind + " (." + ext + ")";
  }

  function appendPropertyRow(label, value) {
    const dt = document.createElement("dt");
    dt.textContent = label;
    const dd = document.createElement("dd");
    dd.textContent = value;
    els.propertiesList.append(dt, dd);
  }

  function renderProperties(entry) {
    els.propertiesName.textContent = entry.name;
    els.propertiesList.textContent = "";
    appendPropertyRow("Name", entry.name);
    appendPropertyRow("Path", entry.path || "/");
    appendPropertyRow("Type", entryType(entry));
    appendPropertyRow(
      "Size",
      entry.isDir
        ? "—"
        : formatSize(entry.size) +
            " (" +
            entry.size.toLocaleString() +
            (entry.size === 1 ? " byte)" : " bytes)")
    );
    appendPropertyRow("Modified", formatTime(entry.modTime));
    if (entry.mode) appendPropertyRow("Permissions", entry.mode);
  }

  // The listing data is shown immediately; /api/stat then refreshes it so the
  // dialog is accurate even when the folder was loaded a while ago.
  let propertiesRequest = 0;

  async function showProperties(entry) {
    const request = ++propertiesRequest;
    els.propertiesModal.hidden = false;
    renderProperties(entry);
    els.propertiesClose.focus();

    try {
      const fresh = await api("/api/stat", entry.path);
      if (request === propertiesRequest && !els.propertiesModal.hidden) {
        renderProperties(fresh);
      }
    } catch (err) {
      setStatus("Failed to refresh properties: " + err.message, true);
    }
  }

  function closeProperties() {
    els.propertiesModal.hidden = true;
  }

  function emptyText() {
    return currentEntries.length === 0 ? "This folder is empty." : "No files match your filter.";
  }

  // Pinned folders are shortcuts: they lead the listing and are not repeated in
  // the normal entries of the current folder.
  function pinnedEntries() {
    const query = filterText.trim().toLowerCase();
    return pinnedFolders
      .filter(function (item) {
        if (item.path === currentPath) return false;
        return !query || item.name.toLowerCase().indexOf(query) !== -1;
      })
      .map(function (item) {
        return (
          currentEntries.find(function (entry) {
            return entry.path === item.path;
          }) || { name: item.name, path: item.path, isDir: true, size: 0, modTime: "" }
        );
      });
  }

  function excludePinned(entries) {
    if (pinnedFolders.length === 0) return entries;
    const pinned = {};
    pinnedFolders.forEach(function (item) {
      pinned[item.path] = true;
    });
    return entries.filter(function (entry) {
      return !pinned[entry.path];
    });
  }

  function renderList() {
    updateSortIndicators();
    const visible = visibleEntries();
    updateCount(currentEntries.length, visible.length);
    const entries = excludePinned(visible);
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

  function sectionRow(label, colSpan) {
    const tr = document.createElement("tr");
    tr.className = "section-row";
    const td = document.createElement("td");
    td.colSpan = colSpan;
    const span = document.createElement("span");
    span.className = "section-label";
    span.textContent = label;
    td.appendChild(span);
    tr.appendChild(td);
    return tr;
  }

  function tableRow(entry, columns, pinned) {
    const tr = document.createElement("tr");
    tr.className = entry.isDir ? "dir" : "file";
    if (pinned) tr.classList.add("pinned-row");
    tr.dataset.path = entry.path;

    columns.forEach(function (col) {
      tr.appendChild(renderCell(col, entry, pinned));
    });

    tr.addEventListener("click", function () {
      if (entry.isDir) navigate(entry.path);
      else openFile(entry);
    });
    return tr;
  }

  function renderTable(entries) {
    els.body.textContent = "";
    const columns = activeColumns();
    const pinned = pinnedEntries();

    if (entries.length === 0 && pinned.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = columns.length;
      td.className = "empty";
      td.textContent = emptyText();
      tr.appendChild(td);
      els.body.appendChild(tr);
      return;
    }

    if (pinned.length > 0) {
      els.body.appendChild(sectionRow("Pinned", columns.length));
      pinned.forEach(function (entry) {
        els.body.appendChild(tableRow(entry, columns, true));
      });
    }
    if (entries.length > 0) {
      if (pinned.length > 0) els.body.appendChild(sectionRow("This folder", columns.length));
      entries.forEach(function (entry) {
        els.body.appendChild(tableRow(entry, columns, false));
      });
    }
  }

  function appendEntryBadges(wrap, entry, pinned) {
    if (pinned) wrap.appendChild(menuIcon("pin", "pin-badge"));
    if (isFavorite(entry.path)) wrap.appendChild(menuIcon("star", "favorite-badge"));
  }

  function renderCell(col, entry, pinned) {
    const td = document.createElement("td");
    td.className = COLUMN_CLASS[col.key];

    if (col.key === "name") {
      const wrap = document.createElement("span");
      wrap.className = "name-inner";
      const icon = document.createElement("span");
      icon.className = "icon";
      renderEntryIcon(icon, entry);
      const label = document.createElement("span");
      label.className = "name-label";
      label.textContent = entry.name;
      wrap.append(icon, label);
      appendEntryBadges(wrap, entry, pinned);
      td.appendChild(wrap);
    } else if (col.key === "size") {
      td.textContent = entry.isDir ? "—" : formatSize(entry.size);
    } else if (col.key === "time") {
      td.textContent = formatTime(entry.modTime);
    } else if (col.key === "actions") {
      const actions = document.createElement("span");
      actions.className = "row-actions";

      if (!entry.isDir) {
        const downloadBtn = document.createElement("button");
        downloadBtn.type = "button";
        downloadBtn.className = "btn row-download icon-action";
        downloadBtn.title = "Download";
        downloadBtn.setAttribute("aria-label", "Download " + entry.name);
        downloadBtn.appendChild(menuIcon("download"));
        downloadBtn.addEventListener("click", function (e) {
          e.stopPropagation();
          downloadEntry(entry);
        });
        actions.appendChild(downloadBtn);
      }

      const deleteBtn = document.createElement("button");
      deleteBtn.type = "button";
      deleteBtn.className = "btn danger row-delete icon-action";
      deleteBtn.title = "Delete";
      deleteBtn.setAttribute("aria-label", "Delete " + entry.name);
      deleteBtn.appendChild(menuIcon("delete"));
      deleteBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        deleteEntry(entry);
      });
      actions.appendChild(deleteBtn);
      td.appendChild(actions);
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

  function gridSection(label) {
    const section = document.createElement("div");
    section.className = "grid-section";
    section.textContent = label;
    return section;
  }

  function gridCard(entry, pinned) {
    const card = document.createElement("div");
    card.className = "card " + (entry.isDir ? "dir" : "file");
    if (pinned) card.classList.add("pinned-card");
    card.tabIndex = 0;

    const icon = document.createElement("div");
    icon.className = "card-icon";
    renderEntryIcon(icon, entry);

    const name = document.createElement("div");
    name.className = "card-name";
    name.textContent = entry.name;
    name.title = entry.name;

    const badges = document.createElement("span");
    badges.className = "card-badges";
    if (pinned) badges.appendChild(menuIcon("pin", "pin-badge"));
    if (isFavorite(entry.path)) badges.appendChild(menuIcon("star", "favorite-badge"));

    const actions = document.createElement("div");
    actions.className = "card-actions";

    if (!entry.isDir) {
      const downloadBtn = document.createElement("button");
      downloadBtn.type = "button";
      downloadBtn.className = "btn card-download icon-action";
      downloadBtn.title = "Download";
      downloadBtn.setAttribute("aria-label", "Download " + entry.name);
      downloadBtn.appendChild(menuIcon("download"));
      downloadBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        downloadEntry(entry);
      });
      actions.appendChild(downloadBtn);
    }

    const deleteBtn = document.createElement("button");
    deleteBtn.type = "button";
    deleteBtn.className = "btn danger card-delete icon-action";
    deleteBtn.title = "Delete";
    deleteBtn.setAttribute("aria-label", "Delete " + entry.name);
    deleteBtn.appendChild(menuIcon("delete"));
    deleteBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      deleteEntry(entry);
    });
    actions.appendChild(deleteBtn);

    card.append(icon, name);
    if (badges.childElementCount > 0) card.appendChild(badges);
    if (!entry.isDir) {
      const meta = document.createElement("div");
      meta.className = "card-meta";
      meta.textContent = formatSize(entry.size);
      card.appendChild(meta);
    }
    card.appendChild(actions);
    card.dataset.path = entry.path;

    card.addEventListener("click", function () {
      if (entry.isDir) navigate(entry.path);
      else openFile(entry);
    });
    card.addEventListener("keydown", function (e) {
      // Ignore Enter/Space on the quick-action buttons inside the card.
      if (e.target !== card) return;
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        if (entry.isDir) navigate(entry.path);
        else openFile(entry);
      }
    });
    return card;
  }

  function renderGrid(entries) {
    els.grid.textContent = "";
    const pinned = pinnedEntries();

    if (entries.length === 0 && pinned.length === 0) {
      const empty = document.createElement("div");
      empty.className = "empty";
      empty.textContent = emptyText();
      els.grid.appendChild(empty);
      return;
    }

    if (pinned.length > 0) {
      els.grid.appendChild(gridSection("Pinned"));
      pinned.forEach(function (entry) {
        els.grid.appendChild(gridCard(entry, true));
      });
    }
    if (entries.length > 0) {
      if (pinned.length > 0) els.grid.appendChild(gridSection("This folder"));
      entries.forEach(function (entry) {
        els.grid.appendChild(gridCard(entry, false));
      });
    }
  }

  async function navigate(path, options) {
    const record = !options || options.recordHistory !== false;
    setStatus("");
    closeViewer();
    try {
      const data = await api("/api/list", path);
      if (data.path !== currentPath) {
        filterText = "";
        els.filter.value = "";
        if (record) {
          navHistory.push(currentPath);
          if (navHistory.length > 50) navHistory.shift();
        }
      }
      currentPath = data.path;
      renderBreadcrumbs(data.path);
      renderEntries(data.entries);
      if (!options || options.recordRecent !== false) {
        recordRecent({ path: data.path, name: pathName(data.path), isDir: true });
      }
    } catch (err) {
      setStatus("Failed to load " + path + ": " + err.message, true);
      renderEntries([]);
    }
    updateBackButton();
  }

  async function openFile(entry) {
    cancelEdit();
    openEntry = entry;
    openFileEditable = false;
    recordRecent(entry);
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
      const parent = entry.path.slice(0, entry.path.lastIndexOf("/"));
      rekeyFolderPaths(entry.path, parent + "/" + name);
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
      forgetFolderPaths(entry.path);
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
  // Inline SVG icons inherit the item colour (including the danger red) and
  // stay crisp at any zoom level.
  const MENU_ICONS = {
    open: '<path d="M3 7a2 2 0 0 1 2-2h3.6a2 2 0 0 1 1.6.8l1 1.2H19a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    preview: '<path d="M2.5 12S6 6 12 6s9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6z"/><circle cx="12" cy="12" r="3"/>',
    rename: '<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M13.5 6.5l4 4"/>',
    download: '<path d="M12 4v11"/><path d="M7 10.5l5 5 5-5"/><path d="M5 20h14"/>',
    delete: '<path d="M4 7h16"/><path d="M9.5 7V4.8h5V7"/><path d="M6.5 7l.9 12.2h9.2L17.5 7"/><path d="M10.5 11v5M13.5 11v5"/>',
    newFolder: '<path d="M3 7a2 2 0 0 1 2-2h3.6a2 2 0 0 1 1.6.8l1 1.2H19a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M12 10.5v5M9.5 13h5"/>',
    newFile: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M12 12v6M9 15h6"/>',
    upload: '<path d="M12 20V9"/><path d="M7 13.5l5-5 5 5"/><path d="M5 4h14"/>',
    list: '<path d="M8.5 6h12M8.5 12h12M8.5 18h12"/><path d="M4 6h.01M4 12h.01M4 18h.01"/>',
    grid: '<rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 7.5h.01"/>',
    properties: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M9 13h6M9 17h4"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    pin: '<path d="M12 17v5"/><path d="M15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1v3.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76z"/>',
    star: '<path d="M12 3.6l2.6 5.3 5.8.8-4.2 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8L3.6 9.7l5.8-.8z"/>',
    recent: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    close: '<path d="M6 6l12 12M18 6L6 18"/>',
  };

  // Element to refocus when the menu closes, if focus was inside the menu.
  let contextReturnFocus = null;

  function hideContextMenu() {
    if (els.contextMenu.hidden) return;
    const hadFocus = els.contextMenu.contains(document.activeElement);
    els.contextMenu.hidden = true;
    els.contextMenu.textContent = "";
    els.favoritesToggle.setAttribute("aria-expanded", "false");
    els.recentToggle.setAttribute("aria-expanded", "false");
    if (hadFocus && contextReturnFocus && contextReturnFocus.isConnected) {
      contextReturnFocus.focus({ preventScroll: true });
    }
    contextReturnFocus = null;
  }

  function menuIcon(name, extraClass) {
    const wrap = document.createElement("span");
    wrap.className = "menu-icon" + (extraClass ? " " + extraClass : "");
    wrap.setAttribute("aria-hidden", "true");
    wrap.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" ' +
      'stroke-linecap="round" stroke-linejoin="round">' +
      (MENU_ICONS[name] || "") +
      "</svg>";
    return wrap;
  }

  // Colour swatches rendered inside the menu for folder entries.
  function colorRow(current, onPick) {
    const row = document.createElement("div");
    row.className = "color-row";
    row.setAttribute("role", "group");
    row.setAttribute("aria-label", "Folder color");

    function addDot(color, className) {
      const dot = document.createElement("button");
      dot.type = "button";
      dot.className = "color-dot" + (className ? " " + className : "");
      if (color) {
        dot.dataset.color = color;
        dot.style.background = "var(--folder-" + color + ")";
      }
      const selected = current === color;
      dot.classList.toggle("selected", selected);
      dot.setAttribute("aria-pressed", selected ? "true" : "false");
      const label = color ? color.charAt(0).toUpperCase() + color.slice(1) : "Default color";
      dot.title = label;
      dot.setAttribute("aria-label", label);
      dot.addEventListener("click", function (e) {
        e.stopPropagation();
        hideContextMenu();
        onPick(color);
      });
      row.appendChild(dot);
    }

    FOLDER_COLORS.forEach(function (color) {
      addDot(color);
    });
    addDot("", "none");
    return row;
  }

  function favoriteMenuItems() {
    if (favorites.length === 0) {
      return [
        { label: "No favorites yet", icon: "star", disabled: true },
        { separator: true },
        { label: "Right-click a file or folder to add it", icon: "info", disabled: true },
      ];
    }
    return favorites.map(function (item) {
      return {
        label: item.name,
        title: item.path,
        icon: item.isDir ? "open" : "preview",
        action: function () {
          if (item.isDir) {
            navigate(item.path);
          } else {
            openFile({ name: item.name, path: item.path, isDir: false, size: 0, modTime: "" });
          }
        },
        remove: function () {
          removeFavorite(item.path);
        },
      };
    });
  }

  function recentMenuItems() {
    if (recent.length === 0) {
      return [
        { label: "No recent items yet", icon: "recent", disabled: true },
        { separator: true },
        { label: "Folders you visit and files you open appear here", icon: "info", disabled: true },
      ];
    }
    const items = recent.map(function (item) {
      return {
        label: item.name,
        title: item.path,
        icon: item.isDir ? "open" : "preview",
        action: function () {
          if (item.isDir) {
            navigate(item.path);
          } else {
            openFile({ name: item.name, path: item.path, isDir: false, size: 0, modTime: "" });
          }
        },
        remove: function () {
          removeRecent(item.path);
        },
      };
    });
    items.push({ separator: true });
    items.push({ label: "Clear recent", icon: "delete", danger: true, action: clearRecent });
    return items;
  }

  function viewMenuItems() {
    return [
      { label: "List view", icon: "list", checked: viewMode === "list", action: function () { applyView("list"); } },
      { label: "Grid view", icon: "grid", checked: viewMode === "grid", action: function () { applyView("grid"); } },
    ];
  }

  function globalMenuItems() {
    return [
      { label: "New folder", icon: "newFolder", action: function () { createFolder(); } },
      { label: "New file", icon: "newFile", action: function () { createFile(); } },
      { label: "Upload files…", icon: "upload", action: function () { els.fileInput.click(); } },
      { separator: true },
    ]
      .concat(viewMenuItems())
      .concat([
        { separator: true },
        { label: "Server info", icon: "info", action: function () { openInfo(); } },
      ]);
  }

  function buildMenu(x, y, items, title) {
    hideContextMenu();
    els.contextMenu.textContent = "";

    if (title) {
      const heading = document.createElement("div");
      heading.className = "context-title";
      heading.textContent = title;
      heading.title = title;
      els.contextMenu.appendChild(heading);
    }

    items.forEach(function (item) {
      if (item.separator) {
        const sep = document.createElement("div");
        sep.className = "context-sep";
        els.contextMenu.appendChild(sep);
        return;
      }
      if (item.swatches) {
        els.contextMenu.appendChild(colorRow(item.current, item.onPick));
        return;
      }
      const button = document.createElement("button");
      button.type = "button";
      button.className =
        "context-item" + (item.danger ? " danger" : "") + (item.checked ? " checked" : "");
      button.setAttribute("role", "menuitem");
      if (item.title) button.title = item.title;
      if (item.checked) button.setAttribute("aria-checked", "true");
      button.appendChild(menuIcon(item.icon));

      const label = document.createElement("span");
      label.className = "menu-label";
      label.textContent = item.label;
      button.appendChild(label);

      if (item.checked) button.appendChild(menuIcon("check", "menu-check"));

      if (item.disabled) {
        button.disabled = true;
        button.classList.add("is-disabled");
      } else {
        button.addEventListener("click", function () {
          hideContextMenu();
          item.action();
        });
      }

      if (!item.remove) {
        els.contextMenu.appendChild(button);
        return;
      }

      // A row with a trailing remove button (used by the favorites menu).
      const row = document.createElement("div");
      row.className = "context-row";
      row.appendChild(button);

      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "context-remove";
      remove.title = "Remove from favorites";
      remove.setAttribute("aria-label", "Remove " + item.label + " from favorites");
      remove.appendChild(menuIcon("close"));
      remove.addEventListener("click", function () {
        hideContextMenu();
        item.remove();
      });
      row.appendChild(remove);
      els.contextMenu.appendChild(row);
    });

    contextReturnFocus = document.activeElement;
    els.contextMenu.hidden = false;

    // Keep the menu fully on screen, including when it opens near the top or
    // left edge, and let it scroll if it is taller than the viewport.
    const rect = els.contextMenu.getBoundingClientRect();
    const left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8));
    const top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8));
    els.contextMenu.style.left = left + "px";
    els.contextMenu.style.top = top + "px";

    const first = els.contextMenu.querySelector(".context-item:not(:disabled)");
    if (first) first.focus({ preventScroll: true });
  }

  function showContextMenu(x, y, entry) {
    const favoriteItem = {
      label: isFavorite(entry.path) ? "Remove from favorites" : "Add to favorites",
      icon: "star",
      action: function () { toggleFavorite(entry); },
    };
    const items = entry.isDir
      ? [
          { label: "Open", icon: "open", action: function () { navigate(entry.path); } },
          { label: "Rename", icon: "rename", action: function () { renameEntry(entry); } },
          favoriteItem,
          { separator: true },
          {
            label: isPinned(entry.path) ? "Unpin folder" : "Pin folder",
            icon: "pin",
            action: function () { togglePin(entry); },
          },
          {
            swatches: true,
            current: folderColor(entry.path),
            onPick: function (color) { setFolderColor(entry, color); },
          },
          { separator: true },
          { label: "Properties", icon: "properties", action: function () { showProperties(entry); } },
          { label: "Delete", icon: "delete", danger: true, action: function () { deleteEntry(entry); } },
        ]
      : [
          { label: "Preview", icon: "preview", action: function () { openFile(entry); } },
          { label: "Rename", icon: "rename", action: function () { renameEntry(entry); } },
          favoriteItem,
          { label: "Download", icon: "download", action: function () { downloadEntry(entry); } },
          { separator: true },
          { label: "Properties", icon: "properties", action: function () { showProperties(entry); } },
          { label: "Delete", icon: "delete", danger: true, action: function () { deleteEntry(entry); } },
        ];
    items.push({ separator: true });
    items.push.apply(items, globalMenuItems());
    buildMenu(x, y, items, entry.name);
    els.contextMenu.dataset.source = "entry";
  }

  function showViewMenu(x, y) {
    buildMenu(x, y, globalMenuItems(), currentPath);
    els.contextMenu.dataset.source = "view";
  }

  // The header star opens the same menu component as the context menu, anchored
  // under the button.
  function showFavoritesMenu() {
    const rect = els.favoritesToggle.getBoundingClientRect();
    buildMenu(rect.right, rect.bottom + 6, favoriteMenuItems(), "Favorites");
    els.contextMenu.dataset.source = "favorites";
    els.favoritesToggle.setAttribute("aria-expanded", "true");
  }

  function showRecentMenu() {
    const rect = els.recentToggle.getBoundingClientRect();
    const title = recent.length ? "Recent (" + recent.length + ")" : "Recent";
    buildMenu(rect.right, rect.bottom + 6, recentMenuItems(), title);
    els.contextMenu.dataset.source = "recent";
    els.recentToggle.setAttribute("aria-expanded", "true");
  }

  els.recentToggle.addEventListener("click", function (e) {
    e.stopPropagation();
    if (!els.contextMenu.hidden && els.contextMenu.dataset.source === "recent") {
      hideContextMenu();
      return;
    }
    showRecentMenu();
  });

  els.favoritesToggle.addEventListener("click", function (e) {
    // Stop propagation so the document click handler does not close the menu
    // again right after it opens.
    e.stopPropagation();
    if (!els.contextMenu.hidden && els.contextMenu.dataset.source === "favorites") {
      hideContextMenu();
      return;
    }
    showFavoritesMenu();
  });

  // Arrow keys move through the open menu, Home/End jump to its ends, and Tab
  // dismisses it. Escape is handled by the global keydown listener.
  els.contextMenu.addEventListener("keydown", function (e) {
    const items = Array.prototype.slice.call(
      els.contextMenu.querySelectorAll(".context-item:not(:disabled), .color-dot, .context-remove")
    );
    if (items.length === 0) return;
    const index = items.indexOf(document.activeElement);

    if (e.key === "ArrowDown") {
      e.preventDefault();
      items[index < 0 ? 0 : (index + 1) % items.length].focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      items[index < 0 ? items.length - 1 : (index - 1 + items.length) % items.length].focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      items[0].focus();
    } else if (e.key === "End") {
      e.preventDefault();
      items[items.length - 1].focus();
    } else if (e.key === "Tab") {
      hideContextMenu();
    }
  });

  // The menu is anchored to the pointer, so it would drift on scroll or when
  // the window resizes. Close it instead (unless the menu itself scrolls).
  window.addEventListener("scroll", function (e) {
    if (els.contextMenu.hidden) return;
    if (e.target && e.target.nodeType === 1 && els.contextMenu.contains(e.target)) return;
    hideContextMenu();
  }, true);
  window.addEventListener("resize", function () {
    hideContextMenu();
    updateBreadcrumbFade();
  });

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
    let entry =
      target &&
      currentEntries.find(function (item) {
        return item.path === target.dataset.path;
      });
    // A pinned shortcut can live in another folder, so it is not in
    // currentEntries; fall back to the synthetic pinned entry.
    if (!entry && target) {
      const path = target.dataset.path;
      entry = pinnedEntries().find(function (item) {
        return item.path === path;
      });
    }
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

  els.viewerDownload.prepend(menuIcon("download"));
  els.viewerDownload.classList.add("btn-with-icon");

  els.viewerRename.addEventListener("click", function () {
    if (openEntry) renameEntry(openEntry);
  });

  els.viewerEdit.addEventListener("click", startEdit);
  els.viewerSave.addEventListener("click", saveEdit);
  els.viewerCancel.addEventListener("click", cancelEdit);

  els.fileInput.addEventListener("change", function () {
    uploadFiles(els.fileInput.files);
  });

  // The header button is the upload path on touch devices, where the
  // right-click context menu is awkward or unavailable.
  els.uploadToggle.addEventListener("click", function () {
    els.fileInput.click();
  });

  els.navBack.addEventListener("click", goBack);
  els.breadcrumbs.addEventListener("scroll", updateBreadcrumbFade, { passive: true });

  els.infoToggle.addEventListener("click", openInfo);
  els.infoClose.addEventListener("click", closeInfo);
  els.infoModal.addEventListener("click", function (e) {
    if (e.target === els.infoModal) closeInfo();
  });

  els.propertiesClose.addEventListener("click", closeProperties);
  els.propertiesModal.addEventListener("click", function (e) {
    if (e.target === els.propertiesModal) closeProperties();
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
    if (e.key === "Escape" && !els.contextMenu.hidden) {
      hideContextMenu();
    } else if (e.key === "Escape" && !els.uploadModal.hidden) {
      if (uploadRequest) uploadRequest.abort();
      else closeUploadModal();
    } else if (e.key === "Escape" && !els.columnsMenu.hidden) {
      hideColumnsMenu();
    } else if (e.key === "Escape" && !els.infoModal.hidden) {
      closeInfo();
    } else if (e.key === "Escape" && !els.propertiesModal.hidden) {
      closeProperties();
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
  applyGridSize(gridSize);
  updateFavoritesButton();
  updateRecentButton();
  applyView(viewMode);
  navigate(currentPath, { recordRecent: false });
})();
