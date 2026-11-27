<p align="center">
  <img src="img/logo.png" alt="Sudoku Golang CLI Logo" width="250"/>
</p>

📘 Documentation available in other languages:

* 🇬🇧 [Русский](README.md)

---

## Description 📝

This project is a CLI tool written in Go for managing infrastructure and services related to Sudoku via `docker-compose`. It allows you to conveniently build, start, stop, and rebuild containers through a set of commands.

## Table of Contents 📑

* [Description](#description)
* [Requirements](#requirements)
* [Installation and Build](#installation-and-build)
* [Available Commands](#available-commands)
* [Web admin](#web-admin)
* [Configuration](#configuration)
* [Infrastructure](#infrastructure)
    * [Services](#services)
        * [Traefik](#traefik)
        * [Redis (KeyDB)](#redis-keydb)
        * [Elasticsearch](#elasticsearch)
        * [RabbitMQ](#rabbitmq)
    * [Network](#network)
* [Project Structure](#project-structure)

---

## Installation and Build ⚙️

sudoku runs on **Linux, macOS and Windows** (amd64 and arm64): a single dependency-free binary with the admin
panel embedded. All it needs is Docker (Docker Desktop, Colima, OrbStack, Rancher Desktop or dockerd) with compose
(`docker compose` or `docker-compose`).

**Release binary** (recommended): download the archive for your platform from the releases page
(`darwin_arm64` — Apple Silicon Mac, `darwin_amd64` — Intel Mac, `linux_*`, `windows_*`), unpack it and run

```sh
xattr -d com.apple.quarantine ./sudoku   # macOS only: the binary is not signed
./sudoku setup                           # Windows: .\sudoku.exe setup
```

`setup` asks for your projects folder and the container prefix, shows what goes where, and installs:

| What | Where |
|---|---|
| the `sudoku` command | `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\sudoku`), offers to add it to PATH |
| `.env`, the infrastructure `compose.yaml`, `sudoku-config/`, backups | `~/.sudoku` (or `$SUDOKU_HOME`) |
| your projects | stay where they are: `<projects folder>/<project>/compose-sudoku.yaml` |

Afterwards the downloaded archive can be deleted and `sudoku` works from any directory: `sudoku doctor`,
`sudoku start`, `sudoku serve`. The projects folder can be changed at any time (`sudoku setup` or the admin settings);
the infrastructure does not depend on it. Re-running `setup` is safe: existing files are kept (unless
`--overwrite-env` / `--regenerate-compose`; old ones go to `.bak`). Unattended: `sudoku setup --root ~/projects
--prefix dev --yes`. Updates: `sudoku update` (verifies the SHA-256 from the release's `checksums.txt`), no new
`setup` needed.

**Moving from an old install** (binary, `.env`, `compose.yaml` and `sudoku-config/` inside the projects folder):
`setup` finds it by itself — in the current directory, next to the binary and through Docker's containers — and
offers to move it into `~/.sudoku` together with the data (`sudoku-config/*/data`, backups). Infrastructure containers
are recreated from the new place; `sudoku_network` and project containers are left alone. Explicitly:
`sudoku setup --migrate-from <dir>`. The old layout keeps working without moving.

**From source** (needs Go and Node.js/npm):

```sh
git clone … && cd sudoku-golang
cp .env.example .env        # optional: template for ~/.sudoku/.env (ROOT_PROJECTS_FOLDER, PREFIX_CONTAINER_NAME)
make build                  # UI + binary + `sudoku setup` (OVERWRITE_ENV=1 / REGENERATE_COMPOSE=1 to overwrite)
```

Other targets: `make bin` (binary only, into `build/`), `make cross` (all 6 platforms into `build/dist`), `make test`,
`make lint`, `make release-snapshot` (goreleaser locally). Releases are built by `.github/workflows/release.yml` on
`v*` tags.

## Available Commands 💻

```sh
sudoku start [targets...]   # start (no targets = everything; infrastructure first, then wait for its healthchecks)
sudoku stop | down | restart | build | rebuild | force-build | force-rebuild [targets...]
sudoku status [targets...]  # status table (--json, -a to include foreign containers)
sudoku logs <targets...> -f # logs (compose logs), --tail, --since, -t
sudoku exec <service> [-- command...]  # shell or command in a service (-u user, -T no TTY)
sudoku sets list | add <name> <targets...> | rm <name>   # project sets
sudoku doctor               # diagnostics: docker, compose, socket, projects, ports, permissions (--json)
sudoku serve                # web admin
sudoku setup                # install / change the projects folder / move an old install
sudoku update [--check]     # update to the latest release
sudoku version
sudoku completion bash|zsh|fish|powershell   # completion (projects, services and sets)
```

**Targets**: a project name (a folder with `compose-sudoku.yaml`), `infra` (the root `compose.yaml`), a service name
(found in any project), `project/service` (or `project:service`), patterns (`api-*`, `*/db`). Sets: `--set backend`.

```sh
sudoku start infra api billing/worker
sudoku restart --set backend --no-wait
sudoku logs infra/traefik -f
sudoku exec billing/db -- psql -U postgres
```

Global flags: `--debug`, `--log`, `-C <dir>` (run from another directory; its `.env` is used if it has `ROOT_PROJECTS_FOLDER`). `start`/`restart`/`rebuild` accept
`--wait` / `--no-wait` (wait for the infrastructure healthchecks) and `--no-preflight` (skip the port conflict
check). The exit code is 1 if any compose file failed.

## Web admin 🖥️

`./sudoku serve` starts the admin at `http://127.0.0.1:7777`. The address comes from `ADMIN_ADDR` in `.env` and the
`--addr` flag overrides it; only loopback addresses are allowed. Enable logs with `--log`.
The UI is embedded into the binary.

`make build` does not overwrite an existing `~/.sudoku/compose.yaml` or files in `~/.sudoku/sudoku-config/` (they hold
your edits and ports added from the admin). Regenerate it with `REGENERATE_COMPOSE=1 make build` (the old file is kept as `compose.yaml.bak`).

Features:

* lists every container of the projects under `ROOT_PROJECTS_FOLDER`, grouped by compose file: status, docker name,
  IP and DNS names per network, published ports, Traefik hosts (`Host(...)` from labels);
* stops, starts and restarts a single container;
* starts, stops and restarts all projects (same as `sudoku start/stop/restart`) with a per-compose-file run log;
* per-container panel: live **logs** (`docker logs -f`, filter, wrap, download), an in-browser **terminal**
  (`docker exec -it`, bash or sh) and **build** info: the full `docker build` command derived from
  `docker compose config` (with build args), ready-made compose commands and a "rebuild service" button;
* **Monitoring**: live resource usage via the Docker Engine API (every 2 s by default, up to 30 minutes of history): CPU, memory, network
  and block I/O in total and per container, charts with crosshair tooltips, a sortable table with sparklines, and
  Docker disk usage (images, volumes, container layers, build cache). Every container also has a "Resources" tab.
  By default metrics are collected **only while the monitoring page (or a Resources tab) is open**. Settings has a
  background-collection toggle and the polling interval for both modes: live 1–60 s (default 2 s), background
  5 s – 1 h (default 10 s). Stored in `~/.sudoku/sudoku-config/admin.yaml`;
* **Cleanup**: frees disk space by removing unused images, build cache (all of it, or entries older than 1/7/30 days)
  and stopped containers. It shows a preview first and removes only what you tick. **Volumes are never removed**, not
  even with containers: `docker rm` runs without `-v`. Stopped containers with Docker volumes and anything that looks
  like a database (by image, name or data path) are protected and can't be selected. Images are deleted without
  force, so an image used by any container is refused. The server re-reads Docker state and re-checks every item
  before deleting;
* **"Don't start"** flag per service: skipped by "start all" / "restart all" and by the CLI `sudoku start`, `restart`,
  `rebuild`, `build`. Stored in `~/.sudoku/sudoku-config/disabled.yaml` (hand-editable). If an enabled
  service `depends_on` a disabled one, compose still starts it, and the admin warns about that;
* **Ports**: manages Traefik ports in the root `compose.yaml` (adds/removes `ports:` entries with comments and,
  optionally, entryPoints in `sudoku-config/traefik/traefik.yaml`), shows which containers use each entryPoint and
  applies changes by recreating Traefik. A `*.sudoku.bak` copy is written before every change;
* **Multi-select**: tick services (or a whole group/table) and start, stop or restart them in one run
  (`compose up -d` / `stop` for just those services of each file), or flag them "don't start" / "start again" at once;
* **Config editor** (Settings): edits the sudoku `.env` (`~/.sudoku/.env`) (`ROOT_PROJECTS_FOLDER`, `DOCKER_PATH`,
  `DOCKER_COMPOSE_PATH`, `MAX_WORKERS`, `ADMIN_ADDR`). Values are validated, other lines and comments are kept, a
  `.env.sudoku.bak` copy is written, and `serve` re-executes itself to apply them. Changing the address warns that the
  current page will stop working and then opens the new address. An enabled autostart unit is refreshed.
  `make build` does not overwrite an existing `~/.sudoku/.env` (`OVERWRITE_ENV=1` forces it);
* **Settings**: autostart of `sudoku serve` at login (macOS LaunchAgent, Linux `systemd --user`) and re-enabling
  confirmations hidden with "don't show again";
* optionally shows foreign compose containers (outside `ROOT_PROJECTS_FOLDER`), read-only.
* **Live updates**: the admin follows Docker events and pushes changes to the browser over SSE (`/api/events`) —
  statuses change within ~250 ms without polling (polling remains a 30 s safety net);
* **Alerts**: an OS notification (macOS, Linux `notify-send`, Windows toast) and a browser notification when a
  container crashes (not stopped on purpose), fails its healthcheck or is OOM-killed; listed on "Monitoring";
* **Startup order**: "Start all" and `sudoku start` bring up the infrastructure, wait for its healthchecks
  (configurable, up to 3 min by default) and only then start the projects;
* **Sets**: named groups of targets (`sudoku-config/sets.yaml`), started/stopped with one click, created from the
  services selected in the table;
* **Command palette**: `⌘K` / `Ctrl+K` — navigation, logs/terminal/config/restart of any service;
* **Logs**: level filter (WARN+/ERROR), search with highlighting and `Enter`/`Shift+Enter` navigation, a merged
  stream of all services of a project, full-log download streamed from the server;
* **Config**: the effective `compose config` of a service, the uninterpolated version and its `${VAR}` references
  with unset ones highlighted; secrets are masked until revealed;
* **Runs**: history of all runs (last 100, `sudoku-config/runs/`) with each step's output, survives restarts;
* **Image updates**: "Check updates" compares the local image digest with the registry (no pull, auth from
  `~/.docker/config.json`); an "UPDATE" badge marks the service;
* **Backups**: one-click dumps of PostgreSQL, MySQL/MariaDB, MongoDB and Redis/KeyDB/Valkey into `${ROOT}/backups/`,
  download and restore (typed confirmation), keeping the last N per database;
* **Network → Domains & TLS**: Traefik domains and how they resolve, a managed block in the hosts file (without
  permission it stages the file and prints `sudo cp …` / the PowerShell command), local HTTPS via mkcert plus the
  Traefik file-provider config;
* **Port conflicts**: before starting, checks whether a published port is taken by another container or a host
  process and offers to stop the conflicting container;
* **Schedules**: periodic disk cleanup (build cache, dangling images, stopped containers) and update checks;
* **Diagnostics**: the same checks as `sudoku doctor` on the settings page.

Security: the server listens only on loopback (`localhost`, `127.0.0.1`, `::1`) or a unix socket
(`ADMIN_ADDR=unix:/path/sudoku.sock`, mode 0600), rejects cross-site requests (`Sec-Fetch-Site`, `Origin`, JSON-only
POSTs) and cannot be framed (CSP `frame-ancestors 'none'`). On shared machines set `ADMIN_TOKEN`: the panel then opens
only via the `http://127.0.0.1:7777/?token=…` link printed by `sudoku serve` (an HttpOnly cookie afterwards), and API
clients send `Authorization: Bearer <token>`. Never expose the admin publicly: it controls Docker on your machine.

UI development:

```sh
make bin         # build only the binary (UI + go build) into build/sudoku, no deploy
make serve       # build the UI and run the API on :7777
make web-dev     # Vite dev server on :5173 proxying /api to :7777
```

The UI design system lives in `.agents/design/`.

## Configuration 🔧

Configuration comes from `.env` in the sudoku directory — `$SUDOKU_HOME`, else the current directory when its `.env`
has `ROOT_PROJECTS_FOLDER` (old layout, repository checkout), else `~/.sudoku` —, environment variables or
`/vault/secrets/CRED_<NAME>`. The same directory holds the infrastructure `compose.yaml` and `sudoku-config/` (in the
old layout they sit in `ROOT_PROJECTS_FOLDER` until `sudoku setup` moves them).

* `ROOT_PROJECTS_FOLDER` — the projects folder (required; `~` and relative paths resolve against the sudoku directory)
* `DOCKER_PATH` — docker command (default `docker` from PATH; paths with spaces are fine)
* `DOCKER_COMPOSE_PATH` — compose command (default `docker-compose` if installed, else `docker compose`)
* `MAX_WORKERS` — stop/down parallelism (1–64, default 5)
* `ADMIN_ADDR` — admin address: loopback `host:port` (default `127.0.0.1:7777`) or `unix:/path.sock`
* `ADMIN_TOKEN` — admin access token (optional)
* `PREFIX_CONTAINER_NAME` — infrastructure container-name prefix (for `sudoku setup`)

The Docker Engine is located like the docker CLI does: `DOCKER_HOST` (unix, `npipe://` on Windows, `tcp://` with
`DOCKER_TLS_VERIFY`/`DOCKER_CERT_PATH`), then the current `docker context`, then the default socket / named pipe.

Admin and CLI settings (startup order, notifications, schedules, backups, metrics) live in
`~/.sudoku/sudoku-config/admin.yaml`, sets in `sets.yaml`, disabled services in `disabled.yaml`.

Autostart of `sudoku serve` at login: macOS LaunchAgent, Linux `systemd --user`, Windows a hidden launcher in the
Startup folder.

## Infrastructure 🏗️

The project uses Docker Compose to run the local infrastructure required for the Sudoku CLI service. All services are connected via a dedicated network `sudoku_network`.

### Services 🛠️

#### Traefik 🌐

* Version: `2.11.31`
* Reverse proxy for routing traffic to other services.
* Exposed ports:

    * `8080` — Traefik web dashboard (accessible locally at `127.0.0.1:8080`)
    * `80`, `443`, `5173`, `16379`, `8000`, `25`, `110`, `143`, `465`, `587`, `993`, `995`, `8443`, `8081`, `13306`, `13307`
* Connects to the Docker socket for automatic container discovery.
* Configuration files:

    * `sudoku-config/traefik/traefik.yaml`
    * `sudoku-config/traefik/custom/`

#### Redis (KeyDB) 🟢

* Image: `eqalpha/keydb:latest`
* Primary service for caching and data storage.
* Port `6379` exposed via Traefik TCP router.
* Data stored in `sudoku-config/redis/data`.
* User and group are taken from `DOCKER_UID` and `DOCKER_GID` environment variables.

#### Elasticsearch 🔍

* Image: `elasticsearch:9.1.8`
* Used for storing and searching large volumes of data.
* Configuration:

    * `ES_JAVA_OPTS=-Xms512m -Xmx512m`
    * `discovery.type=single-node`
* Data stored in `sudoku-config/elasticsearch/data`.
* Port `9200` exposed via Traefik TCP router.

#### RabbitMQ 🐇

* Image: `rabbitmq:4.2.1-management-alpine`
* Message broker for inter-service communication.
* Exposed ports:

    * `5672` — main client port
    * `15672` — RabbitMQ management web interface
* Default credentials:

    * User: `default`
    * Password: `default`

### Network 🌐

All services are connected to the `sudoku_network` using the `bridge` driver. **IPv6** is disabled.

## Project Structure 📂

```
cmd/                  ── CLI entry point (main.go)
internal/
├── infra/
│   └── configs/      ── loading and processing configs (configs.go)
├── logger/
│   └── logger.go     ── logging
├── service/
│   └── compose.go    ── working with Docker Compose and progress bars
└── sudoku/
    ├── commands/     ── CLI commands (build, start, stop, rebuild, etc.)
    └── cli.go        ── CLI initialization and startup
```
