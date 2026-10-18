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

Tests live only in `internal/files` (covering root confinement, symlink escapes,
listing order, and delete safety); `internal/config` and `internal/server` have none.

Always run `make fmt vet test` before finishing; all three currently pass.

## Layout

- `main.go` — entrypoint; embeds `web/` via `//go:embed all:web` and serves it.
- `internal/config` — env/`.env` loading. Precedence: **process env > `.env` > defaults**.
  `.env` is read from the current working directory.
- `internal/files` — root-confined filesystem access (`List`, `Stat`, `ReadFile`, `Open`,
  `Delete`). This is the security boundary; test changes here.
- `internal/server` — HTTP routes, Basic auth, JSON/error handling.
- `web/` — static UI (`index.html`, `static/style.css`, `static/app.js`). **Assets are
  embedded at build time**, so edits require a rebuild/restart, not just a refresh.

## Gotchas

- `HOME_DIR` is required; the server exits if it is unset. `ADMIN`/`PASSWORD` default to
  `admin`/`admin`; `PORT` defaults to `8080`.
- Basic auth wraps **every** route, including the static UI — the browser prompts once.
- API path args are root-relative slash paths in `?path=/sub/file`. Traversal (`..`) is
  **clamped into the root** (yields 404 for nonexistent paths), not rejected; symlink
  escapes that resolve outside the root return 403. Keep both behaviors.
- `/api/read` refuses files over `files.MaxReadSize` (2 MiB) so callers download instead.
- Delete uses `resolveNoFollow` (not `resolve`): it resolves the parent but never the
  final element, so deleting a symlink unlinks the link and leaves its target intact,
  even when the target is outside the root. Deleting the root returns
  `ErrRootDelete` (400). A non-empty directory without `recursive=true` returns
  `ErrDirNotEmpty` (409).
- Still no mkdir/rename/upload/move endpoints. Add them only against confirmed scope.
