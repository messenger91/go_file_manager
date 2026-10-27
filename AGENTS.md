# AGENTS.md

## What this is

File manager: a Go HTTP server (JSON API + embedded web UI) that exposes everything
under a single root folder. Browsing and previewing are read-only; deleting is
supported. No external dependencies — **stdlib only, on purpose**. There is no
`go.sum`; adding a third-party import is a design change, not a detail.

Requires **Go 1.25+** (the router uses method patterns like `"GET /api/list"`).

## Git

GitHub repo `messenger91/go_file_manager`, default branch `main`, remote `origin`.
`.env` and `bin/` are gitignored — commit `.env.example`, never `.env`. Pushing from
a non-interactive sandbox fails with `could not read Username for
'https://github.com'`; the user pushes from their own authenticated terminal.

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

Tests live in `internal/files` (root confinement, symlink escapes, listing order, delete
safety) and `internal/server` (login/session flow, auth redirects, logout), plus small
ones in `internal/host` (smoke test of `Collect()`) and `internal/version` (semver check
on the constant); `internal/config` has none.

Always run `make fmt vet test` before finishing; all three currently pass. **Bump the version**
in `internal/version/version.go` with every change — minor for a feature, patch for a fix
(major for a breaking change) — since the Server info dialog and the startup log show it.

## Layout

- `main.go` — entrypoint; embeds `web/` via `//go:embed all:web` and serves it.
- `internal/config` — env/`.env` loading. Precedence: **process env > `.env` > defaults**.
  `.env` is read from the current working directory.
- `internal/files` — root-confined filesystem access (`List`, `Search`, `Stat`, `ReadFile`,
  `Open`, `Delete`, `Mkdir`, `Rename`, `Move`, `Copy`, `Trash`, `EmptyTrash`, `Create`,
  `Write`, `Upload`, `Zip`, `Disk`). This is the security boundary; test changes here.
- `internal/server` — HTTP routes, authentication (login form + sessions, PBKDF2 hash
  verification in `password.go`, login throttle, share links in `share.go`), JSON/error
  handling.
- `cmd/hashpw` — prints a `PASSWORD_HASH` value for `.env`; thin wrapper over
  `server.NewPasswordHash` (`go run ./cmd/hashpw 'password'`).
- `internal/host` — host info for `GET /api/info` (hostname, OS name, arch, uptime, load
  average, memory). Read from `/proc` and `/etc/os-release`; unavailable values are left
  zero, so it compiles and runs on any OS.
- `internal/version` — the application version constant, reported by `GET /api/info`,
  shown in the Server info dialog and logged at startup.
- `web/` — static UI (`index.html`, `login.html`, `static/style.css`, `static/app.js`).
  **Assets are embedded at build time**, so edits require a rebuild/restart, not just a
  refresh.

## Gotchas

- `HOME_DIR` is required; the server exits if it is unset. `ADMIN`/`PASSWORD` default to
  `admin`/`admin`; `PORT` defaults to `8080`. `PORT` is a **port only** — the listener is
  always `:PORT` (all interfaces); there is no host/bind setting, so restrict access with
  a firewall or reverse proxy. `README.md` has a systemd + nginx deployment guide.
- Auth is a **login form with a session cookie** (`gfm_session`, 24h TTL), not a browser
  Basic-auth prompt: the server never sends `WWW-Authenticate`, so the native dialog can't
  appear. Unauthenticated `/api/*` calls get `401` JSON; page requests redirect to
  `/login?next=…`. Only `/login`, `POST /logout`, `/static/`, and `GET /s/<token>` (share
  links) are public. HTTP Basic is still accepted on every route for scripts
  (`curl -u admin:admin`). `PASSWORD_HASH` (`pbkdf2-sha256$…`, from `cmd/hashpw`) replaces
  the plaintext `PASSWORD` when set; a malformed hash **fails closed** (a `broken`
  verifier refuses every password) rather than downgrading to the default admin/admin.
  The login form throttles guessing: 5 failures per source host bounce further attempts
  for a window that doubles up to 5 min (`loginThrottle`), answered exactly like a wrong
  password. `SESSIONS_FILE` persists sessions as JSON (0600) across restarts; without it
  they are memory-only and a restart logs everyone out. `internal/server` holds the auth
  tests; run them with `make test`.
