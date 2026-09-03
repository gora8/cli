#!/usr/bin/env bash
# Re-copies the source of truth (gora8-commerce/ at the repo root) into
# this directory so `go:embed` (skill.go) can compile it into the
# binary — go:embed can't reach outside its own module via `..`, so a
# physical copy here is unavoidable, not a design choice. Run this
# whenever gora8-commerce/{SKILL.md,REFERENCE.md} changes, before
# building the CLI. Not wired into `go build` or CI automatically yet —
# a stale embedded copy fails safe (an old-but-consistent skill gets
# installed) rather than failing loud, so treat this as a real, manual
# step until it's worth automating.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
cp ../../../../gora8-commerce/SKILL.md ../../../../gora8-commerce/REFERENCE.md .
echo "Synced SKILL.md and REFERENCE.md from gora8-commerce/."
