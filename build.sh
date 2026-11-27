#!/usr/bin/env bash
# Сборка (админка + бинарник) и деплой в ROOT_PROJECTS_FOLDER.
# Деплой делает сам бинарник (`sudoku setup`) — та же логика работает на Windows без bash.
#   OVERWRITE_ENV=1       перезаписать .env в корне (старый → .env.bak)
#   REGENERATE_COMPOSE=1  перегенерировать compose.yaml (старый → compose.yaml.bak)
set -euo pipefail
IFS=$'\n\t'

die() {
    echo "❌ $*" >&2
    exit 1
}

[[ -f .env ]] || die ".env не найден (пример: .env.example)"

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
[[ "${OVERWRITE_ENV:-0}" == "1" ]] && args+=(--overwrite-env)
[[ "${REGENERATE_COMPOSE:-0}" == "1" ]] && args+=(--regenerate-compose)

./build/sudoku setup "${args[@]+"${args[@]}"}"
echo "🎉 Готово!"
