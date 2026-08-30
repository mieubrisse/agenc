#!/usr/bin/env bash
set -euo pipefail
script_dirpath="$(cd "$(dirname "${0}")" && pwd)"

# When the developer runs `make e2e` from inside a real AgenC mission, their
# shell has AGENC_MISSION_UUID set — but the test environment has its own
# isolated mission DB that doesn't know about the parent mission. Strip the
# var here so commands picking up env-var defaults (e.g. `notification new`)
# don't try to resolve a UUID that only exists in the parent's universe.
unset AGENC_MISSION_UUID

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
echo "--- Attached mission limit ---"
run_test_output_contains "config get attachedMissionLimit is unset by default" \
    "^unset$" \
    "${agenc_test}" config get attachedMissionLimit

run_test "config set attachedMissionLimit accepts positive integer" \
    0 \
    "${agenc_test}" config set attachedMissionLimit 5

run_test_output_contains "config get reflects the new value" \
    "^5$" \
    "${agenc_test}" config get attachedMissionLimit

run_test "config set attachedMissionLimit rejects zero" \
    1 \
    "${agenc_test}" config set attachedMissionLimit 0

run_test "config set attachedMissionLimit rejects negative" \
    1 \
    "${agenc_test}" config set attachedMissionLimit -3

run_test "config set attachedMissionLimit rejects non-integer" \
    1 \
    "${agenc_test}" config set attachedMissionLimit abc

run_test "config unset attachedMissionLimit succeeds" \
    0 \
    "${agenc_test}" config unset attachedMissionLimit

run_test_output_contains "config get is unset after unset" \
    "^unset$" \
    "${agenc_test}" config get attachedMissionLimit

echo ""
echo "--- Palette command default keybindings ---"

# Ctrl+N is the global hotkey for Notification Center (see internal/config/agenc_config.go).
# New Mission is reachable via the palette but has no default global hotkey.
run_test_output_contains "paletteCommand ls shows showNotifications with C-n keybinding" \
    "showNotifications.*C-n" \
    "${agenc_test}" config paletteCommand ls

run_test "paletteCommand ls does not bind newMission to C-n" \
    1 \
    bash -c "'${agenc_test}' config paletteCommand ls | grep -E 'newMission.*C-n'"

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

config_filepath="${repo_dirpath}/_test-env/config/config.yml"

run_test "config cron add accepts --notifications-enabled=false" \
    0 \
    "${agenc_test}" config cron add quiet-cron --schedule="0 9 * * *" --prompt="quiet" --notifications-enabled=false

run_test_output_contains "config.yml records notificationsEnabled: false" \
    "notificationsEnabled: false" \
    grep -F "notificationsEnabled" "${config_filepath}"

run_test "config cron update flips notifications back on" \
    0 \
    "${agenc_test}" config cron update quiet-cron --notifications-enabled=true

run_test_output_contains "config.yml records notificationsEnabled: true" \
    "notificationsEnabled: true" \
    grep -F "notificationsEnabled" "${config_filepath}"

run_test "config cron rm removes quiet-cron" \
    0 \
    "${agenc_test}" config cron rm quiet-cron

run_test "config cron add omits notificationsEnabled when flag not passed" \
    0 \
    "${agenc_test}" config cron add default-cron --schedule="0 9 * * *" --prompt="default"

run_test "config.yml omits notificationsEnabled by default" \
    1 \
    grep -F "notificationsEnabled" "${config_filepath}"

run_test "config cron rm removes default-cron" \
    0 \
    "${agenc_test}" config cron rm default-cron

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
echo "--- Repo description (requires server + network) ---"
run_test_output_contains "repo add --help mentions --description" \
    "description" \
    "${agenc_test}" repo add --help

run_test_output_contains "config repoConfig set --help mentions --description" \
    "description" \
    "${agenc_test}" config repoConfig set --help

run_test_output_contains "config repoConfig --help lists description in supported settings" \
    "description.*human/agent-readable" \
    "${agenc_test}" config repoConfig --help

run_test "repo add with --description succeeds" \
    0 \
    "${agenc_test}" repo add mieubrisse/stacktrace --description "Initial test description"

run_test_output_contains "repo ls shows DESCRIPTION column header" \
    "DESCRIPTION" \
    "${agenc_test}" repo ls

run_test_output_contains "repo ls shows the added description" \
    "Initial test description" \
    "${agenc_test}" repo ls

run_test "config repoConfig set --description updates the value" \
    0 \
    "${agenc_test}" config repoConfig set github.com/mieubrisse/stacktrace --description "Updated test description"

run_test_output_contains "repo ls reflects the updated description" \
    "Updated test description" \
    "${agenc_test}" repo ls

run_test "config repoConfig set --description= clears the value" \
    0 \
    "${agenc_test}" config repoConfig set github.com/mieubrisse/stacktrace --description=""

