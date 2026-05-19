#!/usr/bin/env bash
# agentic_describe_challenge.sh
#
# Round-260 paired-mutation deep-doc challenge for digital.vasic.agentic.
#
# Validates that:
#   1. The deep-doc ledger (docs/test-coverage.md) lists every exported
#      symbol from the agentic package.
#   2. The multi-locale fixture (tests/fixtures/agentic/payloads.json)
#      parses and contains at least 3 locales.
#   3. The multi-locale runner (challenges/runner/main.go) builds and
#      runs, byte-preserving non-ASCII workflow content through a real
#      3-node DAG, conditional branching, ShouldEnd short-circuit,
#      retry-with-backoff, checkpoint/restore round-trip, and the
#      nil-handler sentinel.
#   4. The README enumerates the agentic package surface and the
#      round-260 anti-bluff guarantees section.
#
# Paired-mutation invariant (CONST-035 + CONST-050(B)):
#   With --anti-bluff-mutate the script plants a deliberate symbol-rename
#   mutation in the ledger (in a tmp copy), reruns validation, and asserts
#   the gate FAILS with exit 99. This proves the gate actually catches
#   ledger-vs-source drift instead of rubber-stamping it.
#
# Verbatim 2026-05-19 operator mandate: "all existing tests and Challenges
# do work in anti-bluff manner - they MUST confirm that all tested codebase
# really works as expected! We had been in position that all tests do execute
# with success and all Challenges as well, but in reality the most of the
# features does not work and can't be used! This MUST NOT be the case and
# execution of tests and Challenges MUST guarantee the quality, the
# completition and full usability by end users of the product!"
#
# Exit codes:
#   0  — gate PASS on clean tree
#   1  — gate FAIL on clean tree (real failure to fix)
#   99 — paired-mutation correctly detected (good — proves anti-bluff)
#   2  — usage / environment error

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

MUTATE=0
for arg in "$@"; do
    case "$arg" in
        --anti-bluff-mutate) MUTATE=1 ;;
        --help|-h)
            sed -n '1,32p' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: $arg" >&2
            exit 2
            ;;
    esac
done

PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }

LEDGER="${MODULE_DIR}/docs/test-coverage.md"
FIXTURE="${MODULE_DIR}/tests/fixtures/agentic/payloads.json"
RUNNER="${MODULE_DIR}/challenges/runner/main.go"
README="${MODULE_DIR}/README.md"

LEDGER_WORK="${LEDGER}"
TMP_LEDGER=""
if [ "${MUTATE}" -eq 1 ]; then
    TMP_LEDGER="$(mktemp)"
    cp "${LEDGER}" "${TMP_LEDGER}"
    # Plant a rename: ErrNodeHandlerNotConfigured -> ErrBogus_MUTATED
    sed -i 's/ErrNodeHandlerNotConfigured/ErrBogus_MUTATED/g' "${TMP_LEDGER}"
    LEDGER_WORK="${TMP_LEDGER}"
    echo "=== Agentic Describe Challenge (anti-bluff-mutate mode) ==="
else
    echo "=== Agentic Describe Challenge (clean mode) ==="
fi
echo ""

# Section 1: ledger presence and freshness
echo "Section 1: docs/test-coverage.md ledger"
if [ ! -f "${LEDGER_WORK}" ]; then
    fail "ledger missing at ${LEDGER_WORK}"
else
    pass "ledger present"
    if grep -q "round-260" "${LEDGER_WORK}"; then
        pass "ledger marked round-260"
    else
        fail "ledger missing round-260 marker"
    fi
    if grep -q "execution of tests and Challenges MUST guarantee" "${LEDGER_WORK}"; then
        pass "ledger carries Article XI §11.9 mandate"
    else
        fail "ledger missing Article XI §11.9 mandate"
    fi
fi

# Section 2: every exported pkg symbol appears in ledger
echo ""
echo "Section 2: exported symbols cross-reference"

extract_symbols() {
    local pkg_dir="$1"
    local files
    files=$(find "${pkg_dir}" -maxdepth 1 -type f -name '*.go' \
        ! -name '*_test.go')
    [ -z "${files}" ] && return 0
    # shellcheck disable=SC2086
    grep -hE '^(func ([A-Z][A-Za-z0-9_]*\()|func \([^)]+\) ([A-Z][A-Za-z0-9_]*\()|type [A-Z][A-Za-z0-9_]* |var Err[A-Z][A-Za-z0-9_]*)' \
        ${files} 2>/dev/null \
        | sed -E 's/^func \([^)]+\) ([A-Z][A-Za-z0-9_]*)\(.*$/\1/; s/^func ([A-Z][A-Za-z0-9_]*)\(.*$/\1/; s/^type ([A-Z][A-Za-z0-9_]*).*$/\1/; s/^var (Err[A-Z][A-Za-z0-9_]*).*$/\1/' \
        | sort -u
}

CHECKED=0
MISSING=0
PKG_DIR="${MODULE_DIR}/agentic"
if [ ! -d "${PKG_DIR}" ]; then
    fail "agentic/ missing — cannot cross-reference"
else
    while IFS= read -r sym; do
        [ -z "${sym}" ] && continue
        CHECKED=$((CHECKED + 1))
        if grep -qE "\\b${sym}\\b" "${LEDGER_WORK}"; then
            : # symbol cross-referenced
        else
            fail "ledger missing symbol agentic.${sym}"
            MISSING=$((MISSING + 1))
        fi
    done < <(extract_symbols "${PKG_DIR}")
