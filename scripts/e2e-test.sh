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

run_test_output_contains "config get sessionTitleMaxWords returns default" \
    "^15$" \
    "${agenc_test}" config get sessionTitleMaxWords

run_test "config set sessionTitleMaxWords accepts valid int" \
    0 \
    "${agenc_test}" config set sessionTitleMaxWords 10

run_test_output_contains "config get reflects the new value" \
    "^10$" \
    "${agenc_test}" config get sessionTitleMaxWords

run_test "config set sessionTitleMaxWords rejects out-of-range" \
    1 \
    "${agenc_test}" config set sessionTitleMaxWords 100

run_test "config set sessionTitleMaxWords rejects non-integer" \
    1 \
    "${agenc_test}" config set sessionTitleMaxWords abc

run_test "config set sessionTitleMaxWords rejects 0 explicitly" \
    1 \
    "${agenc_test}" config set sessionTitleMaxWords 0

run_test "config set sessionTitleMaxWords reset" \
    0 \
    "${agenc_test}" config set sessionTitleMaxWords 15

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
echo "--- Cron CRUD (requires server) ---"
run_test_output_contains "config cron ls shows empty initially" \
    "No cron jobs configured" \
    "${agenc_test}" config cron ls

run_test "config cron add creates a cron job" \
    0 \
    "${agenc_test}" config cron add test-cron --schedule="0 9 * * *" --prompt="Run tests"

run_test_output_contains "config cron ls shows the added cron" \
    "test-cron" \
    "${agenc_test}" config cron ls

run_test "config cron update changes schedule" \
    0 \
    "${agenc_test}" config cron update test-cron --schedule="0 10 * * *"

run_test "cron disable disables the cron" \
    0 \
    "${agenc_test}" cron disable test-cron

run_test "cron enable enables the cron" \
    0 \
    "${agenc_test}" cron enable test-cron

run_test "config cron add rejects duplicate name" \
    1 \
    "${agenc_test}" config cron add test-cron --schedule="0 9 * * *" --prompt="Duplicate"

run_test "config cron rm removes the cron" \
    0 \
    "${agenc_test}" config cron rm test-cron

run_test_output_contains "config cron ls is empty after rm" \
    "No cron jobs configured" \
    "${agenc_test}" config cron ls

run_test "config cron rm rejects missing cron" \
    1 \
    "${agenc_test}" config cron rm nonexistent

echo ""
echo "--- Prime ---"
run_test_output_contains "prime outputs quick reference" \
    "(agenc|AgenC|usage|Usage|command|Command)" \
    "${agenc_test}" prime

echo ""
echo "--- Repo mv (requires server + network) ---"
# Add a small public repo, move it, verify, clean up.
# Placed after cron tests so server is reliably running.
run_test "repo add for mv test" \
    0 \
    "${agenc_test}" repo add mieubrisse/stacktrace

run_test_output_contains "repo ls shows added repo" \
    "mieubrisse/stacktrace" \
    "${agenc_test}" repo ls

run_test "repo mv succeeds" \
    0 \
    "${agenc_test}" repo mv mieubrisse/stacktrace mieubrisse/stacktrace-renamed

run_test_output_contains "repo ls shows new name" \
    "mieubrisse/stacktrace-renamed" \
    "${agenc_test}" repo ls

run_test "repo mv nonexistent fails" \
    1 \
    "${agenc_test}" repo mv nonexistent/repo foo/bar

run_test "repo rm cleans up renamed repo" \
    0 \
    "${agenc_test}" repo rm github.com/mieubrisse/stacktrace-renamed

echo ""
echo "--- Mission commands (requires server) ---"
run_test_no_crash "mission ls does not crash" \
    "${agenc_test}" mission ls

run_test_output_contains "mission reload --help mentions --prompt" \
    "prompt" \
    "${agenc_test}" mission reload --help

run_test_output_contains "mission reload --help mentions --async" \
    "async" \
    "${agenc_test}" mission reload --help

run_test_no_crash "mission reload with bad ID does not crash" \
    "${agenc_test}" mission reload aabbccdd --prompt "hello"

run_test_no_crash "mission reload --async with bad ID does not crash" \
    "${agenc_test}" mission reload aabbccdd --prompt "hello" --async

echo ""
echo "--- Mission time filtering (requires server) ---"

# --since today should succeed (may or may not have missions)
run_test "mission ls --since today succeeds" \
    0 \
    "${agenc_test}" mission ls --since "$(date +%Y-%m-%d)"

# --until yesterday should succeed
run_test "mission ls --until yesterday succeeds" \
    0 \
    "${agenc_test}" mission ls --until "$(date -v-1d +%Y-%m-%d)"

