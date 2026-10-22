#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.env.local}"
START_WORKER=0
MIGRATION_COMMAND=""
cd "$ROOT_DIR"

usage() {
  printf 'Usage: %s [--worker | --migrate up|status]\n' "$0"
}

while (($#)); do
  case "$1" in
    --worker)
      START_WORKER=1
      ;;
    --migrate)
      if (($# < 2)) || [[ "$2" != up && "$2" != status ]]; then
        printf 'Migration command must be up or status.\n' >&2
        usage >&2
        exit 2
      fi
      MIGRATION_COMMAND="$2"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if (( START_WORKER )) && [[ -n "$MIGRATION_COMMAND" ]]; then
  printf '%s\n' '--worker and --migrate cannot be used together.' >&2
  exit 2
fi

if [[ ! -f "$ENV_FILE" ]]; then
  printf 'Missing config: %s\nCopy .env.local.example to .env.local and fill in the connection values.\n' "$ENV_FILE" >&2
  exit 1
fi

set -a
# .env.local is a developer-owned shell environment file.
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

require_value() {
  local name="$1"
  if [[ -z "${!name:-}" || "${!name}" == *CHANGE_ME* ]]; then
    printf 'Set %s in %s before starting.\n' "$name" "$ENV_FILE" >&2
    exit 1
  fi
}

for name in MYSQL_DSN REDIS_ADDR REDIS_PASSWORD; do
  require_value "$name"
done

if [[ "${APP_ENV:-local}" != local ]]; then
  printf 'APP_ENV must be local in %s.\n' "$ENV_FILE" >&2
  exit 1
fi

export APP_ENV=local
export HTTP_ADDR="${HTTP_ADDR:-127.0.0.1:8080}"

WORKER_PID=""
API_PID=""

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  for pid in "$API_PID" "$WORKER_PID"; do
    if [[ -n "$pid" ]]; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  for pid in "$API_PID" "$WORKER_PID"; do
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if (( START_WORKER )); then
  printf 'Starting Worker. It will consume tasks from the shared Redis queue and run the scheduler.\n' >&2
  go run ./cmd/worker &
  WORKER_PID=$!
fi

if [[ -n "$MIGRATION_COMMAND" ]]; then
  printf 'Running database migration: %s\n' "$MIGRATION_COMMAND"
  go run ./cmd/api migrate "$MIGRATION_COMMAND"
  exit $?
fi

printf 'Starting local API at %s with direct MySQL/Redis connections. Verify that MYSQL_DSN targets training_dev.\n' "$HTTP_ADDR"
go run ./cmd/api serve &
API_PID=$!
wait "$API_PID"
