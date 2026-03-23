#!/bin/bash
# Tailscale Serve로 Cluster Manager 실행
# Tailnet 내부에서만 접근 가능 (자동 인증)

set -e

PORT=${PORT:-8080}
BINARY="./build/naga-server"

# 빌드 확인
if [ ! -f "$BINARY" ]; then
    echo "Building server..."
    make build-server
fi

# 서버 시작 (백그라운드)
echo "Starting Cluster Manager on port $PORT..."
$BINARY &
SERVER_PID=$!

# Tailscale Serve 설정
echo "Exposing via Tailscale Serve..."
tailscale serve --bg $PORT

echo ""
echo "✅ Cluster Manager is running!"
echo ""
echo "Access URLs:"
echo "  - Local:     http://localhost:$PORT"
echo "  - Tailscale: https://$(tailscale status --self --json | jq -r '.Self.DNSName' | sed 's/\.$//')/"
echo ""
echo "Only users in your Tailnet can access the Tailscale URL."
echo ""
echo "To stop: kill $SERVER_PID && tailscale serve --remove $PORT"

# 종료 시 정리
trap "kill $SERVER_PID 2>/dev/null; tailscale serve --remove $PORT" EXIT

# 서버 대기
wait $SERVER_PID
