# Go File Manager

A small file manager for Ubuntu Server 24 (LTS), written in Go with no
external dependencies. It exposes a JSON API and a web UI for browsing,
previewing, and deleting files under a single root folder.

## Features

- Browse directories and view file metadata
- Create new folders from the right-click context menu
- List and grid views, switchable from the top bar or the right-click context menu (remembered per browser)
- Filter the current folder and sort by name, size, or modified date
- Preview text files in the browser, download any file
- Delete files and folders (row button or right-click context menu, with an in-app confirmation dialog)
- HTTP Basic auth (default `admin` / `admin`)
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
| `ADMIN`    | no       | `admin` | Basic-auth username            |
| `PASSWORD` | no       | `admin` | Basic-auth password            |
| `PORT`     | no       | `8080`  | HTTP listen port               |

## HTTP API

All endpoints require Basic auth.

| Method | Path            | Description                          |
| ------ | --------------- | ------------------------------------ |
| GET    | `/api/list`     | List a directory (`?path=/sub`)      |
| GET    | `/api/stat`     | Metadata for one path                |
| GET    | `/api/read`     | Text preview of a file (max 2 MiB)   |
| GET    | `/api/download` | Raw file download                    |
| DELETE | `/api/delete`   | Delete a file or folder              |
| POST   | `/api/mkdir`    | Create a folder                      |

`DELETE /api/delete?path=/sub` removes an empty folder (or a file). Deleting a
non-empty folder returns `409 Conflict` unless `&recursive=true` is passed. The
root folder itself can never be deleted.

`POST /api/mkdir?path=/sub/new` creates a folder. The parent must exist; a
missing parent returns `404` and an existing path returns `409`. The root itself
cannot be created.

## Development

```sh
make test   # go test ./...
make vet    # go vet ./...
make fmt    # go fmt ./...
```
