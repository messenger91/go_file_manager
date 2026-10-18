# Go File Manager

A small read-only file manager for Ubuntu Server 24 (LTS), written in Go with no
external dependencies. It exposes a JSON API and a web UI for browsing and
previewing files under a single root folder.

## Features

- Browse directories and view file metadata
- Preview text files in the browser, download any file
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

## Development

```sh
make test   # go test ./...
make vet    # go vet ./...
make fmt    # go fmt ./...
```
