#!/usr/bin/env bash
# Runs every time the codespace starts: the simulator and API come up on port 8080 by themselves, detached from
# this script, with the log in /tmp/mq-api.log. `make dev` in a terminal still works (stop this one first:
# pkill -f 'go-build.*api' ; or just use the running one).
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
LOG=/tmp/mq-api.log

if curl -fsS -o /dev/null http://localhost:8080/healthz 2>/dev/null; then
  echo "API already answers on :8080"; exit 0
fi
echo "starting the API (log: $LOG)"
setsid nohup make dev >"$LOG" 2>&1 < /dev/null &
pid=$!

# the first start compiles the server: wait for it, but never block the codespace for long
for _ in $(seq 1 90); do
  if curl -fsS -o /dev/null http://localhost:8080/healthz 2>/dev/null; then
    echo "API is up: open the Ports tab, port 8080"; exit 0
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "make dev stopped; the end of $LOG:" >&2
    tail -n 30 "$LOG" >&2
    exit 0
  fi
  sleep 2
done
echo "API did not answer within 3 minutes; see $LOG" >&2
tail -n 30 "$LOG" >&2
exit 0
