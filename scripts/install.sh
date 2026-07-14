#!/usr/bin/env bash
# SWAPI one-click install script
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION="__VERSION__"  # 由 build-swapi-images.ps1 注入

echo "=== SWAPI Deploy Bundle v${VERSION} ==="
echo ""

# Step 1: Load Docker image
echo "[1/4] Loading Docker image..."
docker load -i "$DIR/swapi-${VERSION}.tar"
echo "  Done."
echo ""

# Step 2: Configure environment
echo "[2/4] Configuring environment..."
if [ ! -f "$DIR/.env.production" ]; then
  cp "$DIR/.env.production.example" "$DIR/.env.production"
  echo "  Created .env.production from template."
  echo "  >> Please edit .env.production to set passwords:"
  echo "     nano $DIR/.env.production"
  echo "  >> Then re-run: bash $DIR/install.sh"
  exit 0
fi
echo "  .env.production exists."
echo ""

# Step 3: Deploy
echo "[3/4] Starting containers..."
DEPLOY_VERSION="${VERSION}" docker compose -p swapi \
  --env-file "$DIR/.env.production" \
  -f "$DIR/docker-compose.deploy.yml" \
  up -d
echo "  Done."
echo ""

# Step 4: Verify
echo "[4/4] Verifying..."
sleep 15
bash "$DIR/scripts/verify-services.sh"
echo ""
echo "=== SWAPI Deploy Complete! ==="
echo "  API:   https://api.sinxwhalex.com/api/status"
echo "  Admin: https://api.sinxwhalex.com"
echo ""
echo "  Next: Configure Nginx + SSL if not done yet (see deployment plan)."