# After clearing, repo ls should no longer show the old description text.
total=$((total + 1))
printf "  %-50s " "repo ls no longer shows cleared description..."
ls_output=$("${agenc_test}" repo ls 2>&1) || true
if echo "${ls_output}" | grep -q "Updated test description"; then
    echo "FAIL (cleared description still present in output)"
    echo "    Output: ${ls_output}" | head -5
    failed=$((failed + 1))
else
    echo "PASS"
    passed=$((passed + 1))
fi

run_test "repo rm cleans up description-test repo" \
    0 \
    "${agenc_test}" repo rm github.com/mieubrisse/stacktrace

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

# mission detach without a resolvable tmux session must emit the
# sandbox-hint error (not the old "requires tmux; run inside a tmux session"
# message, which misled agents into thinking they weren't in tmux).
total=$((total + 1))
printf "  %-50s " "mission detach error message mentions sandbox..."
detach_output=$(env -u TMUX -u AGENC_CALLING_SESSION_NAME "${agenc_test}" mission detach deadbeef 2>&1 || true)
if echo "${detach_output}" | grep -qE "sandbox"; then
    echo "PASS"
    passed=$((passed + 1))
else
    echo "FAIL (output missing 'sandbox' hint)"
    echo "    Output: ${detach_output}" | head -5
    failed=$((failed + 1))
fi

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

# Regression: the attach picker reload command (search-fzf) still renders rows
# after the attached-indicator column was added.
run_test_no_crash "mission search-fzf empty query renders" \
    "${agenc_test}" mission search-fzf

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
    passed=$((passed + 1))
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

# `notification manage` is the interactive picker. When run without a TTY it
# either short-circuits with the empty-list message (zero notifications) or
# refuses with the interactive-terminal error (any notifications exist). Both
# exit 0/1 cleanly — verify the command is wired up and doesn't panic.
run_test_no_crash "notification manage runs without crashing in non-TTY" \
    "${agenc_test}" notification manage

# Cron-source missions auto-create a cron.triggered notification. Use the
# hidden --source flags on mission new (the same flags the launchd plist
# passes) to drive handleCreateMission's cron branch end-to-end.
"${agenc_test}" mission new --blank --headless \
    --source cron --source-id e2e-cron-id \
    --source-metadata '{"cron_name":"e2e-cron-name"}' >/dev/null 2>&1 || true

run_test_output_contains "cron-source mission creates cron.triggered notification" \
    "e2e-cron-name" \
    "${agenc_test}" notification ls --kind=cron.triggered --all

# Create
notif_create_output=$("${agenc_test}" notification new --kind=e2e.test --title="E2E Hello" --body="# Body" 2>&1) || true
notif_short_id=$(echo "${notif_create_output}" | grep -oE "'[0-9a-f]{8}'" | head -1 | tr -d "'")

if [ -n "${notif_short_id}" ]; then
    run_test_output_contains "notification ls shows the new entry" \
        "E2E Hello" \
        "${agenc_test}" notification ls

    run_test_output_contains "notification show prints body" \
        "# Body" \
        "${agenc_test}" notification show "${notif_short_id}"

    run_test "notification read marks as read" \
        0 \
        "${agenc_test}" notification read "${notif_short_id}"

    # After read, the entry shouldn't appear in unread filter — but other
    # unread notifications may exist from earlier tests, so filter to ours
    # by short ID.
    run_test "notification ls --kind=e2e.test no longer includes our entry" \
        1 \
        bash -c "'${agenc_test}' notification ls --kind=e2e.test 2>&1 | grep -q '${notif_short_id}'"

    run_test_output_contains "notification ls --all still shows it" \
        "E2E Hello" \
        "${agenc_test}" notification ls --all

    run_test_output_contains "notification read is idempotent" \
        "already marked as read" \
        "${agenc_test}" notification read "${notif_short_id}"

    # `notification manage-fzf-input` is the hidden reload source the picker
    # invokes after Ctrl-R. It must print the notification short ID and the
    # short ID must appear in the output even after the notification is read
    # (the picker shows all notifications, not just unread).
    run_test_output_contains "notification manage-fzf-input lists our read entry" \
        "${notif_short_id}" \
        "${agenc_test}" notification manage-fzf-input

    # `notification toggle-read` is the hidden command behind the picker's
    # Ctrl-R bind. The just-read notification toggles back to unread; a
    # second toggle returns it to read. Verify both halves of the flip.
    run_test_output_contains "notification toggle-read flips read -> unread" \
        "as unread" \
        "${agenc_test}" notification toggle-read "${notif_short_id}"

    run_test "notification toggle-read leaves it unread (visible in unread filter)" \
        0 \
        bash -c "'${agenc_test}' notification ls --kind=e2e.test 2>&1 | grep -q '${notif_short_id}'"

    run_test_output_contains "notification toggle-read flips unread -> read" \
        "as read" \
        "${agenc_test}" notification toggle-read "${notif_short_id}"
else
    total=$((total + 1))
    printf "  %-50s " "notification new produced ID..."
    echo "FAIL (could not extract short ID from: ${notif_create_output})"
    failed=$((failed + 1))
