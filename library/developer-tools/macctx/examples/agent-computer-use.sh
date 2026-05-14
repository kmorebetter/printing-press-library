#!/usr/bin/env bash
set -euo pipefail
BIN=${BIN:-macctx-pp-cli}
OUT=${OUT:-/tmp/macctx-agent-demo}
mkdir -p "$OUT"

$BIN doctor --json > "$OUT/doctor.json"
$BIN dump --json --screenshot --see --path "$OUT/frontmost.png" > "$OUT/context.json"
$BIN act hotkey --keys cmd,s > "$OUT/proposed-save.txt"

cat <<EOF
Wrote demo context to $OUT
- doctor.json
- context.json
- frontmost.png
- proposed-save.txt

The proposed action is dry-run. Review it before adding --execute.
EOF
