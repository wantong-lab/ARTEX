#!/usr/bin/env bash
# C2 入站中继 Windows 真机 E2E 包装脚本（环境变量见 README）。
set -euo pipefail
cd "$(dirname "$0")/../.."

need=(
  ARTEX_PG_DSN
  ARTEX_C2_INGRESS_PLATFORM_HOST
  ARTEX_C2_INGRESS_PLATFORM_PORT
  ARTEX_C2_INGRESS_VPS_SSH
  ARTEX_C2_INGRESS_VPS_KEY
  ARTEX_C2_INGRESS_VPS_PUBLIC
  ARTEX_C2_INGRESS_VPS_PORT
  ARTEX_C2_INGRESS_WIN_SSH
  ARTEX_C2_INGRESS_WIN_KEY
  ARTEX_CHISEL_PATH
  ARTEX_CHISEL_CLIENT_DIR
  ARTEX_TUNNEL_STAGE_PORT
)
missing=()
for k in "${need[@]}"; do
  [[ -n "${!k:-}" ]] || missing+=("$k")
done
if (( ${#missing[@]} )); then
  echo "缺少环境变量: ${missing[*]}" >&2
  exit 2
fi

exec go test ./server -run TestC2IngressWindowsE2E -count=1 -v -timeout 420s
