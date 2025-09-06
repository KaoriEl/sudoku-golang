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

if [[ -f "${COMPOSE_SOURCE}" ]]; then
    if [[ ! -f "${COMPOSE_TARGET}" ]]; then
        cp -f "${COMPOSE_SOURCE}" "${COMPOSE_TARGET}"
        echo "✅ compose.yaml скопирован в: ${COMPOSE_TARGET}"
    else
        echo "ℹ️ compose.yaml уже существует в руте, копирование пропущено"
    fi
else
    echo "⚠️ compose.yaml не найден в ${CONFIG_DIR}, пропускаю копирование"
fi

rm -rf build