- API path args are root-relative slash paths in `?path=/sub/file`. Traversal (`..`) is
  **clamped into the root** (yields 404 for nonexistent paths), not rejected; symlink
  escapes that resolve outside the root return 403. Keep both behaviors.
- `/api/read` refuses files over `files.MaxReadSize` (2 MiB) so callers download instead.
  `/api/list` omits entries whose name starts with a dot unless `hidden=1` is passed; the
  UI sends it only when the settings toggle is on (default: hidden).
- Every mutation (delete, mkdir, rename, move, copy, trash, create, write, upload) uses `resolveTarget`
  (not `resolve`): it resolves the parent but never the final element, so operating on a
  symlink affects the link and leaves its target intact, even when the target is outside
  the root. `Write` additionally refuses a symlink target outright (`ErrForbidden`, 403).
  Operating on the root returns `ErrRootOperation` (400), as does an invalid new name
  (`ErrInvalidName`) and moving/copying a folder into its own subtree
  (`ErrMoveIntoSelf`/`ErrCopyIntoSelf`) or copying a special file (`ErrUnsupported`). A
  non-empty directory without `recursive=true` returns `ErrDirNotEmpty` (409);
  `Mkdir`/`Rename`/`Create`/`Upload`/`Move`/`Copy` onto an existing path returns
  `os.ErrExist` (409) and a missing source/parent returns `os.ErrNotExist` (404).
- Size limits: `files.MaxReadSize` (2 MiB, `/api/read`), `files.MaxWriteSize` (8 MiB,
  `/api/write` body), `files.MaxUploadSize` (50 MiB, `/api/upload`). Overwriting is never
  implicit: write requires an existing file, create/upload refuse an existing path. An
  upload over the limit is rejected by `MaxBytesReader` and mapped to `413` with a
  "larger than the 50 MB limit" message (the limit is on the **whole multipart body**, so
  it includes MIME framing overhead).
- Media preview: `openFile()` calls `previewKind(name)` — "image", "video", "audio" or
  "pdf" for the extension lists at the top of app.js, "text" otherwise. Text keeps the
  old `/api/read` path (≤2 MiB, editable when no NUL bytes); the other kinds render into
  `#viewer-media` (an `<img>`/`<video controls>`/`<audio controls>`/`<embed
  type="application/pdf">` streaming from `/api/download`) with the `<pre>` body hidden,
  and Edit is always hidden — only Delete/Rename/Download make sense. `/api/download`
  sets `Content-Type` from the `mediaTypes` map in server.go (browsers sniff `<img>` and
  `<video>` by content, but `<embed>` needs the real type); the Disposition stays
  `attachment`, so scripts still get a download. `closeViewer()` empties `#viewer-media`,
  which also stops playback.
- Folder download: `GET /api/download-zip?path=/docs` calls `files.Zip(rel, w)`, which
  streams `archive/zip` with the folder itself as the archive root (extracting does not
  splatter files). Several `path=` arguments call `files.ZipMulti` instead: files land at
  the archive root, folders as subtrees, name collisions get " 2" appended
  (`uniqueArcname`), and the response is `selected.zip` — that is what the batch bar's
  Download uses. Symlinks are stored as links (`os.Readlink` target as the entry body,
  `ModeSymlink`), unreadable subtrees and special files are skipped, and errors after the
  first byte only get logged — the handler does an upfront `Stat` of every path for the
  403/404/400 cases ("not a folder" stays reserved for a single file). The UI's
  `downloadEntry()` dispatches by `isDir`, and folders carry a Download quick action in
  list, grid and context menu.
