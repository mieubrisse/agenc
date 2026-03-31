#!/usr/bin/env bash
set -euo pipefail
script_dirpath="$(cd "$(dirname "${0}")" && pwd)"

repo_dirpath="$(cd "${script_dirpath}/.." && pwd)"
agenc_test="${repo_dirpath}/_build/agenc-test"

passed=0
failed=0
total=0

run_test() {
    local test_name="${1}"
    shift
    local expected_exit="${1}"
    shift

    total=$((total + 1))
    printf "  %-50s " "${test_name}..."

    local output
    local actual_exit=0
    output=$("$@" 2>&1) || actual_exit=$?

    if [ "${actual_exit}" -ne "${expected_exit}" ]; then
        echo "FAIL (exit ${actual_exit}, expected ${expected_exit})"
        if [ -n "${output}" ]; then
            echo "    Output: ${output}" | head -5
        fi
        failed=$((failed + 1))
        return
    fi

    echo "PASS"
    passed=$((passed + 1))
}

run_test_output_contains() {
    local test_name="${1}"
    shift
    local expected_pattern="${1}"
    shift

    total=$((total + 1))
    printf "  %-50s " "${test_name}..."

    local output
    local actual_exit=0
    output=$("$@" 2>&1) || actual_exit=$?

    if [ "${actual_exit}" -ne 0 ]; then
        echo "FAIL (exit ${actual_exit}, expected 0)"
        if [ -n "${output}" ]; then
            echo "    Output: ${output}" | head -5
        fi
        failed=$((failed + 1))
        return
    fi

    if ! echo "${output}" | grep -qE "${expected_pattern}"; then
        echo "FAIL (output missing pattern: ${expected_pattern})"
        echo "    Output: ${output}" | head -5
        failed=$((failed + 1))
        return
    fi

    echo "PASS"
    passed=$((passed + 1))
}

# Run a command and accept any exit code <= 1 (i.e. it did not crash/segfault).
# Useful for commands that require a server but should not panic without one.
run_test_no_crash() {
    local test_name="${1}"
    shift

    total=$((total + 1))
    printf "  %-50s " "${test_name}..."

    local output
    local actual_exit=0
    output=$("$@" 2>&1) || actual_exit=$?

    if [ "${actual_exit}" -le 1 ]; then
        echo "PASS (exit ${actual_exit})"
        passed=$((passed + 1))
    else
        echo "FAIL (exit ${actual_exit})"
        if [ -n "${output}" ]; then
            echo "    Output: ${output}" | head -5
        fi
        failed=$((failed + 1))
    fi
}

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------
if [ ! -x "${agenc_test}" ]; then
    echo "ERROR: ${agenc_test} not found or not executable."
    echo "Run 'make bin' first."
    exit 1
fi

if [ ! -d "${repo_dirpath}/_test-env" ]; then
    echo "ERROR: _test-env/ directory not found."
    echo "Run 'make test-env' first."
    exit 1
fi

echo "Running E2E tests against ${agenc_test}"
echo "Test environment: ${repo_dirpath}/_test-env"
echo ""

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

echo "--- Basic CLI ---"
run_test_output_contains "version prints a version string" \
    "agenc version " \
    "${agenc_test}" version

run_test "help exits successfully" \
    0 \
    "${agenc_test}" --help

run_test "unknown command exits non-zero" \
    1 \
    "${agenc_test}" this-command-does-not-exist

echo ""
echo "--- Repo commands (requires server) ---"
# repo ls needs a running server; verify it doesn't panic or segfault.
run_test_no_crash "repo ls does not crash" \
    "${agenc_test}" repo ls

echo ""
echo "--- Config commands ---"
run_test "config --help succeeds" \
    0 \
    "${agenc_test}" config --help

run_test_output_contains "config get returns a value or unset" \
    "(unset|.+)" \
    "${agenc_test}" config get defaultModel

run_test "config sleep --help succeeds" \
    0 \
    "${agenc_test}" config sleep --help

echo ""
echo "--- Sleep mode (requires server) ---"
run_test_output_contains "sleep ls shows empty initially" \
    "No sleep windows configured" \
    "${agenc_test}" config sleep ls

run_test "sleep add creates a window" \
    0 \
    "${agenc_test}" config sleep add --days mon,tue --start 22:00 --end 06:00

run_test_output_contains "sleep ls shows the added window" \
    "mon,tue 22:00" \
    "${agenc_test}" config sleep ls

run_test "sleep rm removes the window" \
    0 \
    "${agenc_test}" config sleep rm 0

run_test_output_contains "sleep ls is empty after rm" \
    "No sleep windows configured" \
    "${agenc_test}" config sleep ls

run_test "sleep add rejects invalid day" \
    1 \
    "${agenc_test}" config sleep add --days monday --start 22:00 --end 06:00

echo ""
echo "--- Prime ---"
run_test_output_contains "prime outputs quick reference" \
    "(agenc|AgenC|usage|Usage|command|Command)" \
    "${agenc_test}" prime

echo ""
echo "--- Mission commands (requires server) ---"
run_test_no_crash "mission ls does not crash" \
    "${agenc_test}" mission ls

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=========================================="
echo "  E2E Results: ${passed}/${total} passed, ${failed} failed"
echo "=========================================="

if [ "${failed}" -gt 0 ]; then
    exit 1
fi
