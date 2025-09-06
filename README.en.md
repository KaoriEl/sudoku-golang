<p align="center">
  <img src="img/logo.png" alt="Sudoku Golang CLI Logo" width="250"/>
</p>

📘 Documentation available in other languages:

* 🇬🇧 [Русский](README.md)

---

## Description 📝

This project is a CLI tool written in Go for managing infrastructure and services related to Sudoku via `docker-compose`. It allows you to conveniently build, start, stop, and rebuild containers through a set of commands.

---

## Table of Contents 📑

* [Description](#description)
* [Requirements](#requirements)
* [Installation and Build](#installation-and-build)
* [Available Commands](#available-commands)
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

1. Clone the repository:

```sh
git clone
cd sudoku-golang
````

2. Create a `.env` file in the project root and set the required variables (see [Configuration](#configuration)).

3. Run the build script:

```sh
make build
```

The script performs the following actions:

* ✅ Checks for the existence of `.env` and the `ROOT_PROJECTS_FOLDER` variable.
* 📂 Creates necessary folders: `${ROOT_PROJECTS_FOLDER}` and `build`.
* 🛠️ Builds the `sudoku` binary into the `build` folder.
* 📁 Copies the binary and `.env` to `${ROOT_PROJECTS_FOLDER}`.
* 📄 Copies the configuration folder `sudoku-config` and the `compose.yaml` file to `${ROOT_PROJECTS_FOLDER}` (if not already copied).

4. After successful execution, the binary and configuration will be ready to use in `${ROOT_PROJECTS_FOLDER}`.

## Available Commands 💻

Commands can be run via the `sudoku` binary or directly via Go:

```sh
./sudoku build        # Builds all containers
./sudoku start        # Starts the built containers
./sudoku stop         # Stops all containers
./sudoku forceBuild   # Forces container build
./sudoku down         # Stops and removes all containers
./sudoku restart      # Restarts all containers
./sudoku rebuild      # Stops, builds, and starts containers
./sudoku forceRebuild # Forces container rebuild
```

## Configuration 🔧

Configuration is loaded from `.env` or environment variables.

Available variables:

* `DOCKER_PATH` — path to the Docker executable (default: `/usr/bin/env docker`)
* `DOCKER_COMPOSE_PATH` — path to Docker Compose (default: `/usr/bin/env docker compose`)
* `MAX_WORKERS` — number of workers (default: 5)
* `ROOT_PROJECTS_FOLDER` — root folder for projects where the binary and configuration will be copied (required for the script to work)

## Infrastructure 🏗️

The project uses Docker Compose to run the local infrastructure required for the Sudoku CLI service. All services are connected via a dedicated network `sudoku_network`.

### Services 🛠️

#### Traefik 🌐

* Version: `2.9.6`
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

* Image: `elasticsearch:7.14.1`
* Used for storing and searching large volumes of data.
* Configuration:

    * `ES_JAVA_OPTS=-Xms512m -Xmx512m`
    * `discovery.type=single-node`
* Data stored in `sudoku-config/elasticsearch/data`.
* Port `9200` exposed via Traefik TCP router.

#### RabbitMQ 🐇

* Image: `rabbitmq:3.12-management-alpine`
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