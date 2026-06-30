#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="${1:-all}"

sync_web() {
  rm -rf "$ROOT/server/internal/staticassets/web/"*
  cp -r "$ROOT/web/dist/." "$ROOT/server/internal/staticassets/web/"
}

sync_docs() {
  rm -rf "$ROOT/server/internal/staticassets/docs/"*
  cp -r "$ROOT/packages/docs/dist/." "$ROOT/server/internal/staticassets/docs/"
}

case "$TARGET" in
  all)
    sync_web
    sync_docs
    ;;
  web)
    sync_web
    ;;
  docs)
    sync_docs
    ;;
  *)
    echo "usage: $0 [all|web|docs]" >&2
    exit 2
    ;;
esac
