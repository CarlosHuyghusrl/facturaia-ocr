#!/bin/bash
# lint-claims.sh — custom lint: prohibit claims-discard pattern in handlers
# Rationale: W2 P0 cross-tenant leak via discarded JWT claims (Wave 080526)
# If golangci-lint not installed, use this script in CI as fallback.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

hits=$(grep -rn \
  "claims, _ := auth.GetClaimsFromContext\|_, err := auth.GetClaimsFromContext" \
  "$REPO_ROOT/internal/" "$REPO_ROOT/api/" 2>/dev/null \
  | grep -v "_test.go")

if [ -n "$hits" ]; then
  echo "LINT VIOLATION: claims discarded in handler — security risk (cross-tenant leak)"
  echo "$hits"
  exit 1
fi

echo "OK: NO claims-discard violations found"
exit 0
