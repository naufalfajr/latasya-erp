# Backup and Restore

Off-site backup and restore-drill system for the Latasya ERP SQLite database.

## Design

| Layer | Choice |
|---|---|
| **Off-site storage** | Cloudflare R2 (10 GB free tier, no egress fees, same trust boundary as the existing Cloudflare Tunnel) |
| **At-rest encryption** | R2 server-side default (AES-256, Cloudflare-managed keys) |
| **In-transit encryption** | HTTPS / TLS (rclone + aws SDK defaults) |
| **Backup cadence** | Weekly — `latasya-backup.timer` fires Sundays at 19:00 UTC (02:00 Monday Jakarta) |
| **Retention** | Local: `RETENTION=12` snapshots on the VPS. R2: no automatic deletion — every backup kept (matches UU KUP 10-year mandate). |
| **Restore drill** | `.github/workflows/restore-drill.yml` — weekly, GitHub-hosted runner, four layers of validation |
| **Alerting** | GitHub Actions native email on workflow failure (no dead-man switch yet — revisit if/when a scare happens) |

## Restore-drill validation layers

| Layer | What it catches |
|---|---|
| **0. Freshness** | Upload pipeline silently stopped on the VPS — fails if latest R2 backup is older than 14 days |
| **1. Integrity** | Backup file corrupt — `PRAGMA integrity_check` must return `ok` |
| **2. App compat** | Current binary can't open the backup — boots `latasya-erp` and curls `/healthz` |
| **3. Business invariant** | Data inside the backup is internally inconsistent — `SUM(debit) == SUM(credit)` on `journal_lines` |

## Threat model and accepted trade-offs

Protected against: VPS disk failure, accidental `rm`, ransomware on the VPS, file corruption, schema-migration regressions, silent upload-pipeline failure.

Not protected against (knowingly):
- **Cloudflare account compromise** — mitigated by 2FA on the CF account, not by application-layer encryption.
- **Silent failure of the GitHub Actions cron itself** — no dead-man switch (e.g. healthchecks.io) yet. If a drill workflow is skipped entirely, no email arrives. Revisit after the first scare.

---

## One-time manual setup

These steps cannot be automated from the repo and must be performed by hand the first time.

### 1. Cloudflare R2 bucket

Cloudflare dashboard → R2 → **Create bucket** → name: `latasya-backups`. Default settings.

Note your **Cloudflare account ID** (top-right of the R2 page). You'll need it in steps 2 and 3.

### 2. Two scoped R2 API tokens

R2 → **Manage R2 API Tokens** → create two tokens, each scoped to `latasya-backups` only:

| Token name | Permissions | Lives on |
|---|---|---|
| `latasya-vps-uploader` | **Object Read & Write** on `latasya-backups` only | VPS `/etc/latasya/r2.env` |
| `latasya-drill-reader` | **Object Read** on `latasya-backups` only | GitHub Actions Secrets |

Each token gives you an Access Key ID and Secret Access Key. Save both pairs — Cloudflare only shows the secret once.

### 3. VPS — R2 credentials and rclone

On the VPS:

```bash
# Install rclone.
curl -fsSL https://rclone.org/install.sh | sudo bash
rclone version

# Create the credentials file. The latasya-backup.service unit loads this.
sudo install -d -m 750 -o latasya -g latasya /etc/latasya
sudo tee /etc/latasya/r2.env >/dev/null <<'EOF'
R2_BUCKET=latasya-backups
RCLONE_CONFIG_R2_TYPE=s3
RCLONE_CONFIG_R2_PROVIDER=Cloudflare
RCLONE_CONFIG_R2_ACCESS_KEY_ID=<paste vps-uploader access key id>
RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=<paste vps-uploader secret access key>
RCLONE_CONFIG_R2_ENDPOINT=https://<cloudflare account id>.r2.cloudflarestorage.com
EOF
sudo chown latasya:latasya /etc/latasya/r2.env
sudo chmod 600 /etc/latasya/r2.env
```

Reload systemd to pick up the new `.service` and `.timer`:

```bash
sudo cp deploy/latasya-backup.service /etc/systemd/system/
sudo cp deploy/latasya-backup.timer   /etc/systemd/system/
sudo cp deploy/latasya-backup.sh      /usr/local/bin/latasya-backup.sh
sudo chmod 755 /usr/local/bin/latasya-backup.sh
sudo systemctl daemon-reload
sudo systemctl enable --now latasya-backup.timer
```

