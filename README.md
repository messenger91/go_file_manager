# Go File Manager

A small file manager for Ubuntu Server 24 (LTS), written in Go with no
external dependencies. It exposes a JSON API and a web UI for browsing,
previewing, and deleting files under a single root folder.

## Features

- Browse directories and view file metadata
- Create folders and files, and upload files, from the right-click context menu
- Edit and save text files in the browser (the viewer's Edit button)
- Rename files and folders (context menu, or the Rename button in the file viewer)
- List and grid views, switchable from the top bar or the right-click context menu (remembered per browser)
- Filter the current folder and sort by name, size, or modified date
- Preview text files in the browser, download any file
- Delete files and folders (row button or right-click context menu, with an in-app confirmation dialog)
- Login page with an in-memory session cookie (default `admin` / `admin`); HTTP Basic auth
  still works for scripts and API clients
- Server info popup from the header (disk/RAM usage meters + hostname, OS, uptime, load
  average, CPU, Go version)
- Light and dark theme (remembered per browser)
- Access confined to `HOME_DIR`; traversal is clamped into the root and symlink escapes are rejected

## Quick start

```sh
cp .env.example .env   # then set HOME_DIR
make run               # http://localhost:8080
```

Or build a binary:

```sh
make build             # host platform -> bin/go_file_manager
make build-linux       # static Linux amd64 + arm64 binaries
```

## Configuration

Set via environment variables or a `.env` file (env vars win):

| Variable   | Required | Default | Purpose                        |
| ---------- | -------- | ------- | ------------------------------ |
| `HOME_DIR` | yes      | —       | Root folder exposed by the app |
| `ADMIN`    | no       | `admin` | Login username                 |
| `PASSWORD` | no       | `admin` | Login password                 |
| `PORT`     | no       | `8080`  | HTTP listen port               |

## HTTP API

Every endpoint requires authentication. The web UI signs in through the `/login` form and
carries a session cookie; scripts can authenticate with HTTP Basic (`curl -u admin:admin`).
Unauthenticated `GET /api/*` requests return `401` with a JSON body, and page requests are
redirected to `/login`. `POST /logout` ends the session.

| Method | Path            | Description                          |
| ------ | --------------- | ------------------------------------ |
| GET    | `/api/list`     | List a directory (`?path=/sub`)      |
| GET    | `/api/stat`     | Metadata for one path                |
| GET    | `/api/read`     | Text preview of a file (max 2 MiB)   |
| GET    | `/api/download` | Raw file download                    |
| GET    | `/api/info`     | Root path, disk usage, host info      |
| DELETE | `/api/delete`   | Delete a file or folder              |
| POST   | `/api/mkdir`    | Create a folder                      |
| POST   | `/api/rename`   | Rename a file or folder in place     |
| POST   | `/api/create`   | Create an empty file                 |
| PUT    | `/api/write`    | Overwrite a file (body is new content)|
| POST   | `/api/upload`   | Upload one or more files (multipart) |

`DELETE /api/delete?path=/sub` removes an empty folder (or a file). Deleting a
non-empty folder returns `409 Conflict` unless `&recursive=true` is passed. The
root folder itself can never be deleted.

`POST /api/mkdir?path=/sub/new` creates a folder. The parent must exist; a
missing parent returns `404` and an existing path returns `409`. The root itself
cannot be created.

`POST /api/rename?path=/sub/old.txt&name=new.txt` renames in place (same folder).
`name` must be a plain name with no `/`. Renaming onto an existing path returns
`409` rather than overwriting; a missing source returns `404`.

`POST /api/create?path=/sub/note.txt` creates an empty file (`409` if it exists).
`PUT /api/write?path=/sub/note.txt` replaces a file's contents with the raw
request body (limit 8 MiB); the file must already exist. `POST /api/upload?path=/dir`
accepts multipart form data with one or more `file` parts and never overwrites —
each name that already exists is reported in the `failed` array.

## Development

```sh
make test   # go test ./...
make vet    # go vet ./...
make fmt    # go fmt ./...
```