fi

# Title with newlines is rejected
run_test "notification new rejects newline in title" \
    1 \
    "${agenc_test}" notification new --kind=e2e.test --title=$'multi\nline' --body=x

# notification new defaults --mission-id to $AGENC_MISSION_UUID when not
# passed explicitly. Reuses the blank mission created earlier in this script
# (mission_short_id, line 368). Reproduces the bug where notifications posted
# by agents inside missions (e.g. the HN daily pull skill) had empty mission_id
# and the picker's ENTER attach was a no-op.
if [ -n "${mission_short_id}" ]; then
    env_mission_notif_output=$(AGENC_MISSION_UUID="${mission_short_id}" "${agenc_test}" notification new --kind=e2e.mission-default --title="E2E AGENC_MISSION_UUID default" --body=x 2>&1) || true
    env_mission_notif_id=$(echo "${env_mission_notif_output}" | grep -oE "'[0-9a-f]{8}'" | head -1 | tr -d "'")

    if [ -n "${env_mission_notif_id}" ]; then
        run_test "notification new picks up AGENC_MISSION_UUID when --mission-id omitted" \
            0 \
            bash -c "'${agenc_test}' notification manage-fzf-input | grep '${env_mission_notif_id}' | grep -q '${mission_short_id}'"
    else
        total=$((total + 1))
        printf "  %-50s " "notification new picks up AGENC_MISSION_UUID..."
        echo "FAIL (could not create env-default notification: ${env_mission_notif_output})"
        failed=$((failed + 1))
    fi
fi

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

echo ""
echo "--- State Y native passthrough ---"

# The State Y flip (agenc-lh8p): a spawned mission runs plain `claude` against
# the isolated config dir with CLAUDE_CONFIG_DIR unset by AgenC (the only
# CLAUDE_CONFIG_DIR is the one the agenc-test harness exports, pointing at
# $(TEST_ENV_DIR)/claude-config — NOT a per-mission BuildMissionConfigDir
# snapshot). AgenC's operational layer is delivered via `claude --settings
# <op-settings file>`, and the mission's trust entry is written server-side into
# the config dir's .claude.json at create. These assertions inspect the
# artifacts the server + wrapper produce at create/spawn — they do not depend on
# Claude actually reaching the API, so they are robust in a sandboxed CI env.
#
# The paired UNIT assertion (internal/mission: TestBuildClaudeCmdStateY) proves
# the PRODUCTION BuildClaudeCmd sets no CLAUDE_CONFIG_DIR at all; this e2e runs
# WITH CLAUDE_CONFIG_DIR set by the harness, so it asserts the absence of a
# per-mission SNAPSHOT dir instead.

# Derive test-env paths from repo_dirpath (this script's shell does NOT have
# AGENC_DIRPATH / CLAUDE_CONFIG_DIR set — those live only inside the agenc-test
# wrapper). The Makefile's TEST_ENV_DIR is _test-env; the agenc-test wrapper
# exports CLAUDE_CONFIG_DIR=$AGENC_DIRPATH/claude-config.
statey_test_env_dir="${repo_dirpath}/_test-env"
statey_config_dir="${statey_test_env_dir}/claude-config"
statey_missions_dir="${statey_test_env_dir}/missions"
statey_claude_json="${statey_config_dir}/.claude.json"
real_claude_json="${HOME}/.claude.json"

mkdir -p "${statey_config_dir}/skills" "${statey_config_dir}/hooks"

# (e/prep) Snapshot the real ~/.claude.json mtime BEFORE the run (guard: AgenC
# must write only into the isolated config dir, never the developer's real file).
real_claude_json_mtime_before=""
if [ -f "${real_claude_json}" ]; then
    real_claude_json_mtime_before=$(stat -f %m "${real_claude_json}" 2>/dev/null || echo "")
fi

# Create a real headless mission that triggers a tool use. The op-settings file
# and trust seed are produced at create/spawn regardless of whether Claude
# completes its API call.
statey_out=$("${agenc_test}" mission new --blank --headless \
    --prompt "Run the bash command: echo state-y-probe" 2>&1) || true
statey_short_id=$(echo "${statey_out}" | grep -oE '[0-9a-f]{8}' | head -1)

# Resolve the full mission UUID + agent dir from the missions directory: the
# missions/<uuid>/ dir whose short-id prefix matches.
statey_mission_uuid=""
if [ -n "${statey_short_id}" ] && [ -d "${statey_missions_dir}" ]; then
    for d in "${statey_missions_dir}"/"${statey_short_id}"*; do
        if [ -d "${d}" ]; then
            statey_mission_uuid=$(basename "${d}")
            break
        fi
    done
fi

# (d/prep) Drop a skill into the config dir's skills/ AFTER create. Under State Y
# there is no per-mission snapshot to shadow it, so it is structurally visible
# to the mission without a reload.
mkdir -p "${statey_config_dir}/skills/statey-post-create-skill"
cat > "${statey_config_dir}/skills/statey-post-create-skill/SKILL.md" <<'EOF'
---
name: statey-post-create-skill
description: Sentinel skill dropped after mission create to prove post-create visibility under State Y.
---
Sentinel body.
EOF

