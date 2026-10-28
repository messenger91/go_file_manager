# AGENTS.md

## What this is

File manager: a Go HTTP server (JSON API + embedded web UI) exposing a single
root folder. Reads are guarded; every entry-level mutation exists too
(mkdir, create, write, upload, rename, move, copy, chmod, trash, extract).
No external dependencies — **stdlib only, on purpose**. There is no `go.sum`;
adding a third-party import is a design change, not a detail.

Requires **Go 1.25+** (the router uses method patterns like `"GET /api/list"`).

## Git

GitHub repo `messenger91/go_file_manager`, default branch `main`, remote
`origin`. `.env`, `bin/` and `deploy_bin.sh` are gitignored — commit
`.env.example`, never `.env`. Pushing from a non-interactive sandbox fails
with `could not read Username for 'https://github.com'`; the user pushes
from their own authenticated terminal.

## Commands

```sh
make run            # go run . using ./.env -> http://localhost:8080
make build          # bin/go_file_manager (host)
make build-linux    # static CGO_ENABLED=0 Linux amd64 + arm64
make test           # go test ./...
make vet            # go vet ./...
make fmt            # gofmt
```

Single test: `go test ./internal/files -run TestResolveStaysWithinRoot`

Tests: `internal/files` (root confinement, symlink escapes, listing order,
delete safety) and `internal/server` (login/session flow, session-file
persistence, malformed password hashes, throttling, share TTL parsing), plus
small ones in `internal/host` (smoke test of `Collect()`) and
`internal/version` (semver check); `internal/config` has none.

Always run `make fmt vet test` before finishing; all three currently pass.

**Bump the version** in `internal/version/version.go` with every change —
minor for a feature, patch for a fix (major for breaking) — since the Server
info dialog and the startup log both show it.

## Layout

- `main.go` — entrypoint, embeds `web/` via `//go:embed all:web`, listens on
  `:PORT`.
- `internal/config` — env/`.env` loading; precedence is
  **process env > `.env` > defaults**, and `.env` is read from the current
  working directory.
- `internal/files` — root-confined FS ops (`List`, `Search`, `Stat`,
  `ReadFile`, `Open`, `Delete`, `Mkdir`, `Rename`, `Move`, `Copy`, `Trash`,
  `EmptyTrash`, `Create`, `Write`, `Upload`, `Zip`, `ZipMulti`, `Extract`,
  `Chmod`, `Disk`). This is the security boundary; test changes here.
- `internal/server` — routes, auth (login form, sessions, PBKDF2 in
  `password.go`, login throttle, share links in `share.go`), JSON responses
  and the error→status mapping.
- `cmd/hashpw` — prints a `PASSWORD_HASH` for `.env`
  (`go run ./cmd/hashpw 'password'`).
- `internal/host` — host info for `GET /api/info`; reads `/proc` and
  `/etc/os-release`, unavailable values stay zero so it compiles on any OS.
- `internal/version` — the application version constant.
- `web/` — static UI (`index.html`, `login.html`, `static/style.css`,
  `static/app.js`). **Assets are embedded at build time**, so `web/` edits
  need a rebuild/restart, not just a refresh.
- `TODO.md` is the original project plan, not current scope — don't treat it
  as a backlog.

## Config & auth gotchas

- `HOME_DIR` is required; the server exits if unset. `ADMIN`/`PASSWORD`
  default to `admin`/`admin`, `PORT` to `8080`. `PORT` is a **port only** —
  the listener is always `:PORT` (all interfaces); restrict access with a
  firewall or reverse proxy (README has a systemd + nginx guide).
- Auth is a **login form + session cookie** (`gfm_session`, 24h TTL), not a
  browser Basic-auth prompt: the server never sends `WWW-Authenticate`.
  Unauthenticated `/api/*` calls get `401` JSON; page requests redirect to
  `/login?next=…`. Only `/login`, `POST /logout`, `/static/`, and
  `GET /s/<token>` (share links) are public. HTTP Basic is still accepted on
  every route for scripts (`curl -uadmin:admin`).
