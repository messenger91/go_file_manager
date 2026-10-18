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
safety) and `internal/server` (login/session flow, auth redirects, logout);
`internal/config` has none.

Always run `make fmt vet test` before finishing; all three currently pass.

## Layout

- `main.go` — entrypoint; embeds `web/` via `//go:embed all:web` and serves it.
- `internal/config` — env/`.env` loading. Precedence: **process env > `.env` > defaults**.
  `.env` is read from the current working directory.
- `internal/files` — root-confined filesystem access (`List`, `Stat`, `ReadFile`, `Open`,
  `Delete`, `Mkdir`, `Rename`, `Create`, `Write`, `Upload`, `Disk`). This is the security
  boundary; test changes here.
- `internal/server` — HTTP routes, authentication (login form + sessions), JSON/error
  handling.
- `internal/host` — host info for `GET /api/info` (hostname, OS name, arch, uptime, load
  average, memory). Read from `/proc` and `/etc/os-release`; unavailable values are left
  zero, so it compiles and runs on any OS.
- `web/` — static UI (`index.html`, `login.html`, `static/style.css`, `static/app.js`).
  **Assets are embedded at build time**, so edits require a rebuild/restart, not just a
  refresh.

## Gotchas

- `HOME_DIR` is required; the server exits if it is unset. `ADMIN`/`PASSWORD` default to
  `admin`/`admin`; `PORT` defaults to `8080`.
- Auth is a **login form with an in-memory session cookie** (`gfm_session`, 24h TTL), not
  a browser Basic-auth prompt: the server never sends `WWW-Authenticate`, so the native
  dialog can't appear. Unauthenticated `/api/*` calls get `401` JSON; page requests
  redirect to `/login?next=…`. Only `/login`, `POST /logout`, and `/static/` are public.
  HTTP Basic is still accepted on every route for scripts (`curl -u admin:admin`).
  Sessions are not persisted, so a restart logs everyone out. `internal/server` holds the
  auth tests; run them with `make test`.
- API path args are root-relative slash paths in `?path=/sub/file`. Traversal (`..`) is
  **clamped into the root** (yields 404 for nonexistent paths), not rejected; symlink
  escapes that resolve outside the root return 403. Keep both behaviors.
- `/api/read` refuses files over `files.MaxReadSize` (2 MiB) so callers download instead.
- Every mutation (delete, mkdir, rename, create, write, upload) uses `resolveTarget`
  (not `resolve`): it resolves the parent but never the final element, so operating on a
  symlink affects the link and leaves its target intact, even when the target is outside
  the root. `Write` additionally refuses a symlink target outright (`ErrForbidden`, 403).
  Operating on the root returns `ErrRootOperation` (400), as does an invalid new name
  (`ErrInvalidName`). A non-empty directory without `recursive=true` returns
  `ErrDirNotEmpty` (409); `Mkdir`/`Rename`/`Create`/`Upload` onto an existing path returns
  `os.ErrExist` (409) and a missing source/parent returns `os.ErrNotExist` (404).
- Size limits: `files.MaxReadSize` (2 MiB, `/api/read`), `files.MaxWriteSize` (8 MiB,
  `/api/write` body), `files.MaxUploadSize` (64 MiB, `/api/upload`). Overwriting is never
  implicit: write requires an existing file, create/upload refuse an existing path.
- No move endpoint yet (rename is same-folder only). Add it only against confirmed scope.
- `files.Disk` uses `syscall.Statfs`, so the code is Unix-only (fine for the Linux target,
  but a Windows build would fail). `FreeBytes` is space available to the process
  (`Bavail`), so `used + free < total` by the reserved blocks — the UI percentage matches
  `df` by dividing by `used + free`.
