#!/usr/bin/env bash
# agentic_workflow_challenge.sh - Validates Agentic module functionality
set -euo pipefail
PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "PASS: $1"; }
fail() { FAIL=$((FAIL+1)); echo "FAIL: $1"; }

# Test 1: go.mod exists
[ -f "$(dirname "$0")/../../go.mod" ] && pass "go.mod exists" || fail "go.mod missing"

# Test 2: workflow.go exists
[ -f "$(dirname "$0")/../../agentic/workflow.go" ] && pass "workflow.go exists" || fail "workflow.go missing"

echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
