#!/usr/bin/env bash
# Restore a backup made by scripts/backup.sh. This REPLACES the app's
# MongoDB database, uploaded files and vectors. MongoDB's own users are left
# alone (they come from MONGO_USER / MONGO_PASSWORD in .env).
#
#   scripts/restore.sh backups/20261003T030000Z [--yes]
set -euo pipefail

cd "$(dirname "$0")/.."
SRC="${1:?usage: scripts/restore.sh <backup-dir> [--yes]}"
ENV_FILE="${ENV_FILE:-.env}"
COMPOSE="${COMPOSE:-docker compose --env-file $ENV_FILE}"

set -a
# shellcheck disable=SC1090
[ -f "$ENV_FILE" ] && . "$ENV_FILE"
set +a
: "${MONGO_USER:?MONGO_USER must be set (in .env)}"
: "${MONGO_PASSWORD:?MONGO_PASSWORD must be set (in .env)}"

SRC_ABS="$(cd "$SRC" && pwd)"
(cd "$SRC_ABS" && sha256sum -c SHA256SUMS)

if [ "${2:-}" != "--yes" ]; then
  read -r -p "Replace ALL current ConverseAI data with $SRC_ABS? Type 'restore': " answer
  [ "$answer" = "restore" ] || { echo "aborted"; exit 1; }
fi

PROJECT="$($COMPOSE config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["name"])')"

step="starting"
trap 'echo "[restore] FAILED during: $step. The app is stopped; fix the problem and re-run (the restore is repeatable)." >&2' ERR

step="stopping the app"
echo "[restore] $step"
$COMPOSE stop converseai >/dev/null

# Talk to mongod via the container's network address. On a fresh volume the
# image first runs a temporary localhost-only server to create the root user
# and then restarts; connecting over the network only reaches the real one.
mongo_host='"$(hostname -i)"'
mongo_ping() {
  $COMPOSE exec -T converseai-db sh -c "mongosh --quiet --host $mongo_host \
    --username \"\$1\" --password \"\$2\" --authenticationDatabase admin \
    --eval 'db.adminCommand({ping: 1}).ok'" _ "$MONGO_USER" "$MONGO_PASSWORD" >/dev/null 2>&1
}

step="waiting for mongodb"
echo "[restore] $step"
ok=0
for _ in $(seq 1 90); do
  if mongo_ping; then ok=$((ok + 1)); else ok=0; fi
  [ "$ok" -ge 3 ] && break
  sleep 1
done
[ "$ok" -ge 3 ] || { echo "[restore] mongodb is not accepting connections" >&2; false; }

step="restoring mongodb"
echo "[restore] $step"
log="$(mktemp)"
if ! $COMPOSE exec -T converseai-db sh -c "mongorestore --host $mongo_host --drop --archive --gzip --nsInclude '${DB_NAME:-ai_chat}.*' \
  --username \"\$1\" --password \"\$2\" --authenticationDatabase admin" _ "$MONGO_USER" "$MONGO_PASSWORD" \
  < "$SRC_ABS/mongo.archive.gz" > "$log" 2>&1; then
  cat "$log" >&2
  rm -f "$log"
  false
fi
grep -E "document\(s\) restored" "$log" || true
rm -f "$log"

volume_restore() { # <service> <volume> <file>
  $COMPOSE stop "$1" >/dev/null
  docker run --rm -v "$2":/data -v "$SRC_ABS":/backup:ro alpine:3.22.6 \
    sh -c 'find /data -mindepth 1 -delete && tar xzf "/backup/$0" -C /data' "$3"
  $COMPOSE start "$1" >/dev/null
}

step="restoring minio"
echo "[restore] minio"
volume_restore converseai-storage "${PROJECT}_converseai_storage_data" minio.tar.gz
step="restoring chroma"
echo "[restore] chroma"
volume_restore converseai-vector "${PROJECT}_converseai_vector_data" chroma.tar.gz

step="starting the app"
echo "[restore] starting the app"
$COMPOSE up -d --wait converseai >/dev/null
echo "[restore] done"