- Share links are self-service public URLs: the context menu's "Copy share link"
  (`shareEntry()`) posts `POST /api/share?path=…&ttl=…` (ttl optional — `7d`, `12h`,
  `30m` are parsed by `parseShareTTL`) and copies `location.origin + url` to the
  clipboard, falling back to showing it in the status line when the clipboard is
  unavailable (non-secure contexts). `GET /s/<token>` is public and serves the entry:
  mapped Content-Types inline, otherwise `attachment`, folders as zip. Tokens are 16
  random bytes (hex) and are pure map keys — no path data in the URL. `GET /api/shares`
  lists them; the Settings menu renders one revocable row per share
  (`showSettingsMenu` is async and fetches them fresh; a failure just drops the rows).
  Shares live in memory: a restart revokes everything, and a renamed/deleted entry
  makes its link 404.
- Multi-select: Ctrl/Cmd+click toggles, Shift+click ranges (anchored at
  `lastSelectedPath`), Ctrl+A selects all visible (skipped inside inputs), and the
  toolbar's Select button (`#select-toggle`, `aria-pressed`) enters Select mode where
  plain clicks select instead of opening — the touch path. Selection lives in the
  `selectedPaths` map; the batch bar (`#batch-bar`, shown while selecting or when
  non-empty) offers Trash / Move to… / Download / Delete / Clear with per-entry failure
  counting (`batchSuffix`), one `refreshCurrentView()` at the end and
  `rekeyFolderPaths()` per moved entry. Batch move opens the destination picker with
  `pickerMode: "batch-move"` and `pickerEntry = null` (OK disabled at the current
  folder); Delete confirms once. Rows/cards get `.selected`; `navigate()` and
  `enterVirtualView()` clear the selection, so batch paths can never leak across
  listings — stale paths are skipped by `selectionEntries()`.
- Drag-and-drop upload: `dragenter`/`dragover`/`dragleave`/`drop` on `main` feed
  `e.dataTransfer.files` into the existing `uploadFiles()` flow. `dragCarriesFiles()`
  checks the `Files` type so internal drags (column reorder) are ignored;
  `uploadsAllowed()` mirrors the 📤 button's disabled state, so virtual views refuse
  drops too. `dragDepth` counts enter/leave pairs for the `.drag-over` highlight, and a
  document-level `drop` handler preventDefaults file drops outside the listing so the
  browser never navigates to a dropped file.
- Grid thumbnails: the Settings menu's "Show image thumbnails" toggle (`thumbnails`
  localStorage key, default off, `toggleThumbnails()` re-renders without a refetch)
  makes `gridCard()` render image files (same `previewKind()` lists as the viewer) as a
  lazy `<img class="card-thumb">` from `/api/download`, square via `aspect-ratio`, with
  an `error` fallback to the file emoji. List rows keep their emoji.
- Move, copy and restore share one destination picker (`#destination-modal`). "Move to…"
  and "Copy to…" in the context menu call `openPicker(entry, "move"|"copy")`; it starts at
  the entry's parent, lists only folders from `/api/list` (excluding the entry itself, so
  its subtree can never be entered), focuses the first row, and swaps the title and OK
  label ("Move here"/"Copy here"). Move is disabled while the shown folder is the current
  parent; copy is allowed there and duplicates under `uniqueCopyName()` ("note copy.txt",
  then "note copy 2.txt"), shown in the hint. Confirming posts to `/api/move` or
  `/api/copy`, refreshes the listing and, for non-copies, runs `rekeyFolderPaths()` so
  pins, favorites, tags, notes, hidden marks and recents follow the subtree. A copy
  inherits none of them.
