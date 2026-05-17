#!/usr/bin/env bash
#
# Online SQLite backup for latasya-erp.
#
# Uses `VACUUM INTO` — same online-snapshot semantics as the backup API,
# but writes a single self-contained file in default rollback journal mode,
# so no -wal/-shm companion files are ever created in the backup directory.
# Live DB stays open the entire time — no service downtime.
#
# After the local snapshot, if R2_BUCKET is set the script also uploads the
# snapshot + .json sidecar to Cloudflare R2 via rclone. The R2 remote `r2`
# is read from RCLONE_CONFIG_R2_* env vars (loaded from /etc/latasya/r2.env
# by the systemd unit), so no rclone.conf file is needed.
#
# Configurable via env vars (defaults are production VPS paths):
#   DB_PATH      path to the live database (default: /var/lib/latasya/latasya.db)
#   BACKUP_DIR   where snapshots land   (default: /var/backups/latasya)
#   RETENTION    how many to keep       (default: 12)
#   R2_BUCKET    R2 bucket name; if unset, off-site upload is skipped
#
# Local test (no R2):
#   DB_PATH="$PWD/latasya.db" BACKUP_DIR=/tmp/latasya-backup-test ./deploy/latasya-backup.sh

set -euo pipefail

DB_PATH="${DB_PATH:-/var/lib/latasya/latasya.db}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/latasya}"
RETENTION="${RETENTION:-12}"
R2_BUCKET="${R2_BUCKET:-}"

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

if [ -n "$R2_BUCKET" ]; then
  if ! command -v rclone >/dev/null 2>&1; then
    echo "error: R2_BUCKET set but rclone is not installed" >&2
    exit 1
  fi
  # All rclone config comes from RCLONE_CONFIG_R2_* env vars, so we tell rclone
  # not to read or create any on-disk config file. Required because systemd
  # `ProtectHome=true` masks $HOME, and rclone otherwise aborts trying to open
  # $HOME/.rclone.conf.
  #
  # `--checksum` makes rclone re-verify the remote object against the local
  # sha256 after upload; if R2 received a partial/corrupt write, rclone exits
  # non-zero and `set -e` aborts the script.
  RCLONE_OPTS="--config /dev/null --checksum"
  rclone copy $RCLONE_OPTS "$BACKUP_DIR/$NAME.db"   "r2:$R2_BUCKET/"
  rclone copy $RCLONE_OPTS "$BACKUP_DIR/$NAME.json" "r2:$R2_BUCKET/"
  echo "    uploaded to r2:$R2_BUCKET/$NAME.db"
fi

KEEP_PLUS_ONE=$((RETENTION + 1))
ls -1t "$BACKUP_DIR"/latasya-*.db 2>/dev/null | tail -n +"$KEEP_PLUS_ONE" \
  | while IFS= read -r f; do rm -f "$f"; done
ls -1t "$BACKUP_DIR"/latasya-*.json 2>/dev/null | tail -n +"$KEEP_PLUS_ONE" \
  | while IFS= read -r f; do rm -f "$f"; done

echo "ok: $BACKUP_DIR/$NAME.db"
echo "    size=$SIZE bytes"
echo "    sha256=$SHA"
echo "    latest_migration=$LATEST_MIGRATION"
