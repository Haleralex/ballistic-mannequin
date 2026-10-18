#!/usr/bin/env bash
# Restore a dump made by backup.sh.
#
#   restore.sh [--db NAME] [--fresh] [--yes] FILE
#
#   FILE        a local mq-*.dump (an off-site copy has to be downloaded first, see docs/ops.md)
#   --db NAME   restore into database NAME instead of $BACKUP_DB (mq); it is created when missing
#   --fresh     drop and re-create the target database first (a clean restore, as after losing the server)
#   --yes       do not ask for confirmation when the target is the live database
#
# Check a backup without touching the live data:   restore.sh --db mq_restore_check --fresh FILE
# Restore the live database (stop the API first):  docker compose -f deploy/compose.prod.yml stop api
#                                                  restore.sh --fresh FILE ; then start the API again
# Same settings and connection modes as backup.sh (deploy/backup/lib.sh).
set -euo pipefail
# shellcheck source=deploy/backup/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

usage() { sed -n '2,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 2; }

db=$BACKUP_DB fresh=0 yes=0 file=
while [ $# -gt 0 ]; do
  case "$1" in
    --db) [ $# -ge 2 ] || usage; db=$2; shift 2 ;;
    --fresh) fresh=1; shift ;;
    --yes) yes=1; shift ;;
    -h | --help) usage ;;
    -*) usage ;;
    *) [ -z "$file" ] || usage; file=$1; shift ;;
  esac
done
[ -n "$file" ] || usage
[ -f "$file" ] || die "no such file: $file"
[[ "$db" =~ ^[a-z_][a-z0-9_]*$ ]] || die "database name must be lower-case letters, digits and _: $db"

pg pg_restore --list <"$file" >/dev/null || die "not a pg_dump custom-format file: $file"

if [ "$db" = "$BACKUP_DB" ] && [ "$yes" -ne 1 ]; then
  [ -t 0 ] || die "restoring into the live database $db needs --yes when not run from a terminal"
  echo "This replaces the data in the LIVE database '$db' with $file." >&2
  echo "Stop the API first: docker compose -f deploy/compose.prod.yml stop api" >&2
  read -r -p "Type the database name to continue: " answer
  [ "$answer" = "$db" ] || die "cancelled"
fi

exists=$(pg psql -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname = '$db'")
if [ "$fresh" -eq 1 ] && [ -n "$exists" ]; then
  log "dropping database $db"
  pg psql -d postgres -v ON_ERROR_STOP=1 -qc "DROP DATABASE \"$db\" WITH (FORCE)"
  exists=
fi
if [ -z "$exists" ]; then
  log "creating database $db"
  pg psql -d postgres -v ON_ERROR_STOP=1 -qc "CREATE DATABASE \"$db\""
fi

clean=()
[ "$fresh" -eq 1 ] || clean=(--clean --if-exists)
log "restoring $file into $db"
pg pg_restore --dbname="$db" --no-owner --no-acl --single-transaction --exit-on-error "${clean[@]}" <"$file"

log "restored; rows per table:"
pg psql -d "$db" -v ON_ERROR_STOP=1 -At -F ' ' <<'SQL' >&2
SELECT format('SELECT %L, count(*) FROM %I.%I', schemaname || '.' || relname, schemaname, relname)
FROM pg_stat_user_tables ORDER BY schemaname, relname
\gexec
SQL
log "restore finished"
