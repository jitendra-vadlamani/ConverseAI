#!/usr/bin/env bash
# Build and start ConverseAI with docker compose, after checking .env.
#
#   ./deploy.sh             # app + MongoDB + MinIO + Chroma
#   ./deploy.sh --tls       # also Caddy on :80/:443 (see deploy/Caddyfile)
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "No .env found. Create one from .env.example and fill in every secret:" >&2
  echo "  cp .env.example .env" >&2
  exit 1
fi

missing=()
for var in JWT_SECRET DB_ENCRYPTION_KEY MONGO_USER MONGO_PASSWORD MINIO_ROOT_USER MINIO_ROOT_PASSWORD; do
  value="$(grep -E "^${var}=" .env | tail -1 | cut -d= -f2- || true)"
  if [ -z "$value" ] || [[ "$value" == change-me* ]]; then
    missing+=("$var")
  fi
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "Set real values in .env for: ${missing[*]}" >&2
  echo "Generate them with: openssl rand -hex 32 (JWT_SECRET), openssl rand -hex 16 (the others)" >&2
  exit 1
fi

args=(up -d --build --wait)
profiles=()
[ "${1:-}" = "--tls" ] && profiles=(--profile tls)

VERSION="$(git rev-parse --short HEAD 2>/dev/null || echo dev)" docker compose "${profiles[@]}" "${args[@]}"
port="$(grep -E '^HOST_PORT_APP=' .env | cut -d= -f2- || true)"
echo "ConverseAI is running on port ${port:-8080}. Readiness: curl -s localhost:${port:-8080}/readyz"
