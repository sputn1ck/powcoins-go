#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_ROOT="$(go env GOROOT)"

rm -f "$ROOT/extension/wasm_exec.js"
cp "$GO_ROOT/lib/wasm/wasm_exec.js" "$ROOT/extension/wasm_exec.js"
chmod u+w "$ROOT/extension/wasm_exec.js"
cp "$ROOT/src/shader_unrolled.wgsl" "$ROOT/extension/shader_unrolled.wgsl"

GOOS=js GOARCH=wasm go build -o "$ROOT/extension/faucetpow.wasm" "$ROOT/cmd/faucetpow-wasm"

echo "Built Chrome extension assets in $ROOT/extension"