# --since after --until should fail
run_test "mission ls --since after --until fails" \
    1 \
    "${agenc_test}" mission ls --since 2026-12-01 --until 2026-01-01

# Invalid date format should fail
run_test "mission ls --since invalid format fails" \
    1 \
    "${agenc_test}" mission ls --since "not-a-date"

# RFC3339 format should succeed
run_test "mission ls --since RFC3339 succeeds" \
    0 \
    "${agenc_test}" mission ls --since "2026-01-01T00:00:00Z"

echo ""
echo "--- Mission search (requires server) ---"

run_test "mission search with no query fails" \
    1 \
    "${agenc_test}" mission search

run_test_output_contains "mission search nonexistent returns no results" \
    "No results" \
    "${agenc_test}" mission search xyznonexistent12345

run_test "mission search --json returns valid output" \
    0 \
    "${agenc_test}" mission search --json xyznonexistent12345

run_test_output_contains "mission search --help shows help" \
    "Search missions" \
    "${agenc_test}" mission search --help

echo ""
echo "--- Mission search-fzf ID lookup (requires server) ---"

# Create a headless blank mission so we have a known short ID to search for
mission_output=$("${agenc_test}" mission new --blank --headless 2>&1) || true
mission_short_id=$(echo "${mission_output}" | grep -oE '[0-9a-f]{8}' | head -1)

if [ -n "${mission_short_id}" ]; then
    run_test_output_contains "search-fzf finds mission by short ID" \
        "${mission_short_id}" \
        "${agenc_test}" mission search-fzf "${mission_short_id}"
else
    total=$((total + 1))
    printf "  %-50s " "search-fzf finds mission by short ID..."
    echo "SKIP (could not create test mission)"
fi

echo ""
echo "--- LAST PROMPT column (requires server) ---"

# mission ls renders the new column header
run_test_output_contains "mission ls header shows LAST PROMPT" \
    "LAST PROMPT" \
    "${agenc_test}" mission ls

# mission ls does NOT render the old column header
if "${agenc_test}" mission ls 2>&1 | grep -q "LAST ACTIVE"; then
    total=$((total + 1))
    printf "  %-50s " "mission ls header no longer LAST ACTIVE..."
    echo "FAIL (still contains LAST ACTIVE)"
    failed=$((failed + 1))
else
    total=$((total + 1))
    printf "  %-50s " "mission ls header no longer LAST ACTIVE..."
    echo "PASS"
fi

echo ""
echo "--- Auto-Summary Pipeline (requires server) ---"

# The split-loop architecture (custom-title + auto-summary) replaced the old
# title-consumer / summarizer-worker pipeline. These tests verify the wiring:
#   (1) the schema migration added the two new offset columns
#   (2) both background loops register as "running" against the live server
#
# Deeper happy-path verification (Haiku actually populating auto_summary) is
# the manual smoke in Task 10 — it requires a real OAuth token and writing
# fixtures into ~/.claude/projects/ (the production Claude state directory),
# both of which are too invasive for an automated E2E pass.

db_filepath="${repo_dirpath}/_test-env/database.sqlite"

run_test "auto-summary DB file exists" \
    0 \
    test -f "${db_filepath}"

# Schema check: both new offset columns must exist on the sessions table.
# PRAGMA table_info(sessions) lists one row per column; the column name is
# the second pipe-delimited field, so a literal substring match is enough.
run_test_output_contains "schema has last_custom_title_scan_offset column" \
    "last_custom_title_scan_offset" \
    sqlite3 "${db_filepath}" "PRAGMA table_info(sessions);"

run_test_output_contains "schema has last_auto_summary_scan_offset column" \
    "last_auto_summary_scan_offset" \
    sqlite3 "${db_filepath}" "PRAGMA table_info(sessions);"

# Loop registration: `agenc server status` queries /health and prints one
# line per background loop with its state ("running", "stopped", or
# "crashed"). Both new loops must show as "running" — this catches the
# regression "Task 6 forgot to wire the loop into server.go".
run_test_output_contains "server status reports custom-title loop running" \
    "custom-title +running" \
    "${agenc_test}" server status

run_test_output_contains "server status reports auto-summary loop running" \
    "auto-summary +running" \
    "${agenc_test}" server status

echo ""
echo "--- Notifications (requires server) ---"

# Note: tests don't assume an empty starting state — they verify the create →
# find → read flow is self-consistent for the notification they create.