fi
if [ "${CHECKED}" -gt 0 ] && [ "${MISSING}" -eq 0 ]; then
    pass "all ${CHECKED} exported symbols cross-referenced in ledger"
fi

# Section 3: multi-locale fixture sanity
echo ""
echo "Section 3: multi-locale fixture"
if [ ! -f "${FIXTURE}" ]; then
    fail "fixture missing at ${FIXTURE}"
else
    pass "fixture present"
    LOCALE_COUNT=$(grep -oE '"locale":\s*"[^"]+"' "${FIXTURE}" | sort -u | wc -l)
    if [ "${LOCALE_COUNT}" -ge 3 ]; then
        pass "fixture covers ${LOCALE_COUNT} locales (>=3)"
    else
        fail "fixture covers only ${LOCALE_COUNT} locales (<3)"
    fi
fi

# Section 4: runner builds + runs every section
echo ""
echo "Section 4: multi-locale runner build + run (real workflow engine)"
if [ ! -f "${RUNNER}" ]; then
    fail "runner missing at ${RUNNER}"
else
    pass "runner source present"
    cd "${MODULE_DIR}"
    if go build -o /tmp/agentic_round260_runner ./challenges/runner/ 2>/tmp/agentic_build.log; then
        pass "runner builds"
        if /tmp/agentic_round260_runner -fixtures "${FIXTURE}" > /tmp/agentic_run.log 2>&1; then
            pass "runner exit 0 across every section + locale"
            if grep -q "PASS: \[section1\]\[sr\]" /tmp/agentic_run.log; then
                pass "section1 Cyrillic (sr) DAG round-trip"
            else
                fail "section1 Cyrillic (sr) missing from runner output"
            fi
            if grep -q "PASS: \[section1\]\[ja\]" /tmp/agentic_run.log; then
                pass "section1 Japanese (ja) DAG round-trip"
            else
                fail "section1 Japanese (ja) missing from runner output"
            fi
            if grep -q "PASS: \[section1\]\[ar\]" /tmp/agentic_run.log; then
                pass "section1 Arabic (ar) DAG round-trip"
            else
                fail "section1 Arabic (ar) missing from runner output"
            fi
            if grep -q "PASS: \[section1\]\[zh-CN\]" /tmp/agentic_run.log; then
                pass "section1 Han (zh-CN) DAG round-trip"
            else
                fail "section1 Han (zh-CN) missing from runner output"
            fi
            if grep -q "PASS: \[section2\]" /tmp/agentic_run.log; then
                pass "section2 conditional branching exercised"
            else
                fail "section2 conditional branching missing"
            fi
            if grep -q "PASS: \[section3\] ShouldEnd short-circuited" /tmp/agentic_run.log; then
                pass "section3 ShouldEnd short-circuit enforced"
            else
                fail "section3 ShouldEnd short-circuit missing"
            fi
            if grep -q "PASS: \[section4\] retry succeeded on attempt 4" /tmp/agentic_run.log; then
                pass "section4 RetryPolicy exponential backoff timed"
            else
                fail "section4 RetryPolicy section missing"
            fi
            if grep -q "PASS: \[section5\] .* checkpoints created" /tmp/agentic_run.log; then
                pass "section5 checkpoint creation observed"
            else
                fail "section5 checkpoint creation missing"
            fi
            if grep -q "PASS: \[section5\] RestoreFromCheckpoint reset" /tmp/agentic_run.log; then
                pass "section5 RestoreFromCheckpoint round-trip enforced"
            else
                fail "section5 RestoreFromCheckpoint round-trip missing"
            fi
            if grep -q "PASS: \[section6\] nil-handler surfaced as wrapped sentinel" /tmp/agentic_run.log; then
                pass "section6 ErrNodeHandlerNotConfigured sentinel enforced"
            else
                fail "section6 nil-handler sentinel missing"
            fi
        else
            fail "runner exit non-zero — see /tmp/agentic_run.log"
            sed -n '1,40p' /tmp/agentic_run.log
        fi
    else
        fail "runner build failed — see /tmp/agentic_build.log"
        sed -n '1,40p' /tmp/agentic_build.log
    fi
    rm -f /tmp/agentic_round260_runner
fi

# Section 5: README round-260 anti-bluff section
echo ""
echo "Section 5: README round-260 anti-bluff section"
if grep -q "Anti-bluff guarantees" "${README}"; then
    pass "README declares Anti-bluff guarantees"
else
    fail "README missing Anti-bluff guarantees section"
fi
if grep -q "round-260" "${README}"; then
    pass "README marked round-260"
else
    fail "README missing round-260 marker"
fi

# Cleanup mutated ledger if any
if [ -n "${TMP_LEDGER}" ]; then
    rm -f "${TMP_LEDGER}"
fi

echo ""
echo "=== Summary: ${PASS}/${TOTAL} PASS, ${FAIL} FAIL ==="

if [ "${MUTATE}" -eq 1 ]; then
    if [ "${FAIL}" -gt 0 ]; then
        echo "anti-bluff-mutate: gate correctly detected planted mutation (exit 99)"
        exit 99
    else
        echo "anti-bluff-mutate: gate FAILED to detect planted mutation — bluff!"
        exit 1
    fi
fi

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
exit 0