- `POST /api/move?from=/a&to=/b/a` calls `files.Move`; `POST /api/copy?...` calls
  `files.Copy`. Both resolve each end with `resolveTarget` (symlinks are handled as links,
  never followed), refuse the root, require the destination folder to exist (404), refuse
  an existing destination (409) and refuse a folder into itself or its subtree
  (`ErrMoveIntoSelf`/`ErrCopyIntoSelf`, 400, compared on cleaned relative paths with a
  trailing slash so `/docs2` is not treated as `/docs`). Copy is recursive, preserves
  permission bits (directories get owner access while filling, then are chmod'ed back),
  recreates symlinks with `os.Readlink`+`os.Symlink`, refuses special files
  (`ErrUnsupported`), and removes a partial destination if anything fails.
- Trash is a reversible delete: `POST /api/trash?path=/a/b` calls `files.Trash`, which
  moves the entry into `/.trash` (`files.TrashDir`) at the root, **creating that folder on
  first use** (`os.Mkdir`, `ErrExist` tolerated). It returns `{"trashed": src, "path": dst}`
  because the destination name may not be the source name: a collision inside `.trash`
  gets " 2", " 3"… inserted before the extension by `uniqueTrashedName()` ("a.txt" →
  "a 2.txt"; dotfiles/folders → ".env 2", "docs 2"), so trashing never overwrites. The
  source goes through `resolveTarget` (a symlink is trashed as a link), the root is
  rejected, and the trash folder itself or an entry already inside it returns
  `ErrInTrash` (400, same trailing-slash comparison as move-into-self so `/.trash-old` is
  fine). `/.trash` starts with a dot, so listings hide it unless "Show hidden files" is
  on. In the UI the context menu's "Move to trash" (files and folders, right before
  Delete, `trash` icon in `MENU_ICONS`, not danger) calls `trashEntry()`: no confirmation
  (it is reversible), `rekeyFolderPaths()` to the returned path so
  pins/favorites/tags/notes/hidden marks/recents follow like any move, then refresh +
  status. Delete inside `.trash` is the permanent one; see the trash-view bullet below
  for restore/empty.
- `POST /api/trash/empty` calls `files.EmptyTrash()`: it removes every entry inside
  `/.trash` (like `Delete` recursive — symlinks are unlinked, not followed), leaves the
  folder itself in place, treats a missing `/.trash` as "nothing to remove" (0), and
  returns `{"removed": n}`. There is no per-item soft delete — emptying is irreversible,
  which is why the UI confirms via `confirmDialog()` first.
