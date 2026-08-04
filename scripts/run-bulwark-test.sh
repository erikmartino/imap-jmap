#!/usr/bin/env bash
set -euo pipefail

# Script to start imap-jmap server and Bulwark webmail locally for integration testing.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

PORT="${PORT:-8080}"
BULWARK_PORT="${BULWARK_PORT:-3000}"
CONTAINER_CMD=""

if command -v podman &>/dev/null; then
    CONTAINER_CMD="podman"
elif command -v docker &>/dev/null; then
    CONTAINER_CMD="docker"
else
    echo "Error: Neither podman nor docker command found." >&2
    exit 1
fi

echo "==> Using container tool: ${CONTAINER_CMD}"

# 1. Start imap-jmap server locally if not already running on $PORT
if curl -s "http://127.0.0.1:${PORT}/.well-known/jmap" &>/dev/null; then
    echo "==> imap-jmap server is already running on port ${PORT}"
else
    echo "==> Starting imap-jmap server on port ${PORT}..."
    go run main.go -port "${PORT}" &
    SERVER_PID=$!
    sleep 2
fi

# 2. Stop existing Bulwark container if running
${CONTAINER_CMD} rm -f bulwark-webmail &>/dev/null || true

# 3. Launch Bulwark container
echo "==> Starting Bulwark Webmail container on port ${BULWARK_PORT}..."
${CONTAINER_CMD} run -d \
    --name bulwark-webmail \
    --network host \
    -e JMAP_SERVER_URL="http://127.0.0.1:${PORT}" \
    -e ALLOW_CUSTOM_JMAP_ENDPOINT=true \
    -e STALWART_EXTENSIONS=false \
    -e HOSTNAME=0.0.0.0 \
    -e PORT="${BULWARK_PORT}" \
    ghcr.io/bulwarkmail/webmail:latest

echo "==> Waiting for Bulwark Webmail container to be ready..."
sleep 3

# 4. Verify endpoints
echo "==> Verifying JMAP Discovery endpoint..."
DISCOVERY_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -u testuser:testuser "http://127.0.0.1:${PORT}/.well-known/jmap")
if [ "${DISCOVERY_STATUS}" -eq 200 ]; then
    echo "✅ JMAP Session Discovery: HTTP 200 OK"
else
    echo "⚠️ JMAP Session Discovery returned HTTP ${DISCOVERY_STATUS}"
fi

echo "==> Verifying Bulwark Webmail UI..."
BULWARK_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${BULWARK_PORT}/")
if [ "${BULWARK_STATUS}" -eq 307 ] || [ "${BULWARK_STATUS}" -eq 200 ]; then
    echo "✅ Bulwark Webmail UI: HTTP ${BULWARK_STATUS}"
else
    echo "⚠️ Bulwark Webmail UI returned HTTP ${BULWARK_STATUS}"
fi

echo ""
echo "========================================================"
echo "🎉 Bulwark Webmail Test Setup Ready!"
echo "========================================================"
echo " Bulwark Webmail UI: http://localhost:${BULWARK_PORT}"
echo " JMAP Server URL:    http://127.0.0.1:${PORT}"
echo ""
echo " Test Credentials:"
echo "   Username: testuser"
echo "   Password: testuser"
echo ""
echo "   Username: user@example.com"
echo "   Password: user@example.com"
echo "========================================================"
