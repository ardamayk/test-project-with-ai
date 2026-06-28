#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
rm -rf "$ROOT/server/internal/staticassets/web/"*
rm -rf "$ROOT/server/internal/staticassets/docs/"*
cp -r "$ROOT/web/dist/." "$ROOT/server/internal/staticassets/web/"
cp -r "$ROOT/packages/docs/dist/." "$ROOT/server/internal/staticassets/docs/"