- The trash view (`showTrashView()`, reachable from Settings and from the empty-space
  context menu's "Open trash") shows `/.trash` as a virtual folder: it fetches
  `/api/list?path=/.trash&hidden=1` (forced, so a trashed dotfile stays visible),
  renders `Home › Trash` via `renderTrashBreadcrumbs()`, and shows the danger
  `#empty-trash` toolbar button (hidden elsewhere, disabled while the listing is empty).
  Entries inside the trash (any `isInTrash(path)` hit except `/.trash` itself) get a
  reduced context menu: Open/Preview, **Restore…**, Download (files), Properties, Delete —
  no rename/move/copy/tag/notes/hide. "Restore…" opens the picker in a third mode
  (`openPicker(entry, "restore")`, OK label "Restore here", disabled at `/.trash`) and
  confirms with `/api/move` + `rekeyFolderPaths()`; a destination name that is already
  taken is renamed trash-style by `uniqueRestoreName()` ("a.txt" → "a 2.txt") instead of
  failing, shown in the picker hint. The original location is **not** recorded anywhere,
  so restore means picking a folder, not one click back to where it was.
- Search: `GET /api/search?q=report&path=/docs&hidden=0` calls `files.Search(rootRel,
  query, showHidden)` — a root-confined `filepath.WalkDir` that matches names
  case-insensitively by substring, walks from `path` (default `/`), applies the same dot
  filter as `/api/list` (a dot folder's whole subtree is skipped unless `hidden=1`), never
  follows directory symlinks, caps results at `files.MaxSearchResults` (200, collected in
  walk order before the dirs-first sort) and reports `"truncated": true` at the cap. An
  empty/whitespace `q` is a 400 before it reaches the FS. `files.Search` also rejects an
  empty query with `ErrInvalidName`, and the walk root itself is never a match. The UI:
  the toolbar's `#search-toggle` text button swaps the filter input for `#search-input`
  (`toggleSearch()`; Enter runs, Escape clears or closes); `runSearch(query)` shows the
  matches as a virtual folder with `Home › Search: "q"` breadcrumbs, searching from the
  root with the `showHidden` setting. Re-running (Enter or a mutation's
  `refreshCurrentView()`) does not grow `navHistory`; the tag-view staleness rules apply.
- The three virtual views (tag, trash, search) share machinery: `enterVirtualView()`
  resets the filter, leaves any other virtual view, disables upload
  (`setUploadDisabled()`), hides `#empty-trash`, closes the search bar (`closeSearchBar()`)
  and bumps the shared `viewSeq` (replacing the old per-view `tagViewSeq`), which drops
  stale responses. `refreshCurrentView()` re-runs whichever view is active after a
  mutation; `navigate()` clears all three, re-enables upload and restores the filter
  placeholder; `pinnedEntries()`/`excludePinned()` opt every virtual view out of the
  Pinned section; `globalMenuItems()` omits New folder / New file / Upload in all of them
  and offers "Open trash" everywhere except the trash view itself; `emptyText()` has a
  message per view; `showViewMenu()` titles the menu after the view.
- Navigation is in-page only: `navigate()` never calls `history.pushState`, so the browser
  Back button leaves the app. The in-app `#nav-back` arrow uses its own `navHistory` stack
  (capped at 50): it returns to the previous folder, falls back to the parent when the
  stack is empty, and is disabled at `/`. `goBack()` pops the stack and navigates with
  `{recordHistory: false}` so back does not re-record the folder it just left; ordinary
  navigations record the old path, and a same-path refresh (after a mutation) does not.
  Breadcrumbs render ancestors as `.crumb` links and the current folder as a `.crumb
  .current` span with `aria-current="page"`, separated by decorative `›`; long paths
  auto-scroll to the end and get `.fade-left`/`.fade-right` masks from
  `updateBreadcrumbFade()`.
- Table and grid quick actions share one builder: `renderCell()` (actions column) wraps its
  buttons in `.row-actions`, and `renderGrid()` wraps card buttons in `.card-actions`.
  Both build each button from `menuIcon("download" | "delete")`, so list/grid/context-menu
  icons stay in sync; `.icon-action` sizes them and makes the SVG inherit the button colour
  (`.btn.danger` turns the trash red). Files and folders both get Download (folders stream
  as zip) and Delete. The card
  `keydown` handler ignores events whose `target` is not the card so Enter on a quick
  action does not also open the preview. Dimming (`opacity` until hover) is disabled under
  `@media (hover: none)`.
- Browser-only folder preferences live in three localStorage keys: `pinnedFolders`
  (`[{path, name}]`, in pin order), `folderColors` (`{path: colorKey}`) and `gridSize`
  (`s|m|l`). `pinnedFolders`/`folderColors`/`gridSize` are written with `saveStored()`
  (JSON) and read with `loadStored()`; `gridSize` is a plain string. `folderColor()`
  validates against `FOLDER_COLORS`, so a tampered entry cannot inject CSS. Despite the
  key's name, `folderColors` tags **files too**: a folder shows its colour in the icon, a
  file gets a `.tag-dot` badge in the list and a dot in the card's badge corner.
- Tags are virtual folders. The 🏷 header button (`#tags-toggle`, `showTagsMenu()`) groups
  every tagged entry by palette colour, in `FOLDER_COLORS` order, case-insensitively by
  name (path as the tooltip, count per group). `buildMenu()` renders `{group, color,
  count, action, title}` items: with an `action` the heading becomes a real
  `button.context-group.context-group-action` (role `menuitem`, focusable, and the first
  focus target on open) that calls `showTagView(color)`; without one it stays a
  non-interactive `.context-group` div. Clicking a listed entry calls `openTaggedEntry()`,
  which asks `/api/metadata` first because the menu stores no type, then navigates for a
  folder or previews a file. `updateTagsButton()` keeps the header count ("Tags (n)" /
  "Tags (empty)") in step and runs from `setFolderColor()`, `forgetFolderPaths()` and on
  boot; the rekey/forget helpers keep `folderColors` in step with renames and deletes.
  Only palette-valid colours are listed, so tampered storage entries are ignored, and the
  empty state is two `disabled` hints.
- `showTagView(color)` makes a tag a virtual folder: it sets `tagView`, then
  `enterVirtualView()` pushes the current folder onto `navHistory` (unless
  `recordHistory: false`), and `renderTagBreadcrumbs()` renders `Home › ● Red`
  (on the shared `startVirtualCrumbs()`/`finishCrumbs()` helpers). It fetches
  `/api/metadata` for every tagged path and silently drops paths that 404 from
  `folderColors` (saving and updating the header); any other failure keeps the tag and
  only leaves that entry out, with a status line explaining it. Entries go through the normal
  `renderEntries()`, so sorting, filtering, columns, grid size and the entry context menu
  all work; the virtual-view bullet below covers the pinned/menu/empty-state opt-outs.
  `navigate()` clears `tagView` (and re-enables upload), while `refreshCurrentView()`
  re-runs `showTagView(tagView, {recordHistory: false})` after rename/delete/move/copy so
  mutations stay in the view; `setFolderColor()` does the same, `clearTags()` returns to
  the last folder, and `emptyText()` says "Nothing is tagged red yet."
- Favorites are a fourth key, `favorites` (`[{path, name, isDir}]`, in add order). The ★ in
  the header (`#favorites-toggle`, filled and gold via `.has-items`) opens the same menu
  component anchored under the button (`showFavoritesMenu()`); its items carry a
  `remove` callback that renders a trailing `.context-remove` button, and the empty state
  is two `disabled` items. Toggling happens in the entry context menu for both files and
  folders ("Add to favorites" / "Remove from favorites"). Favorites are marked with a
  `.favorite-badge` in the list and a `.card-badges` corner on grid cards, and
  `rekeyFolderPaths()`/`forgetFolderPaths()` keep them in step with rename/delete.
  `buildMenu()` supports `{disabled, title, remove}` in addition to the item fields above.
- Recent items use the `recent` key (`[{path, name, isDir}]`, newest first, max
  `RECENT_LIMIT` = 20). `recordRecent()` runs after a successful `navigate()` (skipped for
  the boot call via `{recordRecent: false}`) and at the start of `openFile()`, dedupes by
  path and unshifts, so revisiting moves an entry to the top; the root is stored as
  "Home". The 🕘 header button (`#recent-toggle`) opens `recentMenuItems()` via
  `showRecentMenu()`, with per-row remove buttons and a trailing "Clear recent" danger
  item. Recent entries are not badged in the listing. Rename/delete rekey and prune them
  like favorites.
- Settings: the ⚙ header button (`#settings-toggle`, `showSettingsMenu()`) opens
  `settingsMenuItems()` in the shared menu: a `checked` "Show hidden files" toggle, a
  `checked` "Show image thumbnails" toggle (grid view only), the
  `themeMenuItems()` pair ("Light theme" / "Dark theme", the only theme switch), then
  `viewMenuItems()` (List/Grid), an "Open trash" entry, a "Server info" entry (so ℹ️ can
  be hidden on tiny screens), and Clear tags / Clear favorites / Clear recent / Clear
  hidden / Clear notes with counts (disabled at 0). The theme persists in the `theme`
  localStorage key and the
  inline script in `index.html` applies it before first paint; `applyTheme()` only sets
  the attribute and storage now (there is no header button). Dot entries are hidden by default:
  `showHidden` lives in localStorage, `navigate()` adds `&hidden=1` to `/api/list` when it
  is on, and `files.List(rel, showHidden)` skips dot names server-side (so scripts see the
  same default). Toggling re-fetches the current folder with
  `{recordHistory: false, recordRecent: false}` so it neither grows history nor bumps
  recent. Every clear action goes through `confirmDialog()` (the recent menu's "Clear
  recent" included, via `clearRecentItems()`), then resets the array/object, saves it,
  updates the matching header button and re-renders the list where badges or colours
  changed. `clearLabels` show counts only when non-zero.
- Hand-hidden entries are a fifth browser-local key, `hiddenPaths` (array of paths). The
  context menu's Hide/Unhide item (files and folders, before Properties) calls
  `toggleHiddenEntry()`; `shownEntries()` drops hidden paths unless `showHidden` is on, and
  `renderList()` counts against that filtered total. `pinnedEntries()` applies the same
  rule, so a hidden folder's pinned shortcut disappears too (the pin itself is kept). When
  revealed, rows/cards get `.is-hidden` (dimmed) and a `.hidden-badge` (eye-off icon).
  Hiding is client-side and per browser; dot entries are the server-side `hidden=1` filter
  above — the one settings toggle reveals both. `rekeyFolderPaths()` rekeys hidden paths
  and `forgetFolderPaths()` prunes them; `clearHidden()` (Settings) resets every mark after
  a confirmation. Hiding never touches favorites, tags or recents, which stay reachable.
- Notes are the sixth browser-local key, `notes` (`[{path, name, isDir, text, updated}]`,
  one per entry). The context menu's Add note / Edit note (files and folders, between Hide
  and Properties) calls `openNote()`, which fills `#note-modal` (`#note-text` textarea,
  `#note-remove` shown only for an existing note). Saving trims the text: empty removes the
  note, otherwise it replaces the entry with a fresh `updated` (Date.now()). Header button
  `#notes-toggle` (📝-style SVG, accent when non-empty) opens `notesMenuItems()` via
  `showNotesMenu()`: newest first, each row is `{label, detail, title, remove}` where
  `detail` is the note's first line (rendered by `buildMenu()` as `.menu-detail` inside
  `.menu-text`) and `title` is the whole text; clicking opens the note, the trailing ✕
  removes it. Noted entries get a `.note-badge` in list and grid. `rekeyFolderPaths()`
  moves notes with renames, `forgetFolderPaths()` prunes them on delete, and
  `clearNotes()` (Settings → Clear notes, confirmed) wipes all of them. The notes menu's
  empty state is two disabled hints.
- Properties: the context menu's "Properties" item (files and folders, right before
  Move to trash and Delete) calls `showProperties(entry)`. It opens `#properties-modal` immediately with the
  listing's data, then re-fetches `GET /api/metadata` to refresh it, so a folder listed
  long ago still shows the current size/mtime; a request counter (`propertiesRequest`)
  plus a `hidden` check keep a slow response from overwriting a newer dialog. The refresh
  is best-effort and silent on failure — everything shown is already valid — because the
  dialog must not turn into an error. Use `/api/metadata`, **not** `/api/stat`, from the
  browser: EasyPrivacy-style filter lists contain the generic rule `/api/stat?` and
  ad blockers reject that request (`TypeError: Failed to fetch`). `/api/stat` stays as a
  server-side alias for scripts. Rows are `dt`/`dd` pairs in `#properties-list` (same
  `.info-list` styling as Server info); `FILE_TYPES` maps common extensions to a friendly
  label, falling back to "File (.ext)", folders show "—" for size, and entries from
  pins/favorites/recents (which only carry name/path/isDir) show "—" until the refresh
  fills them in. It closes on the Close button, Escape or a backdrop click. The
  subtitle under a modal title is `.modal-subtitle` (shared with the Server info version
  line).