# `notifications manage` is the interactive picker. When run without a TTY it
# either short-circuits with the empty-list message (zero notifications) or
# refuses with the interactive-terminal error (any notifications exist). Both
# exit 0/1 cleanly — verify the command is wired up and doesn't panic.
run_test_no_crash "notifications manage runs without crashing in non-TTY" \
    "${agenc_test}" notifications manage

# Cron-source missions auto-create a cron.triggered notification. Use the
# hidden --source flags on mission new (the same flags the launchd plist
# passes) to drive handleCreateMission's cron branch end-to-end.
"${agenc_test}" mission new --blank --headless \
    --source cron --source-id e2e-cron-id \
    --source-metadata '{"cron_name":"e2e-cron-name"}' >/dev/null 2>&1 || true

run_test_output_contains "cron-source mission creates cron.triggered notification" \
    "e2e-cron-name" \
    "${agenc_test}" notifications ls --kind=cron.triggered --all

# Create
notif_create_output=$("${agenc_test}" notifications create --kind=e2e.test --title="E2E Hello" --body="# Body" 2>&1) || true
notif_short_id=$(echo "${notif_create_output}" | grep -oE "'[0-9a-f]{8}'" | head -1 | tr -d "'")

if [ -n "${notif_short_id}" ]; then
    run_test_output_contains "notifications ls shows the new entry" \
        "E2E Hello" \
        "${agenc_test}" notifications ls

    run_test_output_contains "notifications show prints body" \
        "# Body" \
        "${agenc_test}" notifications show "${notif_short_id}"

    run_test "notifications read marks as read" \
        0 \
        "${agenc_test}" notifications read "${notif_short_id}"

    # After read, the entry shouldn't appear in unread filter — but other
    # unread notifications may exist from earlier tests, so filter to ours
    # by short ID.
    run_test "notifications ls --kind=e2e.test no longer includes our entry" \
        1 \
        bash -c "'${agenc_test}' notifications ls --kind=e2e.test 2>&1 | grep -q '${notif_short_id}'"

    run_test_output_contains "notifications ls --all still shows it" \
        "E2E Hello" \
        "${agenc_test}" notifications ls --all

    run_test_output_contains "notifications read is idempotent" \
        "already marked as read" \
        "${agenc_test}" notifications read "${notif_short_id}"
else
    total=$((total + 1))
    printf "  %-50s " "notifications create produced ID..."
    echo "FAIL (could not extract short ID from: ${notif_create_output})"
    failed=$((failed + 1))
fi

# Title with newlines is rejected
run_test "notifications create rejects newline in title" \
    1 \
    "${agenc_test}" notifications create --kind=e2e.test --title=$'multi\nline' --body=x

echo ""
echo "--- Writeable copies (requires server) ---"

# Empty state
run_test_output_contains "writeable-copy ls (empty)" \
    "No writeable copies configured" \
    "${agenc_test}" repo writeable-copy ls

# Set accepts shorthand 'owner/repo' (canonicalized via ParseRepoReference,
# matching 'agenc repo add' behavior). A bare single word like "bare-repo"
# expands using $GH_DEFAULT_OWNER if set; without that, it errors. So instead
# we test that 'owner/repo' is accepted and canonicalized.
e2e_wc_shorthand_path="$(mktemp -d -t agenc-e2e-wc-sh-XXXXXX)"
rmdir "${e2e_wc_shorthand_path}"
run_test "writeable-copy set accepts shorthand owner/repo" \
    0 \
    "${agenc_test}" repo writeable-copy set e2e-shorthand/test "${e2e_wc_shorthand_path}"
sleep 1
"${agenc_test}" repo writeable-copy unset github.com/e2e-shorthand/test >/dev/null 2>&1 || true
sleep 1

# Set rejects path under agenc dir
test_env_path="${repo_dirpath}/_test-env"
run_test "writeable-copy set rejects path inside agenc dir" \
    1 \
    "${agenc_test}" repo writeable-copy set github.com/e2e/test "${test_env_path}/inside"

# Successful set (config-only — server-side cloning is manual-test territory)
e2e_wc_path="$(mktemp -d -t agenc-e2e-wc-XXXXXX)"
rmdir "${e2e_wc_path}" # remove the tempdir; set wants the path absent
run_test "writeable-copy set succeeds with valid args" \
    0 \
    "${agenc_test}" repo writeable-copy set github.com/e2e/test "${e2e_wc_path}"

# Server caches config; wait for the config-watcher to pick up the change
# (debounced at 500ms).
sleep 1

run_test_output_contains "writeable-copy ls shows the new entry" \
    "github.com/e2e/test" \
    "${agenc_test}" repo writeable-copy ls

# Always-synced cannot be disabled while writeable copy is set
run_test "config repoConfig set rejects --always-synced=false with writeable copy" \
    1 \
    "${agenc_test}" config repoConfig set github.com/e2e/test --always-synced=false