- `PASSWORD_HASH` (`pbkdf2-sha256$…` from `cmd/hashpw`) replaces the
  plaintext `PASSWORD` when set; a malformed hash **fails closed** (refuses
  every password) rather than downgrading to admin/admin.
- Login throttling: 5 failures per source host bounce further attempts for a
  window starting at 5 s that doubles up to a 5 min cap (`loginThrottle`); it
  is answered exactly like a wrong password.
- `SESSIONS_FILE` persists sessions as JSON (mode 0600) across restarts;
  without it they are memory-only and a restart logs everyone out.

## API fundamentals

- Path args are root-relative slash paths (`?path=/sub/file`). `..` traversal
  is **clamped into the root** (nonexistent → 404, not an error reply); a
  symlink resolving outside the root returns 403. Keep both behaviors.
- Mutations (delete, mkdir, rename, move, copy, trash, create, write,
  upload, extract, chmod) use `resolveTarget`, not `resolve`: the parent is
  resolved but **never the final element**, so operating on a symlink affects
  the link and leaves its target intact. `Write` additionally refuses a
  symlink target outright (`ErrForbidden`) — `os.WriteFile` would follow it.
- Error → status mapping (single switch in `server.go` over `errors.Is`):
  `ErrForbidden`/`ErrPermission` 403 · `ErrNotExist` 404 ·
  `ErrRootOperation`, `ErrInvalidName`, `ErrMoveIntoSelf`,
  `ErrCopyIntoSelf`, `ErrUnsupported`, `ErrInTrash` 400 ·
  `ErrArchiveTooLarge` 413 · `ErrDirNotEmpty`, `ErrExist` 409.
- Move/copy/trash subtree checks compare cleaned relative paths **with a
  trailing slash**, so `/docs2` is never treated as inside `/docs`.
- Nothing overwrites implicitly: `write` requires an existing file,
  `create`/`upload` refuse an existing path, mkdir/rename/move/copy onto an
  existing path returns 409, a missing source/parent 404, a non-empty
  directory without `recursive=true` 409, moving/copying a folder into its
  own subtree 400, operating on the root 400.
- Size limits: `MaxReadSize` 2 MiB (`/api/read` refuses bigger files so
  callers download), `MaxWriteSize` 8 MiB, `MaxUploadSize` 50 MiB
  (`MaxBytesReader` → 413 with a "larger than the 50 MB limit" message; the
  limit is on the **whole multipart body**, incl. MIME overhead). Extract is
  capped at `MaxExtractSize` 1 GiB.
- Browsers must call `/api/metadata`, never `/api/stat` for properties:
  generic EasyPrivacy rules match `/api/stat?` and ad blockers kill the
  request (`TypeError: Failed to fetch`). `/api/stat` stays as a server-side
  alias for scripts.
- `/api/download` sets `Content-Type` from the `mediaTypes` map in
  `server.go` but keeps Disposition `attachment`, so scripts always get a
  download while media previews can stream inline from it.

## Trash, zip, extract, search, shares

- Trash (`POST /api/trash`) moves the entry into `/.trash` at the root,
  creating that folder on first use. Collisions inside `.trash` get
  " 2", " 3"… inserted before the extension (`uniqueTrashedName`), so
  trashing never overwrites. The root, the trash folder itself and anything
  inside it return `ErrInTrash`. `/.trash` starts with a dot, so listings
  hide it unless hidden files are shown. `EmptyTrash` removes every entry
  (missing folder counts as 0) via a confirmed UI action — emptying is
  irreversible, there is no per-item soft delete.
- Zip downloads embed the folder itself as the archive root so extraction
  doesn't splatter files. Repeated `path=` args use `ZipMulti` (files at the
  archive root, folders as subtrees, " 2"-style collision names via
  `uniqueArcname`). Symlinks are stored as links (`os.Readlink` target as
  body), unreadable subtrees and special files are skipped; only the upfront
  `Stat` of each path yields 403/404/400.