- Pinned folders render as a "Pinned" section above the current folder's entries (table:
  `sectionRow()` + `.pinned-row`; grid: `.grid-section` + `.pinned-card`), and
  `excludePinned()` keeps them out of the normal listing so nothing appears twice. A
  shortcut from another folder still resolves an entry via `pinnedEntries()` (fallback in
  the `contextmenu` handler). `rekeyFolderPaths()` runs after a successful rename and
  `forgetFolderPaths()` after a delete so shortcuts and colours follow/expire with the
  subtree. Folders use an inline SVG (`renderEntryIcon()` + `FOLDER_SVG`) rather than the
  📁 emoji so they can be coloured; files keep their emoji. The wrapper gets
  `data-color="<key>"` and `.folder-icon[data-color="…"] .folder-svg` in the stylesheet
  supplies the fill — the fill must not be set on the wrapper, because `.folder-svg`
  declares its own `fill` on the `<svg>` and that rule wins over an inherited value.
- Grid size is applied by `applyGridSize()` as a `size-s|m|l` class on `#grid` plus
  `aria-pressed` on the `.size-btn` buttons; the control is hidden in list view by
  `body[data-view="list"] .size-toggle`.
- The context menu is built from item objects (`{label, icon, danger, checked, action}`,
  `{separator: true}` or `{swatches: true, current, onPick}`) by `buildMenu()`; icons are
  inline SVG path strings in `MENU_ICONS`, so they inherit `currentColor` (danger items go
  red) with no image files. The folder colour row comes from `colorRow()`; arrow-key
  navigation walks `.context-item, .color-dot`.
  `showContextMenu`/`showViewMenu` add a `.context-title` (entry name, or the current
  path). The menu clamps to the viewport on all four edges, scrolls when taller than the
  screen, focuses its first item on open, supports ↑/↓/Home/End/Tab, and Escape closes it
  and returns focus to wherever it was. It closes on scroll/resize because it is anchored
  to the pointer.
