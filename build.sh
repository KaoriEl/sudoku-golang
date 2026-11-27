#!/usr/bin/env bash
# Сборка (админка + бинарник) и установка: бинарник в ~/.local/bin, .env/compose.yaml/sudoku-config в ~/.sudoku.
# Установку делает сам бинарник (`sudoku setup`) — та же логика работает на Windows без bash.
# .env репозитория (если есть) — шаблон для ~/.sudoku/.env и источник ROOT_PROJECTS_FOLDER.
#   OVERWRITE_ENV=1       пересоздать ~/.sudoku/.env (старый → .env.bak)
#   REGENERATE_COMPOSE=1  перегенерировать ~/.sudoku/compose.yaml (старый → compose.yaml.bak)
set -euo pipefail
IFS=$'\n\t'

die() {
    echo "❌ $*" >&2
    exit 1
}

mkdir -p build

if [[ -d web ]]; then
    command -v npm >/dev/null 2>&1 || die "npm не найден — он нужен для сборки админки (web/)"
    echo "🎨 Собираю админку..."
    (cd web && npm ci --no-audit --no-fund && npm run build)
fi

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
echo "🚀 Собираю sudoku ${VERSION}..."
go build -trimpath -ldflags "-s -w -X sudoku-golang/internal/version.Version=${VERSION}" -o build/sudoku ./cmd/main.go

args=()
if [[ -f .env ]]; then
    root=$(grep -E '^[[:space:]]*ROOT_PROJECTS_FOLDER=' .env | tail -n1 | cut -d= -f2- | tr -d '"'"'"'\r' || true)
    [[ -n "${root}" ]] && args+=(--root "${root}")
fi
[[ "${OVERWRITE_ENV:-0}" == "1" ]] && args+=(--overwrite-env)
[[ "${REGENERATE_COMPOSE:-0}" == "1" ]] && args+=(--regenerate-compose)

./build/sudoku setup "${args[@]+"${args[@]}"}"
echo "🎉 Готово!"
