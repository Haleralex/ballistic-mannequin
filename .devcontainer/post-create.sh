#!/usr/bin/env bash
# Runs once when the codespace (dev container) is created: test dependencies, Chromium for the browser tests,
# Go modules of the server when it is there.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "== tests: npm ci"
(cd tests && npm ci)

echo "== tests: Chromium and its system libraries (playwright-core)"
(cd tests && npx --no-install playwright-core install --with-deps chromium)

if [ -f server/go.mod ]; then
  echo "== server: go mod download"
  (cd server && go mod download)
fi

cat <<'EOF'

Ready. Start the simulator with the API:   make dev
Then open the "Ports" tab: port 8080. To share it with testers, set its visibility to Public (docs/ops.md).
EOF