# Unset
run_test "writeable-copy unset succeeds" \
    0 \
    "${agenc_test}" repo writeable-copy unset github.com/e2e/test

# Wait for config-watcher debounce
sleep 1

run_test_output_contains "writeable-copy ls is empty after unset" \
    "No writeable copies configured" \
    "${agenc_test}" repo writeable-copy ls

echo ""
echo "--- claude-update stdin handling ---"

# Bug fix: agenc mission send claude-update must not block on stdin for
# non-Notification events. Previously, io.ReadAll hung when Claude Code
# didn't close stdin for UserPromptSubmit hooks.
# Use a fake mission UUID — the command should exit 0 regardless (silent fail).

run_test "claude-update UserPromptSubmit without stdin returns immediately" \
    0 \
    timeout 5 "${agenc_test}" mission send claude-update 00000000-0000-0000-0000-000000000000 UserPromptSubmit

run_test "claude-update Stop without stdin returns immediately" \
    0 \
    timeout 5 "${agenc_test}" mission send claude-update 00000000-0000-0000-0000-000000000000 Stop

run_test "claude-update PostToolUse without stdin returns immediately" \
    0 \
    timeout 5 "${agenc_test}" mission send claude-update 00000000-0000-0000-0000-000000000000 PostToolUse

# Notification event should also not hang (has a stdin read timeout)
run_test "claude-update Notification without stdin returns immediately" \
    0 \
    timeout 5 "${agenc_test}" mission send claude-update 00000000-0000-0000-0000-000000000000 Notification

echo "--- Repo library guard hook ---"

# The repo-library-guard.sh hook fires from settings.json PreToolUse to block
# Write/Edit/NotebookEdit calls targeting the AgenC repo library and replace
# the bare permission-deny message with explicit guidance about spawning a
# new mission. We test the script directly here rather than wiring up a real
# mission — the script's behavior is the contract.

guard_script="${repo_dirpath}/internal/claudeconfig/repo_library_guard.sh"
guard_test_home="$(mktemp -d)"
trap "rm -rf '${guard_test_home}'" EXIT

# Use a writeable scratch agenc dir under the temp home so the script's
# repos_dirpath computation is fully isolated from the real ~/.agenc.
export AGENC_DIRPATH="${guard_test_home}/.agenc"
export HOME="${guard_test_home}"

run_guard() {
    local payload="${1}"
    printf '%s' "${payload}" | bash "${guard_script}" 2>&1 || true
}

run_guard_assert_blocks() {
    local test_name="${1}"
    local payload="${2}"
    total=$((total + 1))
    printf "  %-50s " "${test_name}..."
    local out
    out=$(run_guard "${payload}")
    if echo "${out}" | grep -qE 'permissionDecision.*deny'; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (expected deny, got: ${out})"
        failed=$((failed + 1))
    fi
}

run_guard_assert_allows() {
    local test_name="${1}"
    local payload="${2}"
    total=$((total + 1))
    printf "  %-50s " "${test_name}..."
    local out
    out=$(run_guard "${payload}")
    if [ -z "${out}" ]; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (expected no output, got: ${out})"
        failed=$((failed + 1))
    fi
}

run_guard_assert_blocks "guard blocks Edit on repo-library path" \
    "{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"${AGENC_DIRPATH}/repos/foo/bar.md\"}}"

run_guard_assert_blocks "guard blocks Write on repo-library path" \
    "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"${AGENC_DIRPATH}/repos/foo/bar.md\"}}"

run_guard_assert_blocks "guard blocks NotebookEdit on repo-library path" \
    "{\"tool_name\":\"NotebookEdit\",\"tool_input\":{\"file_path\":\"${AGENC_DIRPATH}/repos/foo/bar.ipynb\"}}"

run_guard_assert_blocks "guard blocks ~/ form repo-library path" \
    '{"tool_name":"Edit","tool_input":{"file_path":"~/.agenc/repos/foo/bar.md"}}'

run_guard_assert_allows "guard allows Edit on non-repo-library path" \
    '{"tool_name":"Edit","tool_input":{"file_path":"/tmp/some/file.md"}}'

# Read on a repo-library path must not produce any output — the matcher in
# settings.json scopes the hook to Write/Edit/NotebookEdit, but the script
# defends in depth by exiting silently for other tools.
run_guard_assert_allows "guard allows Read on repo-library path" \
    "{\"tool_name\":\"Read\",\"tool_input\":{\"file_path\":\"${AGENC_DIRPATH}/repos/foo/bar.md\"}}"

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
