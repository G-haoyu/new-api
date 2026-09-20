#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_PORT="5678"
FRONTEND_PORT="1234"
BACKEND_URL="http://localhost:${BACKEND_PORT}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/new-api-gocache}"

cd "${ROOT_DIR}"

if [[ ! -f .env ]]; then
  echo "Missing ${ROOT_DIR}/.env. Configure the local database and Redis first." >&2
  exit 1
fi

stop_existing_listener() {
  local port="$1"
  local pids

  if ! command -v lsof >/dev/null 2>&1; then
    return 0
  fi

  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -z "${pids}" ]]; then
    return 0
  fi

  echo "Stopping existing service(s) on port ${port}: ${pids//$'\n'/ }"
  kill ${pids} 2>/dev/null || true
  for _ in {1..20}; do
    if ! lsof -tiTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done

  echo "Unable to stop the service on port ${port}." >&2
  exit 1
}

stop_existing_listener "${BACKEND_PORT}"
stop_existing_listener "${FRONTEND_PORT}"

if [[ ! -d web/node_modules ]]; then
  echo "Installing frontend dependencies..."
  (cd web && bun install --frozen-lockfile)
fi

echo "Building frontend assets required by the Go embed..."
(cd web && DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="$(cat ../VERSION)" bun run build)

cleanup() {
  trap - EXIT INT TERM
  if [[ -n "${BACKEND_PID:-}" ]] && kill -0 "${BACKEND_PID}" 2>/dev/null; then
    kill "${BACKEND_PID}" 2>/dev/null || true
    wait "${BACKEND_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "Starting backend on ${BACKEND_URL}..."
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
GOCACHE="${GOCACHE_DIR}" \
go run main.go &
BACKEND_PID=$!

echo "Starting frontend on http://localhost:${FRONTEND_PORT}..."
cd web
VITE_REACT_APP_SERVER_URL="${BACKEND_URL}" \
  bun run dev -- --host 0.0.0.0 --port "${FRONTEND_PORT}"
