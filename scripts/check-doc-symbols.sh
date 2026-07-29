#!/usr/bin/env bash
# Cross-check the Go identifiers cited in a Markdown doc against the code.
#
#   ./scripts/check-doc-symbols.sh [DOC] [REF]
#
#     DOC   markdown file to check   (default: CLAUDE.md)
#     REF   git ref to check against (default: origin/main)
#
# Why the REF argument exists, and why it defaults to origin/main rather than
# the working tree: CLAUDE.md documents the code as it will be AFTER merge. Run
# against a checked-out feature branch that is behind main, every symbol added
# since the branch point reports as missing — all false positives. That is
# exactly how this check misled once already. The doc's target is the merge
# base, so compare against the ref, never against whatever happens to be on
# disk.
#
# Exit 1 if any cited symbol is absent from REF, so this can gate CI.
set -euo pipefail

DOC="${1:-CLAUDE.md}"
REF="${2:-origin/main}"

[ -f "$DOC" ] || { echo "no such doc: $DOC" >&2; exit 2; }
git rev-parse --verify --quiet "$REF" >/dev/null \
  || { echo "no such ref: $REF (try 'git fetch origin')" >&2; exit 2; }

# Warn when the working tree and REF have diverged — the situation that caused
# the original false positives. Informational only; the check itself uses REF.
behind=$(git rev-list --count "HEAD..$REF" 2>/dev/null || echo 0)
if [ "${behind:-0}" -gt 0 ]; then
  echo "note: HEAD is $behind commit(s) behind $REF — checking against $REF, not the working tree."
  echo
fi

# Extract backticked identifiers that look like Go functions/methods: start
# lowercase, contain at least one uppercase (camelCase). That deliberately
# excludes things that are NOT Go symbols and would otherwise be false
# positives:
#   snake_case  -> terraform resource/attribute names (monad_pipeline, secrets_hash)
#                  and struct tags (omitempty), TF CLI config (dev_overrides)
#   Capitalised -> exported SDK types, prose, proper nouns
symbols=$(grep -oE '`[a-z][A-Za-z0-9_]*`' "$DOC" \
  | tr -d '`' \
  | grep -E '[A-Z]' \
  | sort -u)

[ -n "$symbols" ] || { echo "no camelCase Go symbols cited in $DOC"; exit 0; }

# Intentional historical references. A doc legitimately names symbols that no
# longer exist — "deleted in #7, do not reintroduce", "removed in #11, here is
# what replaced it". Those are the doc doing its job, not rot, but a
# presence-only check cannot tell them apart. So they must be declared, in the
# doc itself, with a marker like:
#
#   <!-- doc-symbols:ignore connectorConfigToTF reconcilePipelineEnabled -->
#
# Declaring it in the doc rather than in this script keeps the exemption next to
# the prose that justifies it, and makes removing a stale exemption part of
# editing the text.
ignored=$(grep -oE '<!--[[:space:]]*doc-symbols:ignore[^>]*-->' "$DOC" 2>/dev/null \
  | sed -E 's/<!--[[:space:]]*doc-symbols:ignore//; s/-->//' \
  | tr -s ' \t' '\n' | grep -E '^[A-Za-z]' | sort -u || true)

total=0; missing=0
while IFS= read -r sym; do
  if [ -n "$ignored" ] && printf '%s\n' "$ignored" | grep -qx "$sym"; then
    echo "ignored    $sym — declared an intentional historical reference"
    continue
  fi
  total=$((total + 1))
  # -w: whole word, so `reconcileDynamic` doesn't match `reconcileDynamicFoo`.
  # Search .go excluding tests: a doc telling an agent to call a helper that
  # only exists in _test.go is still a broken instruction.
  if git grep -qw "$sym" "$REF" -- '*.go' ':!*_test.go' 2>/dev/null; then
    continue
  fi
  # Fall back to including tests, to give a more useful message.
  if git grep -qw "$sym" "$REF" -- '*.go' 2>/dev/null; then
    echo "TEST-ONLY  $sym — exists only in _test.go on $REF; not callable from provider code"
  else
    echo "MISSING    $sym — cited in $DOC, absent from $REF"
  fi
  missing=$((missing + 1))
done <<< "$symbols"

echo
if [ "$missing" -eq 0 ]; then
  echo "OK: all $total cited Go symbols exist on $REF"
  exit 0
fi
echo "FAIL: $missing of $total cited Go symbols are stale against $REF"
echo "Either update $DOC, or the symbol was removed and the doc still points at it."
exit 1
