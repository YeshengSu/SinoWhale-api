#!/usr/bin/env bash
# SWAPI health check script
set -euo pipefail

PASS=0
FAIL=0

check() {
  local name="$1"
  local cmd="$2"
  local expected="$3"
  echo -n "  [$name] "
  if result=$(eval "$cmd" 2>/dev/null); then
    if echo "$result" | grep -q "$expected"; then
      echo "PASS"
      PASS=$((PASS + 1))
    else
      echo "FAIL (expected: $expected, got: $result)"
      FAIL=$((FAIL + 1))
    fi
  else
    echo "FAIL (command failed)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== SWAPI Health Check ==="
echo ""

# Container status
echo "[1/4] Container Status:"
check "swapi-new-api" \
  "docker inspect --format='{{.State.Status}}' swapi-new-api" \
  "running"
check "swapi-postgres" \
  "docker inspect --format='{{.State.Status}}' swapi-postgres" \
  "running"
check "swapi-redis" \
  "docker inspect --format='{{.State.Status}}' swapi-redis" \
  "running"
echo ""

# API health
echo "[2/4] API Health:"
check "api-status" \
  "curl -s http://localhost:3088/api/status" \
  '"success"'
echo ""

# Database connectivity
echo "[3/4] Database Connectivity:"
check "postgres-ready" \
  "docker exec swapi-postgres pg_isready -U swapi -d new-api" \
  "accepting"
check "redis-ping" \
  "docker exec swapi-redis redis-cli ping" \
  "PONG"
echo ""

# External access (requires Nginx + DNS)
echo "[4/4] External Access (requires Nginx + DNS):"
check "https-api" \
  "curl -sk https://api.sinxwhalex.com/api/status" \
  '"success"'
echo ""

echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