- `Extract` unpacks into a new folder named after the archive and refuses an
  existing destination (409). When every entry sits under one top-level
  folder, that wrapper is stripped (`stripCommonRoot`). Safety validation
  happens **before the first byte is written**: zip-slip → `ErrInvalidName`,
  symlink entries → `ErrUnsupported`, total uncompressed size over 1 GiB →
  `ErrArchiveTooLarge`. A failure removes the partial destination like
  `Copy`.
- Search matches names case-insensitively by substring, walks from `path`
  (default `/`), applies the same dot filter as `/api/list`, never follows
  directory symlinks, caps at `MaxSearchResults` (200, reported with
  `"truncated": true`), and rejects an empty `q` with 400. With
  `Content: true`, regular files ≤ `MaxContentSearchSize` (1 MiB) are
  scanned too (NUL-sniffing binaries skipped), reporting the first matching
  line as `excerpt` + 1-based `line`; the 64 MiB `contentSearchBudget` var
  (tests shrink it) reports `"scanLimit": true` without stopping the walk.
- Share links are self-service public URLs: `POST /api/share?path=…&ttl=…`
  (`7d`, `12h`, `30m` handled by `parseShareTTL`). Tokens are 16 random
  bytes (hex), pure map keys — no path data in the URL. `GET /s/<token>` is
  public, serves mapped Content-Types inline (otherwise attachment), folders
  as zip. Shares live **in memory**: a restart revokes everything and a
  renamed/deleted entry makes its link 404.

## Web UI conventions (app.js + style.css)

Restart (rebuild) after any `web/` edit. Hard-won rules:

- Row separators are `border-bottom` on `tbody tr`, **never on `td`** — a
  border on a wrapped cell breaks the row line. `td.name` must stay a table
  cell; its flex layout lives on the inner `.name-inner` span.
- Folders use the inline `FOLDER_SVG` so they can be colour-tagged; the fill
  comes from `.folder-icon[data-color="…"] .folder-svg` in the stylesheet.
  Don't set a fill on the wrapper — the `.folder-svg` rule would win.
- Icons are inline-SVG path strings in `MENU_ICONS`, inheriting
  `currentColor` (danger items go red). Menus (context, view, and the
  header menus tags/notes/recent/favorites/settings) share `buildMenu()`,
  which takes item objects: `{label, icon, danger, checked, action}`,
  `{separator: true}`, `{swatches, current, onPick}`, and
  `{title, detail, remove, disabled}` for header-menu rows. The menu clamps
  to the viewport, scrolls when taller, focuses its first item, supports
  ↑↓/Home/End, and closes on scroll/resize (it is anchored to the pointer);
  Escape returns focus. Optional group headings with an `action` become
  clickable `context-group-action` buttons.
- `previewKind(name)` buckets by the extension lists at the top of app.js:
  "image"/"video"/"audio"/"pdf" (rendered streaming from `/api/download`
  with Edit always hidden) or "text" (`/api/read`, ≤2 MiB, editable when
  there are no NUL bytes). The same lists drive grid thumbnails (Settings
  toggle, `thumbnails` localStorage key, default off). `closeViewer()`
  empties the media element, which also stops playback.
- Virtual views (tags, trash, search) share machinery: `enterVirtualView()`
  resets the filter, disables upload, closes the search bar and bumps the
  shared `viewSeq` (stale responses are dropped); `refreshCurrentView()`
  re-runs whichever view is active after a mutation; `navigate()` clears all
  three, re-enables upload and records history; `pinnedEntries()`/
  `excludePinned()` keep them out of the Pinned section; `emptyText()`/
  `showViewMenu()` carry per-view wording.
- Navigation is in-page only: `navigate()` never calls `history.pushState`,
  so the browser Back button leaves the app. The in-app `#nav-back` uses its
  own `navHistory` (capped at 50) and falls back to the parent folder;
  `goBack()` pops with `{recordHistory: false}` so back doesn't re-record.
  A same-path refresh (after a mutation) also doesn't record.
