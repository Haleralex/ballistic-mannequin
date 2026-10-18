# Shared by backup.sh and restore.sh: settings and how to reach Postgres. Sourced, not run.
# shellcheck shell=bash

DEPLOY_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# Settings come from the environment; deploy/.env fills in what is not set (systemd already loads it).
if [ -f "$DEPLOY_DIR/.env" ]; then
  while IFS='=' read -r key value; do
    case "$key" in '' | \#*) continue ;; esac
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    [ -n "${!key+x}" ] || export "$key=$value"
  done <"$DEPLOY_DIR/.env"
fi

BACKUP_DIR=${BACKUP_DIR:-/var/backups/mq}
BACKUP_KEEP_DAYS=${BACKUP_KEEP_DAYS:-14}

# How to run pg_dump / pg_restore / psql:
#   docker (default) — inside the postgres container of a compose project (no Postgres tools needed on the host);
#   local            — the host's tools; the server comes from the usual libpq variables (PGHOST, PGPORT, PGUSER,
#                      PGPASSWORD), e.g. for a managed Postgres.
BACKUP_MODE=${BACKUP_MODE:-docker}
BACKUP_COMPOSE_FILE=${BACKUP_COMPOSE_FILE:-$DEPLOY_DIR/compose.prod.yml}
BACKUP_SERVICE=${BACKUP_SERVICE:-postgres}
# the database dumped and, by default, restored into
BACKUP_DB=${BACKUP_DB:-mq}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "error: $*"; exit 1; }

# pg <tool> [args…]: run a Postgres client tool against the server. stdin/stdout pass through.
pg() {
  local tool=$1
  shift
  case "$BACKUP_MODE" in
    docker)
      local env_file=()
      [ -f "$DEPLOY_DIR/.env" ] && env_file=(--env-file "$DEPLOY_DIR/.env")
      # -U from the container's own POSTGRES_USER; the local socket needs no password
      docker compose -f "$BACKUP_COMPOSE_FILE" "${env_file[@]}" exec -T "$BACKUP_SERVICE" \
        sh -c 'tool=$1; shift; exec "$tool" -U "$POSTGRES_USER" "$@"' sh "$tool" "$@"
      ;;
    local) "$tool" "$@" ;;
    *) die "BACKUP_MODE must be docker or local, not '$BACKUP_MODE'" ;;
  esac
}
