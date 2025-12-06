#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

die() {
    echo "❌ $*" >&2
    exit 1
}

if [[ ! -f .env ]]; then
    die ".env не найден"
fi

sed -i 's/\r$//' .env

ROOT_PROJECTS_FOLDER=$(grep -E '^ROOT_PROJECTS_FOLDER=' .env | cut -d '=' -f2- | xargs)
if [[ -z "${ROOT_PROJECTS_FOLDER}" ]]; then
    die "ROOT_PROJECTS_FOLDER не задан в .env"
fi

mkdir -p "${ROOT_PROJECTS_FOLDER}"
mkdir -p build

BIN_NAME="sudoku"
echo "🚀 Собираю проект..."
go build -o "build/${BIN_NAME}" ./cmd/main.go

TARGET="${ROOT_PROJECTS_FOLDER}/${BIN_NAME}"
cp -f "build/${BIN_NAME}" "${TARGET}"
echo "✅ Бинарник скопирован в: ${TARGET}"

cp -f .env "${ROOT_PROJECTS_FOLDER}/.env"
echo "✅ .env скопирован в: ${ROOT_PROJECTS_FOLDER}/.env"

CONFIG_DIR="sudoku-config"
CONFIG_TARGET="${ROOT_PROJECTS_FOLDER}/${CONFIG_DIR}"

if [[ ! -d "${CONFIG_TARGET}" ]]; then
    if [[ -d "${CONFIG_DIR}" ]]; then
        cp -r "${CONFIG_DIR}" "${CONFIG_TARGET}"
        echo "✅ Папка конфигурации скопирована в: ${CONFIG_TARGET}"
    else
        echo "⚠️ Папка ${CONFIG_DIR} не найдена в текущей директории, пропускаю копирование"
    fi
else
    echo "ℹ️ Папка ${CONFIG_DIR} уже существует в руте, копирование пропущено"
fi

COMPOSE_SOURCE="${CONFIG_DIR}/compose.yaml"
COMPOSE_TARGET="${ROOT_PROJECTS_FOLDER}/compose.yaml"

if [[ ! -f "${COMPOSE_SOURCE}" ]]; then
    die "⚠️ compose.yaml не найден в ${CONFIG_DIR}"
fi

PREFIX=$(grep '^PREFIX_CONTAINER_NAME=' .env | cut -d '=' -f2- | xargs)

echo "🔍 Генерирую новый compose.yaml с префиксом: '${PREFIX}' в ${COMPOSE_TARGET}"

awk -v prefix="$PREFIX" '
function trim(s) { gsub(/^[ \t"]+|[ \t"]+$/, "", s); return s }

# container_name
/^[[:space:]]*container_name:/ {
    indent = substr($0, 1, match($0, /container_name:/)-1)
    val = trim(substr($0, index($0,$2)))
    n = split(val, parts, "_")
    svc = parts[n]
    if (length(prefix) > 0) {
        newval = prefix "_" svc
    } else {
        newval = svc
    }
    print indent "container_name: " newval
    print "♻️ container_name: → " newval > "/dev/stderr"
    next
}

# hostname
/^[[:space:]]*hostname:/ {
    indent = substr($0, 1, match($0, /hostname:/)-1)
    val = trim(substr($0, index($0,$2)))
    print indent "hostname: " val
    print "♻️ hostname: → " val > "/dev/stderr"
    next
}

{print}
' "$COMPOSE_SOURCE" > "$COMPOSE_TARGET"

rm -rf build
echo "🎉 Готово!"