- The top bar's 📤 button (`#upload-toggle`) opens the file picker and uploads into the
  folder currently shown (`currentPath`), sharing the same modal/progress flow as the
  context-menu entry. It is the primary upload path on touch devices. All header controls
  are `flex: none`; below 600px the wordmark is hidden and icon buttons shrink to 36px
  targets, below 420px the brand (logo) is dropped, and below 520px `.topbar` wraps so the
  nav (back arrow + breadcrumbs) gets its own row while the icon controls share the first.
  Nine controls remain (tags, notes, recent, favorites, upload, info, view, settings,
  logout) after the theme switch moved into Settings, so they still fit one row down to
  401px (9×36 + 8×4.8 gap + 19.2 padding ≈ 382px). Below 400px the ℹ️ button is hidden
  (Server info lives in the ⚙ settings menu, always)
  and the remaining eight shave to 34px, so everything still fits across 320px
  (8×34 + 7×3.2 gap + 19.2 padding ≈ 314px). Re-check 320–400px layouts when adding
  another header control.
- The list table's `<thead>` is built by `renderTableHead()` from `activeColumns()` in
  `app.js`; the Columns menu persists visibility + order in the `columns` localStorage key
  (`[[key, visible], …]`). Modified starts hidden below 640px (`defaultColumnPrefs`), at
  least one column must stay visible, and the actions column is pinned last. The Columns
  button is hidden in grid view.
- Row separators are `border-bottom` on `tbody tr`, **not** on `td`: a border on a wrapped
  cell breaks the row line (the original bug). For the same reason `td.name` must stay a
  table cell — the flex layout lives on the inner `.name-inner` span.
- `files.Disk` uses `syscall.Statfs`, so the code is Unix-only (fine for the Linux target,
  but a Windows build would fail). `FreeBytes` is space available to the process
  (`Bavail`), so `used + free < total` by the reserved blocks — the UI percentage matches
  `df` by dividing by `used + free`.