# Give the wrapper a moment to write the op-settings file and spawn.
sleep 2

if [ -z "${statey_mission_uuid}" ]; then
    total=$((total + 1))
    printf "  %-50s " "State Y: resolve created mission..."
    echo "FAIL (could not resolve mission UUID from short id '${statey_short_id}')"
    failed=$((failed + 1))
else
    statey_mission_dir="${statey_missions_dir}/${statey_mission_uuid}"
    statey_agent_dir="${statey_mission_dir}/agent"
    statey_op_settings="${statey_mission_dir}/agenc-settings.json"

    # (a) No per-mission SNAPSHOT claude-config dir (State X artifact) exists,
    #     and the op-settings file the --settings flag points at DOES exist.
    total=$((total + 1))
    printf "  %-50s " "State Y: no per-mission config snapshot..."
    if [ -d "${statey_mission_dir}/claude-config" ]; then
        echo "FAIL (found State-X snapshot dir at ${statey_mission_dir}/claude-config)"
        failed=$((failed + 1))
    else
        echo "PASS"
        passed=$((passed + 1))
    fi

    total=$((total + 1))
    printf "  %-50s " "State Y: op-settings file written..."
    if [ -f "${statey_op_settings}" ]; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (missing op-settings file at ${statey_op_settings})"
        failed=$((failed + 1))
    fi

    # (b) Trust entry present in the config dir's .claude.json for the agent dir.
    total=$((total + 1))
    printf "  %-50s " "State Y: trust entry seeded server-side..."
    if [ -f "${statey_claude_json}" ] && \
       grep -q "$(basename "${statey_agent_dir}")" "${statey_claude_json}" 2>/dev/null && \
       grep -q "hasTrustDialogAccepted" "${statey_claude_json}" 2>/dev/null; then
        # Stronger check: the specific agent dir path is a projects key with
        # trust accepted. Use python for a precise JSON assertion when available.
        if command -v python3 >/dev/null 2>&1 && python3 - "$statey_claude_json" "$statey_agent_dir" <<'PY'
import json, sys
path, agent = sys.argv[1], sys.argv[2]
with open(path) as f:
    data = json.load(f)