Fire one backup manually to verify the chain works end-to-end:

```bash
sudo systemctl start latasya-backup.service
sudo journalctl -u latasya-backup.service -n 50 --no-pager
# Should see "uploaded to r2:latasya-backups/latasya-<TS>.db"
```

Confirm the object landed in R2 (Cloudflare dashboard → R2 → `latasya-backups`).

### 4. GitHub Actions secrets for the drill

Repo → **Settings** → **Secrets and variables** → **Actions** → **New repository secret**. Add three:

| Name | Value |
|---|---|
| `R2_ACCOUNT_ID` | Cloudflare account ID (32 hex chars, no scheme) |
| `R2_READ_KEY_ID` | `latasya-drill-reader` access key id |
| `R2_READ_SECRET` | `latasya-drill-reader` secret access key |

Trigger the drill manually once to confirm: repo → **Actions** → **Restore drill** → **Run workflow**. Should turn green within ~3 minutes.

---

## Day-to-day operations

### Manually trigger a backup

```bash
sudo systemctl start latasya-backup.service
```

### Manually trigger a drill

GitHub repo → **Actions** → **Restore drill** → **Run workflow** → **Run workflow** (green button).

### Restore from R2 onto a fresh machine

This is the procedure to follow if the VPS is lost or `latasya.db` is corrupt. Tested implicitly every week by the drill.

```bash
# 1. On any machine with rclone installed and R2 credentials configured:
mkdir restore && cd restore

# 2. Find the latest backup.
rclone lsf r2:latasya-backups/ --include "latasya-*.db" | sort -r | head -5
# Pick one — typically the newest, unless you're recovering from a known-bad point in time.

LATEST=latasya-<YYYYMMDD-HHMMSS>.db
rclone copy "r2:latasya-backups/$LATEST"        ./
rclone copy "r2:latasya-backups/${LATEST%.db}.json" ./

# 3. Verify the snapshot matches its sidecar checksum.
EXPECTED_SHA=$(jq -r .sha256 "${LATEST%.db}.json")
ACTUAL_SHA=$(sha256sum "$LATEST" | awk '{print $1}')
[ "$EXPECTED_SHA" = "$ACTUAL_SHA" ] || { echo "CHECKSUM MISMATCH"; exit 1; }

# 4. Integrity check.
sqlite3 "$LATEST" "PRAGMA integrity_check;"
# Must print: ok

# 5. Boot the current binary against it.
DB_PATH="$PWD/$LATEST" PORT=8080 ./latasya-erp
# Log in at http://localhost:8080 — verify dashboard balance and recent transactions.

# 6. When satisfied, install the restored DB as the new live one on the recovery VPS:
#    sudo install -m 600 -o latasya -g latasya "$LATEST" /var/lib/latasya/latasya.db
#    sudo systemctl restart latasya-erp
```

### Rotate R2 tokens

Recommended cadence: **annually**, or immediately on suspected compromise. Reminder in this doc — set a calendar event.

1. Cloudflare → R2 → API Tokens → create a new token with the same scope.
2. Update `/etc/latasya/r2.env` on the VPS (for `latasya-vps-uploader`) **or** the GitHub Actions secret (for `latasya-drill-reader`).
3. Fire one manual backup or drill to confirm the new credentials work.
4. Revoke the old token in the Cloudflare dashboard.

---

## File locations

| File | Purpose |
|---|---|
| `deploy/latasya-backup.sh` | The backup script. Installed as `/usr/local/bin/latasya-backup.sh`. |
| `deploy/latasya-backup.service` | systemd unit. Loads `/etc/latasya/r2.env`. |
| `deploy/latasya-backup.timer` | systemd timer. Sundays 19:00 UTC. |
| `/etc/latasya/r2.env` | VPS R2 credentials. Mode 0600, owner `latasya:latasya`. **Not in git.** |
| `/var/backups/latasya/` | Local rolling snapshots (retention=12). |
| `.github/workflows/restore-drill.yml` | Weekly drill workflow. |
| GH Actions Secrets | `R2_ACCOUNT_ID`, `R2_READ_KEY_ID`, `R2_READ_SECRET`. |
