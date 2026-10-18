#!/usr/bin/env bash
# Dump the database into $BACKUP_DIR/mq-<UTC time>.dump (pg_dump custom format), check that the dump reads back,
# delete local dumps older than $BACKUP_KEEP_DAYS days, and copy the new one off-site if configured.
# Settings: deploy/.env (see deploy/.env.example, "backups"). Run by mq-backup.timer, or by hand: make backup.
set -euo pipefail
# shellcheck source=deploy/backup/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

umask 077
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

stamp=$(date -u +%Y-%m-%dT%H%M%SZ)
file="$BACKUP_DIR/mq-$stamp.dump"
tmp="$file.partial"
trap 'rm -f "$tmp"' EXIT

log "dumping database $BACKUP_DB ($BACKUP_MODE) into $file"
pg pg_dump --format=custom --compress=6 "$BACKUP_DB" >"$tmp"

# a dump that pg_restore cannot list is not a backup
pg pg_restore --list <"$tmp" >/dev/null || die "the new dump does not read back: $tmp"
[ -s "$tmp" ] || die "the new dump is empty"
mv "$tmp" "$file"
log "done: $(du -h "$file" | cut -f1)"

# local retention; stale .partial files from an interrupted run go too
find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'mq-*.dump' -mtime +"$BACKUP_KEEP_DAYS" -o -name 'mq-*.partial' -mmin +60 \) \
  -print -delete | sed 's/^/removed old /' >&2

# off-site copy (retention there: a lifecycle rule on the bucket, see docs/ops.md)
if [ -n "${BACKUP_RCLONE_REMOTE:-}" ]; then
  log "uploading to $BACKUP_RCLONE_REMOTE (rclone)"
  rclone copy --no-traverse "$file" "$BACKUP_RCLONE_REMOTE/"
elif [ -n "${BACKUP_S3_URI:-}" ]; then
  log "uploading to ${BACKUP_S3_URI%/}/ (aws s3, endpoint ${BACKUP_S3_ENDPOINT:-default})"
  endpoint=()
  [ -n "${BACKUP_S3_ENDPOINT:-}" ] && endpoint=(--endpoint-url "$BACKUP_S3_ENDPOINT")
  aws s3 cp --only-show-errors "${endpoint[@]}" "$file" "${BACKUP_S3_URI%/}/$(basename "$file")"
fi
log "backup finished"
