#!/usr/bin/env bash
# SWAPI one-click install script
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION="v0.1.0"
echo "=== SWAPI Deploy Bundle v${VERSION} ==="
echo ""
echo "[1/4] Loading Docker image..."
docker load -i "$DIR/swapi-${VERSION}.tar"
echo "[2/4] Configuring env..."
if [ ! -f "$DIR/.env.production" ]; then
  cp "$DIR/.env.production.example" "$DIR/.env.production"
  echo "  Created .env.production from template."
  echo "  >> Please edit .env.production to set passwords:"
  echo "     nano $DIR/.env.production"
  echo "  >> Then re-run: bash $DIR/install.sh"
  exit 0
fi
echo "[3/4] Deploying..."
DEPLOY_VERSION="${VERSION}" docker compose -p swapi --env-file "$DIR/.env.production" -f "$DIR/docker-compose.deploy.yml" up -d
echo "[4/4] Verifying..."
sleep 15
bash "$DIR/scripts/verify-services.sh"
echo ""
echo "=== SWAPI Deploy Complete! ==="
echo "  API:   https://api.sinxwhalex.com/api/status"
echo "  Admin: https://api.sinxwhalex.com"