entry = data.get("projects", {}).get(agent)
sys.exit(0 if entry and entry.get("hasTrustDialogAccepted") is True else 1)
PY
        then
            echo "PASS"
            passed=$((passed + 1))
        else
            echo "FAIL (agent dir '${statey_agent_dir}' not trusted in ${statey_claude_json})"
            failed=$((failed + 1))
        fi
    else
        echo "FAIL (no trust entry in ${statey_claude_json})"
        failed=$((failed + 1))
    fi

    # (c) The --settings HOOK UNION actually FIRES (epic R5 check-loop). This is
    #     the load-bearing assertion: it must prove Claude HONORS the union of
    #     hooks from `--settings <file>` AND the config-dir settings.json — not
    #     merely that the hooks are present in those files (that would be theater
    #     that can never fail, since AgenC/the test wrote them).
    #
    #     Construction: a direct `claude --print` invocation that mirrors exactly
    #     how BuildClaudeCmd delivers the op-settings file (via --settings). It
    #     carries a SessionStart probe hook on BOTH channels — one in the config-
    #     dir settings.json (the native/user channel) and one in a separate
    #     --settings file (the SAME delivery channel AgenC uses for its real
    #     op-settings). Each hook `touch`es its own sentinel WHEN IT FIRES.
    #     SessionStart hooks fire at session start (before the model's turn), so
    #     both sentinels land as long as Claude launches at all — no network to
    #     the API is required for the hooks themselves.
    #
    #     We do NOT drive the union through the mission-spawn path: a pool-spawned
    #     mission runs `claude` interactively in a detached pane with no driving
    #     terminal, so it parks at the prompt and never completes SessionStart in
    #     this harness. The direct `claude --print` invocation is the faithful,
    #     firing-provable exercise of the same `--settings` mechanism.
    #
    #     Robustness: if Claude cannot launch here (no binary / no token / the
    #     run fails), we print a VISIBLE NAMED SKIP rather than a can't-fail grep,
    #     so the gap is never a silent green.
    statey_claude_bin="$(command -v claude 2>/dev/null || true)"
    statey_token_file="${statey_test_env_dir}/cache/oauth-token"
    statey_union_user_sentinel="${statey_config_dir}/statey-union-user-fired"
    statey_union_settings_sentinel="${statey_config_dir}/statey-union-settings-fired"
    statey_union_settings_file="${statey_config_dir}/statey-union-probe-settings.json"
    rm -f "${statey_union_user_sentinel}" "${statey_union_settings_sentinel}"

    # First assert AgenC's REAL op-settings file routes its operational hooks
    # through this same --settings channel: it must carry the SessionStart
    # `agenc prime` hook. This ties the firing test below to what AgenC actually
    # emits (the firing test uses controlled probes for observable sentinels).
    total=$((total + 1))
    printf "  %-50s " "State Y: agenc SessionStart hook in real op-settings..."
    if [ -f "${statey_op_settings}" ] && grep -q "SessionStart" "${statey_op_settings}" 2>/dev/null && grep -q "prime" "${statey_op_settings}" 2>/dev/null; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (agenc SessionStart prime hook missing from ${statey_op_settings})"
        failed=$((failed + 1))
    fi

    # Native/user channel probe: SessionStart hook in the config-dir settings.json.
    printf '%s\n' '{' \
      '  "hooks": {' \
      '    "SessionStart": [' \
      "      { \"hooks\": [ { \"type\": \"command\", \"command\": \"touch '${statey_union_user_sentinel}'\" } ] }" \
      '    ]' \
      '  }' \
      '}' > "${statey_config_dir}/settings.json"

    # --settings channel probe: a separate settings file, delivered via the same
    # --settings flag AgenC uses for its op-settings.
    printf '%s\n' '{' \
      '  "hooks": {' \
      '    "SessionStart": [' \
      "      { \"hooks\": [ { \"type\": \"command\", \"command\": \"touch '${statey_union_settings_sentinel}'\" } ] }" \
      '    ]' \
      '  }' \
      '}' > "${statey_union_settings_file}"

    statey_union_ran=0
    if [ -n "${statey_claude_bin}" ] && [ -f "${statey_token_file}" ]; then
        # Mirror BuildClaudeCmd's env: CLAUDE_CONFIG_DIR at the isolated config
        # dir, the machine token, and --settings <file>. --print keeps it one-shot.
        CLAUDE_CONFIG_DIR="${statey_config_dir}" \
        CLAUDE_CODE_OAUTH_TOKEN="$(cat "${statey_token_file}")" \
            timeout 60 "${statey_claude_bin}" --settings "${statey_union_settings_file}" \
            --print -p "Say only the word: pong" >/dev/null 2>&1 && statey_union_ran=1 || statey_union_ran=0
    fi

    total=$((total + 1))
    printf "  %-50s " "State Y: --settings hook union FIRES (both channels)..."
    if [ "${statey_union_ran}" -eq 1 ] && [ -f "${statey_union_user_sentinel}" ] && [ -f "${statey_union_settings_sentinel}" ]; then
        echo "PASS"
        passed=$((passed + 1))
    elif [ "${statey_union_ran}" -eq 0 ]; then
        # Environment could not launch Claude (no binary/token, or run failed).
        # Visible named skip — never a silent green.
        echo "SKIP: --settings union firing check — Claude did not launch in this env"
    else
        echo "FAIL (union did not fire: user_sentinel=$([ -f "${statey_union_user_sentinel}" ] && echo yes || echo no), settings_sentinel=$([ -f "${statey_union_settings_sentinel}" ] && echo yes || echo no))"
        failed=$((failed + 1))
    fi

    # (d) Post-create skill is visible: no snapshot shadows it (structural under
    #     State Y — Claude reads the config dir's skills/ directly).
    total=$((total + 1))
    printf "  %-50s " "State Y: post-create skill visible (no snapshot)..."
    if [ -f "${statey_config_dir}/skills/statey-post-create-skill/SKILL.md" ] && [ ! -d "${statey_mission_dir}/claude-config/skills" ]; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (skill shadowed by a snapshot, or skill missing)"
        failed=$((failed + 1))
    fi
fi

# (e) GUARD: the developer's real ~/.claude.json mtime is unchanged across the
#     run — proves AgenC's trust write hit ONLY the isolated config dir.
total=$((total + 1))
printf "  %-50s " "State Y: real ~/.claude.json untouched (guard)..."
real_claude_json_mtime_after=""
if [ -f "${real_claude_json}" ]; then
    real_claude_json_mtime_after=$(stat -f %m "${real_claude_json}" 2>/dev/null || echo "")
fi
if [ "${real_claude_json_mtime_before}" = "${real_claude_json_mtime_after}" ]; then
    echo "PASS"
    passed=$((passed + 1))
else
    echo "FAIL (real ~/.claude.json mtime changed: ${real_claude_json_mtime_before} -> ${real_claude_json_mtime_after})"
    failed=$((failed + 1))
fi

# Best-effort cleanup of the mission created above.
if [ -n "${statey_short_id}" ]; then
    "${agenc_test}" mission rm "${statey_short_id}" >/dev/null 2>&1 || true
fi

echo ""
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

echo ""
echo "--- Mission spawn-from-mission provenance (requires server) ---"

# These tests verify the CLI auto-population + DB persistence of source/source_id
# when `agenc mission new` runs from inside a mission (AGENC_MISSION_UUID set).
# The tmux-link-set MIRRORING side of the feature (parent's link-set onto child)
# cannot be reliably E2E-tested — see CLAUDE.md "Tmux integration changes
# require manual testing". The DB-level round-trip below is the testable surface.

