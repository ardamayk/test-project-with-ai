#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <task> <package>... [turbo options]" >&2
  exit 2
fi

task_name="$1"
shift

case "$task_name" in
  build|format|format:check|lint|check|typecheck|test:unit) ;;
  *)
    echo "unsupported workspace task: $task_name" >&2
    exit 2
    ;;
esac

package_filters=()
turbo_options=()
for argument in "$@"; do
  if [[ "$argument" == --* ]]; then
    turbo_options+=("$argument")
    continue
  fi
  case "$argument" in
    web|@repo/ui|@repo/api-client) package_filters+=("--filter=$argument") ;;
    *)
      echo "unsupported workspace package: $argument" >&2
      exit 2
      ;;
  esac
done

pnpm turbo run "$task_name" "${package_filters[@]}" "${turbo_options[@]}"
