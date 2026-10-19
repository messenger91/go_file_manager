# Go File Manager

A small file manager for Ubuntu Server 24 (LTS), written in Go with no
external dependencies. It exposes a JSON API and a web UI for browsing,
previewing, editing, uploading, and deleting files under a single root folder.

## Features

- Browse directories and view file metadata
- Create folders and files, and upload files, from the right-click context menu
- Upload from the 📤 button in the top bar, which targets the folder you are viewing
  (the easy path on phones and tablets, where right-click is awkward)
- Upload with a live progress bar and a Cancel button (50 MB limit, checked in the browser
  and enforced by the server)
- Edit and save text files in the browser (the viewer's Edit button)
- Rename files and folders (context menu, or the Rename button in the file viewer)
- List and grid views, switchable from the top bar or the right-click context menu (remembered per browser)
- Configurable list columns: show/hide and drag (or use ▲/▼) to reorder, remembered per browser
- Filter the current folder and sort by name, size, or modified date
- Responsive, mobile-first list (compact rows; the Modified column starts hidden on narrow screens)
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

`PORT` is only a port number: the app always listens on **all interfaces**
(`:PORT`). Restrict access with a firewall or put it behind a reverse proxy — see
[Deploy on Ubuntu Server 24](#deploy-on-ubuntu-server-24-lts).

## Deploy on Ubuntu Server 24 (LTS)

The binary is statically linked, so the server needs nothing but the binary, a config file,
and a writable folder to expose.

### 1. Build a static Linux binary

On your workstation (or on the server, if it has Go 1.25+):

```sh
make build-linux   # -> bin/go_file_manager-linux-amd64 and -linux-arm64
```

Pick the one matching the server (`uname -m`: `x86_64` → `amd64`, `aarch64` → `arm64`) and
copy it over:

```sh
scp bin/go_file_manager-linux-amd64 you@server:/tmp/go_file_manager
```

### 2. Install it

On the server:

```sh
# A dedicated, unprivileged account to run the service.
sudo useradd --system --no-create-home --shell /usr/sbin/nologin gofm

# The folder the app will expose. Use a dedicated data directory rather than a
# whole home directory.
sudo mkdir -p /srv/files
sudo chown -R gofm:gofm /srv/files

sudo install -o root -g root -m 0755 /tmp/go_file_manager /usr/local/bin/go_file_manager
```

### 3. Configure

Create `/etc/go_file_manager.env`:

```sh
sudo tee /etc/go_file_manager.env >/dev/null <<'EOF'
HOME_DIR=/srv/files
ADMIN=admin
PASSWORD=change-me
PORT=8080
EOF
sudo chown root:root /etc/go_file_manager.env
sudo chmod 600 /etc/go_file_manager.env
```

The service loads this file directly, so you do **not** need a `.env` in the working
directory (that is only used by `make run`).

### 4. Run it as a systemd service

Create `/etc/systemd/system/go_file_manager.service`:

```ini
[Unit]
Description=Go File Manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gofm
Group=gofm
EnvironmentFile=/etc/go_file_manager.env
ExecStart=/usr/local/bin/go_file_manager
Restart=on-failure
RestartSec=2

# Hardening: the filesystem is read-only except for HOME_DIR below.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
# Must list the same path as HOME_DIR, or the service cannot write to it.
ReadWritePaths=/srv/files

[Install]
WantedBy=multi-user.target
```

Then start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now go_file_manager
systemctl status go_file_manager
journalctl -u go_file_manager -f      # follow the logs
```

Open `http://<server-ip>:8080/` and sign in.

> **If `HOME_DIR` is a real home directory** (e.g. `/home/youruser`), keep
> `ReadWritePaths` in sync with it — and leave `ProtectHome` unset, since it would hide
> `/home` from the service entirely.

### 5. Firewall

If you reach the app directly:

```sh
sudo ufw allow 8080/tcp
```

### 6. Put it behind TLS (recommended)

The session cookie is not marked `Secure` because the app speaks plain HTTP. On a network
you do not fully trust, terminate TLS in a reverse proxy and keep the app on the same
host. Example nginx site:

```nginx
server {
    listen 443 ssl;
    server_name files.example.com;

    ssl_certificate     /etc/letsencrypt/live/files.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/files.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

The app binds to all interfaces, so with a proxy do **not** open `8080` to the outside —
`ufw allow 443/tcp` and let the proxy reach the app over loopback (loopback traffic is not
filtered by ufw).

### 7. Updating

```sh
# Rebuild, copy to the server, then:
sudo systemctl stop go_file_manager
sudo install -o root -g root -m 0755 /tmp/go_file_manager /usr/local/bin/go_file_manager
sudo systemctl start go_file_manager
```

Sessions are held in memory, so restarting the service signs everyone out.

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
each name that already exists is reported in the `failed` array. The whole upload is
limited to 50 MiB; anything larger returns `413`.

## Development

```sh
make test   # go test ./...
make vet    # go vet ./...
make fmt    # go fmt ./...
```