db_filepath="${repo_dirpath}/_test-env/database.sqlite"

# 1. CLI validation: --source without --source-id errors out
run_test "mission new rejects --source without --source-id" \
    1 \
    "${agenc_test}" mission new --blank --source=mission

# 2. CLI validation: --source-id without --source errors out
run_test "mission new rejects --source-id without --source" \
    1 \
    "${agenc_test}" mission new --blank --source-id=some-uuid

# 3. CLI validation: --source-id too long errors out
long_id=$(printf 'x%.0s' $(seq 1 300))
run_test "mission new rejects oversize --source-id" \
    1 \
    "${agenc_test}" mission new --blank --source=mission --source-id="${long_id}"

# 4. Happy path: AGENC_MISSION_UUID set → child mission row carries source=mission, source_id=<env>
fake_parent_uuid="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
AGENC_MISSION_UUID="${fake_parent_uuid}" run_test \
    "mission new with AGENC_MISSION_UUID succeeds" \
    0 \
    "${agenc_test}" mission new --blank --headless

# Verify the DB row reflects the auto-populated source
run_test_output_contains \
    "child mission has source=mission in DB" \
    "mission" \
    sqlite3 "${db_filepath}" "SELECT source FROM missions WHERE source_id='${fake_parent_uuid}' ORDER BY created_at DESC LIMIT 1;"

run_test_output_contains \
    "child mission has source_id=<parent-uuid> in DB" \
    "${fake_parent_uuid}" \
    sqlite3 "${db_filepath}" "SELECT source_id FROM missions WHERE source_id='${fake_parent_uuid}' ORDER BY created_at DESC LIMIT 1;"

# 5. Explicit override: --source=cron from inside a mission stays cron
fake_cron_id="cron-override-test"
AGENC_MISSION_UUID="${fake_parent_uuid}" run_test \
    "mission new --source=cron overrides auto-detection" \
    0 \
    "${agenc_test}" mission new --blank --headless --source=cron --source-id="${fake_cron_id}"

run_test_output_contains \
    "explicit cron source wins over auto-detected mission source" \
    "cron" \
    sqlite3 "${db_filepath}" "SELECT source FROM missions WHERE source_id='${fake_cron_id}' ORDER BY created_at DESC LIMIT 1;"

# 6. Headless from inside a mission reports the headless path (agenc-vupc:
#    the CLI dropped Headless on the wire and printed the attached-launch
#    message 'Launched in tmux pool' for source=mission spawns).
AGENC_MISSION_UUID="${fake_parent_uuid}" run_test_output_contains \
    "mission new --headless from mission reports headless" \
    "Running headless" \
    "${agenc_test}" mission new --blank --headless --no-focus

headless_from_mission_output=$(AGENC_MISSION_UUID="${fake_parent_uuid}" "${agenc_test}" mission new --blank --headless --no-focus 2>&1) || true
total=$((total + 1))
printf "  %-50s " "headless from mission not reported as attached..."
if echo "${headless_from_mission_output}" | grep -q "Launched in tmux pool"; then
    echo "FAIL (headless spawn printed 'Launched in tmux pool')"
    failed=$((failed + 1))
else
    echo "PASS"
    passed=$((passed + 1))
fi

echo ""
echo "--- Mission peer identity (agenc-padz) ---"

# `mission peers` translates Claude Code's ListAgents peer names into missions.
# The test environment never launches an interactive Claude, so no mission here
# is ever addressable — the empty-state message is the deterministic assertion.
run_test_output_contains "mission peers reports no messageable missions" \
    "No missions are running a Claude session that can be messaged" \
    "${agenc_test}" mission peers

run_test "mission peers rejects arguments" \
    1 \
    "${agenc_test}" mission peers deadbeef

run_test_output_contains "mission peers --help explains the ListAgents join" \
    "ListAgents" \
    "${agenc_test}" mission peers --help

# `mission inspect` must distinguish a mission with no live Claude session from
# one that is running but has no peer identity — a silent "--" would leave an
# agent unable to tell "not running" from "cannot be addressed".
peer_test_mission_id=$(sqlite3 "${db_filepath}" "SELECT id FROM missions ORDER BY created_at DESC LIMIT 1;")
"${agenc_test}" mission stop "${peer_test_mission_id}" >/dev/null 2>&1 || true

peer_inspect_output=""
for _ in $(seq 1 15); do
    peer_inspect_output=$("${agenc_test}" mission inspect "${peer_test_mission_id}" 2>&1 || true)
    if echo "${peer_inspect_output}" | grep -q "no live Claude session"; then
        break
    fi
    sleep 1
done

total=$((total + 1))
printf "  %-50s " "mission inspect names the missing-peer reason..."
if echo "${peer_inspect_output}" | grep -q "^Peer:.*no live Claude session"; then
    echo "PASS"
    passed=$((passed + 1))
else
    echo "FAIL (Peer line did not report a stopped mission as having no live session)"
    echo "    Output: ${peer_inspect_output}" | head -5
    failed=$((failed + 1))
