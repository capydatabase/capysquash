#!/usr/bin/env bash
# Refresh test-fixtures/capydb_backend/original from a CapyDB backend
# checkout (default ../backend): the migrations committed on its HEAD, not
# its working tree. Update the commit named in the fixture's README after.
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend="${1:-$project_root/../backend}"
target="$project_root/test-fixtures/capydb_backend/original"

commit="$(git -C "$backend" rev-parse --short HEAD)"
rm -f "$target"/*.sql
git -C "$backend" ls-tree --name-only HEAD internal/store/migrations/ | grep '\.sql$' |
  while read -r path; do
    git -C "$backend" show "HEAD:$path" >"$target/$(basename "$path")"
  done
echo "Copied $(ls "$target"/*.sql | wc -l | tr -d ' ') migrations from backend $commit"
