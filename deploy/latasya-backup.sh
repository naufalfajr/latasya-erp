#!/usr/bin/env bash
#
# Online SQLite backup for latasya-erp.
#
# Uses `VACUUM INTO` — same online-snapshot semantics as the backup API,
# but writes a single self-contained file in default rollback journal mode,
# so no -wal/-shm companion files are ever created in the backup directory.
# Live DB stays open the entire time — no service downtime.
#
# Configurable via env vars (defaults are production VPS paths):
#   DB_PATH      path to the live database (default: /var/lib/latasya/latasya.db)
#   BACKUP_DIR   where snapshots land   (default: /var/backups/latasya)
#   RETENTION    how many to keep       (default: 12)
#
# Local test:
#   DB_PATH="$PWD/latasya.db" BACKUP_DIR=/tmp/latasya-backup-test ./deploy/latasya-backup.sh

set -euo pipefail

DB_PATH="${DB_PATH:-/var/lib/latasya/latasya.db}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/latasya}"
RETENTION="${RETENTION:-12}"

if [ ! -f "$DB_PATH" ]; then
  echo "error: DB not found at $DB_PATH" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  echo "error: neither sha256sum nor shasum is installed" >&2
  exit 1
fi

TS=$(date -u +%Y%m%d-%H%M%S)
NAME="latasya-$TS"

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
cd "$BACKUP_DIR"

# Clean up the temp snapshot if the script is killed mid-run.
trap 'rm -f "$BACKUP_DIR/$NAME.db.tmp"' EXIT

sqlite3 "$DB_PATH" "VACUUM INTO '$NAME.db.tmp';"

if ! sqlite3 "$NAME.db.tmp" "PRAGMA integrity_check;" | grep -qx ok; then
  echo "error: integrity_check failed on snapshot" >&2
  rm -f "$NAME.db.tmp"
  exit 1
fi

mv "$NAME.db.tmp" "$NAME.db"
chmod 600 "$NAME.db"

LATEST_MIGRATION=$(sqlite3 "$NAME.db" \
  "SELECT filename FROM schema_migrations ORDER BY filename DESC LIMIT 1;")
SHA=$(sha256_of "$NAME.db")
SIZE=$(wc -c < "$NAME.db" | tr -d ' ')

printf '{"created_at":"%s","sha256":"%s","byte_size":%s,"latest_migration":"%s"}\n' \
  "$TS" "$SHA" "$SIZE" "$LATEST_MIGRATION" > "$NAME.json"
chmod 600 "$NAME.json"

KEEP_PLUS_ONE=$((RETENTION + 1))
ls -1t "$BACKUP_DIR"/latasya-*.db 2>/dev/null | tail -n +"$KEEP_PLUS_ONE" \
  | while IFS= read -r f; do rm -f "$f"; done
ls -1t "$BACKUP_DIR"/latasya-*.json 2>/dev/null | tail -n +"$KEEP_PLUS_ONE" \
  | while IFS= read -r f; do rm -f "$f"; done

echo "ok: $BACKUP_DIR/$NAME.db"
echo "    size=$SIZE bytes"
echo "    sha256=$SHA"
echo "    latest_migration=$LATEST_MIGRATION"