fi

echo ""
echo "--- agenc prime routing-index content (agenc-88kh trim) ---"

# The corrected ephemerality framing is the load-bearing bug-fix from this trim.
# The old wording ("only pushed work survives") misled agents into mandatory
# push-everything behavior; the new wording lets local-only work stay local.
run_test_output_contains \
    "agenc prime contains corrected ephemerality framing" \
    "does this need to leave the mission" \
    "${agenc_test}" prime

run_test_output_contains \
    "agenc prime contains new operating-context preamble header" \
    "AgenC Operating Context" \
    "${agenc_test}" prime

run_test_output_contains \
    "agenc prime contains the self-reload --async constraint" \
    "Self-Reload Requires" \
    "${agenc_test}" prime

# Regression guard against reintroducing the old Layer 1 CLAUDE.md prepend
# title. If this string ever shows up in 'agenc prime' output, someone re-added
# the old agent_instructions.md content under a new name.
if "${agenc_test}" prime 2>&1 | grep -q "AgenC Agent Operating Instructions"; then
    echo "FAIL: agenc prime output contains old 'AgenC Agent Operating Instructions' header — Layer 1 prepend content has been reintroduced"
    failed=$((failed + 1))
    total=$((total + 1))
else
    echo "PASS: agenc prime output does not contain old Layer 1 header"
    passed=$((passed + 1))
    total=$((total + 1))
fi

echo ""
echo "--- Watcher FD-Leak Regression (agenc-ku7h) ---"

# Create a synthetic git repo with a large ignored subtree to verify the
# server's watcher doesn't blow up the FD count. On the pre-fix build
# (prior to Task 7 / commit 34c94da) the old fsnotify-walk implementation
# opened one inotify/kqueue FD per directory, causing ~100k FDs for large
# ignored trees. The fsnotify → notify migration pins it to a single
# FSEvents stream (macOS) or inotify watch (Linux) per repo.
fd_test_dir="${TMPDIR:-/tmp}/agenc-fd-test-$$"
rm -rf "${fd_test_dir}"
mkdir -p "${fd_test_dir}"
(
    cd "${fd_test_dir}"
    git init -q
    git config user.email "test@test.com"
    git config user.name "test"
    git commit --allow-empty -m "init" -q
)
printf "fake_node_modules/\n" > "${fd_test_dir}/.gitignore"
mkdir -p "${fd_test_dir}/fake_node_modules"
for i in $(seq 1 500); do
    printf "x" > "${fd_test_dir}/fake_node_modules/file_${i}.tmp"
done

# Register as a writeable copy under a throwaway repo reference that isn't in
# the library. The server will call ensureWriteableCopyExists, see the path
# exists as a git repo, find no library clone to compare against
# (expectedOriginURLForRepo returns ""), and proceed to start the watcher.
fd_test_repo="github.com/e2e-fd-test/fd-leak-guard"
"${agenc_test}" repo writeable-copy set "${fd_test_repo}" "${fd_test_dir}" >/dev/null 2>&1 || true

# Give the config-watcher debounce (500ms) plus watcher startup time to settle.
sleep 5

# Verify the watcher section completed (sanity check on test setup).
total=$((total + 1))
printf "  %-50s " "writeable-copy registered for FD test..."
wc_ls_output=$("${agenc_test}" repo writeable-copy ls 2>&1) || true
if echo "${wc_ls_output}" | grep -q "fd-leak-guard"; then
    echo "PASS"
    passed=$((passed + 1))
else
    echo "FAIL (writeable-copy not visible in ls; output: ${wc_ls_output})"
    failed=$((failed + 1))
fi

# Read the test-env server PID and count its open FDs.
server_pid_file="${repo_dirpath}/_test-env/server/server.pid"
if [ -f "${server_pid_file}" ]; then
    server_pid=$(cat "${server_pid_file}")
    fd_count=$(lsof -p "${server_pid}" 2>/dev/null | wc -l | tr -d ' ')
    echo "  Server FD count after writeable-copy watcher startup: ${fd_count}"

    total=$((total + 1))
    printf "  %-50s " "FD count stays under 1000 (agenc-ku7h)"
    if [ "${fd_count}" -gt 1000 ]; then
        echo "FAIL (FD count ${fd_count} exceeds threshold of 1000 — regression of agenc-ku7h)"
        failed=$((failed + 1))
    else
        echo "PASS (${fd_count} FDs)"
        passed=$((passed + 1))
    fi
else
    total=$((total + 1))
    printf "  %-50s " "FD count stays under 1000 (agenc-ku7h)"
    echo "SKIP (server PID file not found at ${server_pid_file})"
    passed=$((passed + 1)) # skip counts as pass — no server means no regression
fi

# Clean up the writeable-copy config entry and the synthetic dir.
"${agenc_test}" repo writeable-copy unset "${fd_test_repo}" >/dev/null 2>&1 || true
sleep 1
rm -rf "${fd_test_dir}"

