#!/usr/bin/env bash
# Back up MongoDB, MinIO and Chroma for a docker compose deployment.
#
#   scripts/backup.sh [backup-root]     (default: ./backups)
#
# Writes <backup-root>/<UTC timestamp>/ with:
#   mongo.archive.gz   mongodump of the app database (gzip); MongoDB's own
#                      users are not included, they come from .env
#   minio.tar.gz       the MinIO data volume (uploaded files)
#   chroma.tar.gz      the Chroma data volume (vectors; Chroma is stopped
#                      for a few seconds so the copy is consistent)
#   SHA256SUMS
# and keeps the newest $KEEP backups (default 14).
#
# Nightly at 03:00 via cron:
#   0 3 * * * cd /path/to/ConverseAI && scripts/backup.sh >> backups/backup.log 2>&1
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="${1:-./backups}"
KEEP="${KEEP:-14}"
ENV_FILE="${ENV_FILE:-.env}"
COMPOSE="${COMPOSE:-docker compose --env-file $ENV_FILE}"

set -a
# shellcheck disable=SC1090
[ -f "$ENV_FILE" ] && . "$ENV_FILE"
set +a
: "${MONGO_USER:?MONGO_USER must be set (in .env)}"
: "${MONGO_PASSWORD:?MONGO_PASSWORD must be set (in .env)}"

PROJECT="$($COMPOSE config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["name"])')"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="$ROOT/$STAMP"
mkdir -p "$DEST"
DEST_ABS="$(cd "$DEST" && pwd)"
echo "[backup] writing $DEST_ABS"

echo "[backup] mongodb"
$COMPOSE exec -T converseai-db mongodump --quiet --archive --gzip --db "${DB_NAME:-ai_chat}" \
  --username "$MONGO_USER" --password "$MONGO_PASSWORD" --authenticationDatabase admin \
  > "$DEST/mongo.archive.gz"

volume_tar() { # <volume> <file>
  docker run --rm -v "$1":/data:ro -v "$DEST_ABS":/backup alpine:3.22.6 \
    tar czf "/backup/$2" -C /data .
}

echo "[backup] minio"
volume_tar "${PROJECT}_converseai_storage_data" minio.tar.gz

echo "[backup] chroma"
$COMPOSE stop converseai-vector >/dev/null
trap '$COMPOSE start converseai-vector >/dev/null' EXIT
volume_tar "${PROJECT}_converseai_vector_data" chroma.tar.gz
$COMPOSE start converseai-vector >/dev/null
trap - EXIT

(cd "$DEST" && sha256sum mongo.archive.gz minio.tar.gz chroma.tar.gz > SHA256SUMS)
echo "[backup] done: $(du -sh "$DEST" | cut -f1)"

# Retention: keep the newest $KEEP timestamped backups.
mapfile -t old < <(ls -1d "$ROOT"/*Z 2>/dev/null | sort -r | tail -n +"$((KEEP + 1))")
for dir in "${old[@]}"; do
  echo "[backup] pruning $dir"
  rm -r -- "$dir"
done
