#!/usr/bin/env bash
# Run the runbooks in examples/runn with runn against the runnora-diff on PATH.
# Requires runn, runnora-diff, python3 and curl.
set -euo pipefail

cd "$(dirname "$0")/../examples/runn"

port=18080
python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT

for _ in $(seq 50); do
  if curl -fsS -o /dev/null "http://127.0.0.1:$port/actual.json" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

# runn reads stdin when it is not a terminal, so detach it.
runn run --scopes run:exec --verbose compare.yml http.yml </dev/null