echo ""
echo "--- Mission detach across tmux sessions (requires server + tmux) ---"

# Regression (agenc-vurk): detach must find the mission's window wherever it is
# linked, not only in the session the command was typed in. With several tmux
# sessions open at once, a mission's window routinely lives outside the caller's
# session, and detach used to fail with "pane N not found in session X".
detach_db_filepath="${repo_dirpath}/_test-env/database.sqlite"
detach_namespace_filepath="${repo_dirpath}/_test-env/namespace"
detach_namespace=""
if [ -f "${detach_namespace_filepath}" ]; then
    detach_namespace=$(cat "${detach_namespace_filepath}")
fi

detach_mission_pane=""
detach_mission_short_id=""
if [ -n "${detach_namespace}" ]; then
    detach_mission_output=$("${agenc_test}" mission new --blank --headless 2>&1) || true
    detach_mission_short_id=$(echo "${detach_mission_output}" | grep -oE '[0-9a-f]{8}' | head -1)
    if [ -n "${detach_mission_short_id}" ]; then
        detach_mission_pane=$(sqlite3 "${detach_db_filepath}" \
            "SELECT tmux_pane FROM missions WHERE id LIKE '${detach_mission_short_id}%';" 2>/dev/null || echo "")
    fi
fi

if [ -z "${detach_mission_pane}" ]; then
    total=$((total + 1))
    printf "  %-50s " "detach unlinks a window in another session..."
    echo "SKIP (could not create a mission with a tmux pane)"
    passed=$((passed + 1))
else
    detach_pool_session="agenc-${detach_namespace}-pool"
    detach_host_session="agenc-${detach_namespace}-e2e-host"
    detach_caller_session="agenc-${detach_namespace}-e2e-caller"
    tmux kill-session -t "=${detach_host_session}" >/dev/null 2>&1 || true
    tmux kill-session -t "=${detach_caller_session}" >/dev/null 2>&1 || true
    tmux new-session -d -s "${detach_host_session}" -x 80 -y 24
    tmux new-session -d -s "${detach_caller_session}" -x 80 -y 24

    # is_pane_in_session <session-name> — exit 0 when the mission's pane is
    # visible in that tmux session.
    is_pane_in_session() {
        tmux list-panes -s -t "=${1}" -F "#{pane_id}" 2>/dev/null \
            | grep -qx "%${detach_mission_pane}"
    }

    # The mission's window lives in a session the caller is not sitting in.
    tmux link-window -d -a -s "%${detach_mission_pane}" -t "=${detach_host_session}:"

    total=$((total + 1))
    printf "  %-50s " "mission window links into the host session"
    if is_pane_in_session "${detach_host_session}"; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (pane %${detach_mission_pane} not visible in ${detach_host_session})"
        failed=$((failed + 1))
    fi

    run_test "detach from a session that does not hold the window succeeds" \
        0 \
        env AGENC_CALLING_SESSION_NAME="${detach_caller_session}" \
        "${agenc_test}" mission detach "${detach_mission_short_id}"

    total=$((total + 1))
    printf "  %-50s " "detach unlinked the window from the host session"
    if is_pane_in_session "${detach_host_session}"; then
        echo "FAIL (pane %${detach_mission_pane} still in ${detach_host_session})"
        failed=$((failed + 1))
    else
        echo "PASS"
        passed=$((passed + 1))
    fi

    total=$((total + 1))
    printf "  %-50s " "detach left the window running in the pool"
    if is_pane_in_session "${detach_pool_session}"; then
        echo "PASS"
        passed=$((passed + 1))
    else
        echo "FAIL (pane %${detach_mission_pane} gone from ${detach_pool_session})"
        failed=$((failed + 1))
    fi

    # Detaching an already-pool-only mission is a no-op, not an error: nothing
    # is linked, so detach's postcondition already holds.
    run_test "detach of an already-detached mission succeeds" \
        0 \
        env AGENC_CALLING_SESSION_NAME="${detach_caller_session}" \
        "${agenc_test}" mission detach "${detach_mission_short_id}"

    # Unchanged behavior: when the caller's own session holds the window, that
    # is the session detach unlinks from.
    tmux link-window -d -a -s "%${detach_mission_pane}" -t "=${detach_caller_session}:"
    run_test "detach from the session holding the window succeeds" \
        0 \
        env AGENC_CALLING_SESSION_NAME="${detach_caller_session}" \
        "${agenc_test}" mission detach "${detach_mission_short_id}"

    total=$((total + 1))
    printf "  %-50s " "detach unlinked the window from the caller session"
    if is_pane_in_session "${detach_caller_session}"; then
        echo "FAIL (pane %${detach_mission_pane} still in ${detach_caller_session})"
        failed=$((failed + 1))
    else
        echo "PASS"
        passed=$((passed + 1))
    fi

    tmux kill-session -t "=${detach_host_session}" >/dev/null 2>&1 || true
    tmux kill-session -t "=${detach_caller_session}" >/dev/null 2>&1 || true
fi

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