- Browser-local state is a fixed set of localStorage keys: `showHidden`,
  `theme`, `columns`, `gridSize`, `thumbnails`, `pinnedFolders`,
  `folderColors`, `favorites`, `recent`, `hiddenPaths`, `notes`. JSON ones
  go through `saveStored()`/`loadStored()`; `gridSize` is a plain string.
  `folderColor()` validates against `FOLDER_COLORS` so a tampered entry
  cannot inject CSS; only palette-valid colours are listed anywhere.
- Rename runs `rekeyFolderPaths()` (pins, favorites, tags, notes, hidden
  marks, recents follow the subtree) and delete runs
  `forgetFolderPaths()` (prunes them). A **copy inherits none of them**.
  Restore/moving from trash and copies instead avoid collisions by renaming
  with `uniqueRestoreName()`/`uniqueCopyName()` ("a.txt" → "a 2.txt").
- Keyboard navigation sits at the **END** of the global keydown chain so
  modals win, is gated by `anyPanelOpen()` + `isTextInput(e.target)`, and
  Enter yields to a focused card's own handler. A cursor (`cursorPath`,
  `.cursor` class) marks the entry ↑/↓ act on; shift extends selection,
  Enter opens, F2 renames, Delete trashes, Backspace calls `goBack()`, `?`
  opens the shortcuts sheet. `navigableEntries()` skips the Pinned section.
- Multi-select: Ctrl/Cmd+click toggles, Shift+click ranges from
  `lastSelectedPath`, Ctrl+A selects visible, and Select mode
  (`#select-toggle`) makes plain clicks select (the touch path). The batch
  bar (`#batch-bar`) offers Trash / Move / Download / Delete / Clear with
  per-entry failure counting (`batchSuffix`) and one
  `refreshCurrentView()` at the end. `navigate()` clears the selection so
  paths never leak across listings.
- Drag-and-drop upload reuses `uploadFiles()` with `{file, path}` items:
  `dragCarriesFiles()` ignores internal drags, `dragDepth` counts
  enter/leave pairs for the highlight, and a dropped folder is walked via
  `webkitGetAsEntry` (`readEntries` returns at most 100 per call — batch
  loop in `collectDroppedEntries()`). `uploadsAllowed()` refuses drops in
  virtual views; a document-level `drop` preventDefaults so the browser
  never navigates away.
- The destination picker (`#destination-modal`) serves Move, Copy,
  batch-move and Restore modes; it lists only folders, excludes the moved
  entry's own subtree from navigation, and disables OK at the current
  folder for move/restore.
- Properties (`showProperties`) opens immediately with listing data, then
  silently re-fetches `/api/metadata` (best-effort — a slow response is
  dropped by the `propertiesRequest` counter and must **not** turn into an
  error). `/api/stat` would be ad-blocked (see above).
- Share UI: `shareEntry()` posts the TTL choice and copies
  `location.origin + url` (fallback: status line on non-secure contexts);
  the Settings menu fetches `GET /api/shares` fresh each time and renders
  revocable rows (a fetch failure just drops the rows).
- The nine header controls (tags, notes, recent, favorites, upload, info,
  view, settings, logout) plus back/breadcrumbs must fit down to 320px:
  below 400px the ℹ️ button hides and the eight icons shave to 34px.
  Re-check 320–400px layouts when adding another header control.
- List/grid quick actions share one builder (`renderCell()`/
  `renderGrid()` → `.row-actions` / `.card-actions`); hover dimming is
  disabled under `@media (hover: none)`. The card `keydown` handler ignores
  events whose target is not the card so Enter on a quick action doesn't
  also open the preview.
- The Columns menu persists visibility + order (`columns` key,
  `[[key, visible], …]`) via `renderTableHead()`/`activeColumns()`; Modified
  starts hidden below 640px, at least one column must stay visible, actions
  pinned last. Grid size is a `size-s|m|l` class on `#grid` plus
  `aria-pressed`; the toggle hides in list view.

## Misc

- `files.Disk` uses `syscall.Statfs`, so it is Unix-only (fine for the
  Linux target; a Windows build would fail).
- `FreeBytes` is space available to the process (`Bavail`), so
  `used + free < total` by the reserved blocks; the UI divides by
  `used + free` so its percentage matches `df`.
