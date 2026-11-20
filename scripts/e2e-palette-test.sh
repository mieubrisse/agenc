#!/usr/bin/env bash
set -euo pipefail
script_dirpath="$(cd "$(dirname "${0}")" && pwd)"

# Drives the tmux command palette end to end, the way a person does: press the
# palette key, type into fzf inside the popup, press Enter, and check what the
# dispatched command received (KEV-123).
#
# Two facts shape the whole harness, both measured on 2026-10-19:
#
#   - A display-popup aimed at a session with no attached client renders on
#     whatever client IS attached to that tmux server -- which, on the default
#     server, is the developer's own screen. A separate tmux SESSION isolates
#     nothing. A separate tmux SERVER does.
#   - A popup is not a pane, so `send-keys` cannot type into fzf inside it.
#
# So the palette runs on its own tmux server (the "palette server"), whose only
# client is attached from inside a pane of a second private server (the "host
# server"). Typing into that host pane with `send-keys` is typing at the palette
# server's terminal, which is where a popup takes its input; `capture-pane` on
# the host pane is a screenshot of that terminal, popups included. Both servers
# live in a fresh TMUX_TMPDIR under unique -L names, and the run aborts before
# any popup unless the palette server's only client is the host pane.
#
# Needs tmux >= 3.3 (display-popup -e), fzf and sqlite3. Run via `make e2e-palette`, which
# builds _build/agenc first; `make e2e` runs it too.

repo_dirpath="$(cd "${script_dirpath}/.." && pwd)"
agenc_binary_filepath="${repo_dirpath}/_build/agenc"

# Every name below is unique to this run, so nothing here can address a tmux
# server, an AgenC server, or a mission belonging to anyone else.
PALETTE_SOCKET_NAME="agenc-e2e-palette-$$"
HOST_SOCKET_NAME="agenc-e2e-palette-host-$$"
PALETTE_SESSION_NAME="e2e-palette"
HOST_SESSION_NAME="e2e-host"
# Matches the default paletteTmuxKeybinding ("-n C-y"); asserted against the
# generated keybindings file below rather than trusted.
PALETTE_KEY="C-y"
FAKE_MISSION_UUID="e2e00000-0000-4000-8000-00000000c0de"
SCREEN_WAIT_SECONDS=20

passed=0
failed=0
total=0

record_pass() {
    total=$((total + 1))
    passed=$((passed + 1))
    printf "  %-62s PASS\n" "${1}..."
}

record_fail() {
    total=$((total + 1))
    failed=$((failed + 1))
    printf "  %-62s FAIL (%s)\n" "${1}..." "${2}"
}

echo "--- Palette end to end on an isolated tmux server (KEV-123) ---"

# ---------------------------------------------------------------------------
# Preflight. A missing tool is a failure, not a skip: a check that quietly
# inspects nothing is how coverage goes green while covering nothing.
# ---------------------------------------------------------------------------
if [ ! -x "${agenc_binary_filepath}" ]; then
    echo "FAIL: ${agenc_binary_filepath} not built (run 'make e2e-palette')" >&2
    exit 1
fi
if ! command -v fzf >/dev/null 2>&1; then
    echo "FAIL: fzf is required (the palette's picker) but is not on PATH" >&2
    exit 1
fi
if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "FAIL: sqlite3 is required (to register a stand-in mission) but is not on PATH" >&2
    exit 1
fi
# 3.3, not 3.2: the palette passes the calling pane and session into popups
# with `display-popup -e`, which tmux added in 3.3.
tmux_version_line="$(tmux -V 2>/dev/null || true)"
if [[ ! "${tmux_version_line}" =~ ^tmux\ ([0-9]+)\.([0-9]+) ]]; then
    echo "FAIL: cannot read a tmux version from '${tmux_version_line}'; tmux >= 3.3 is required" >&2
    exit 1
fi
tmux_major="${BASH_REMATCH[1]}"
tmux_minor="${BASH_REMATCH[2]}"
if [ "${tmux_major}" -lt 3 ] || { [ "${tmux_major}" -eq 3 ] && [ "${tmux_minor}" -lt 3 ]; }; then
    echo "FAIL: tmux >= 3.3 is required for display-popup -e (found '${tmux_version_line}')" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# A private tmux universe and a private AgenC.
#
# TMUX is cleared so no tmux call can find the caller's server through it, and
# TMUX_TMPDIR points at a fresh short directory (socket paths are capped near
# 104 bytes). The -L names are unique as well, because tmux silently falls back
# to /tmp/tmux-UID if TMUX_TMPDIR ever stops existing mid-run.
# ---------------------------------------------------------------------------
unset TMUX TMUX_PANE AGENC_MISSION_UUID
unset AGENC_CALLING_MISSION_UUID AGENC_CALLING_PANE_ID AGENC_CALLING_SESSION_NAME
universe_dirpath="$(mktemp -d /tmp/agenc-e2e-pal.XXXXXX)"
export TMUX_TMPDIR="${universe_dirpath}"
export AGENC_DIRPATH="${universe_dirpath}/agenc"
export AGENC_TEST_ENV=1
export CLAUDE_CONFIG_DIR="${AGENC_DIRPATH}/claude-config"

# Generated keybindings and the palette's own commands call `agenc` by name. The
# shim puts this build first on PATH, and both tmux servers inherit that PATH,
# so no step can reach an installed production agenc.
shim_dirpath="${universe_dirpath}/bin"
mkdir -p "${shim_dirpath}"
ln -s "${agenc_binary_filepath}" "${shim_dirpath}/agenc"
export PATH="${shim_dirpath}:${PATH}"

palette_tmux() {
    tmux -L "${PALETTE_SOCKET_NAME}" "$@"
}

host_tmux() {
    tmux -L "${HOST_SOCKET_NAME}" "$@"
}

# Teardown order matters: stop the AgenC server while its directory still
# exists, then the tmux servers while TMUX_TMPDIR still resolves to this
# universe, and only then remove the directory.
cleanup() {
    "${agenc_binary_filepath}" server stop >/dev/null 2>&1 || true
    palette_tmux kill-server >/dev/null 2>&1 || true
    host_tmux kill-server >/dev/null 2>&1 || true
    if [ -n "${universe_dirpath}" ] && [ "${universe_dirpath}/" != "/" ]; then
        rm -rf "${universe_dirpath}"
    fi
}
trap cleanup EXIT

plain_probe_filepath="${universe_dirpath}/probe-plain"
mission_probe_filepath="${universe_dirpath}/probe-mission"
popup_probe_filepath="${universe_dirpath}/probe-popup"

# Three probe commands, one per dispatch path. Each writes the AGENC_CALLING_*
# values it received, so the assertion is on what actually arrived:
#   - plain: a run-shell command, visible from any pane
#   - mission: names AGENC_CALLING_MISSION_UUID, which makes it mission-scoped,
#     so the palette must hide it from a pane with no mission
#   - popup: a display-popup command; its body is single-quoted so the values
#     can only come from the -e flags the palette injects into the popup
# Titles are single fuzzy-distinct tokens, so typing one matches only itself.
# The run-shell probes write through `tee` because the palette appends
# `>> palette.log 2>&1` to every command, which overrides a trailing `> file`.
mkdir -p "${AGENC_DIRPATH}/config" "${CLAUDE_CONFIG_DIR}"
git -C "${AGENC_DIRPATH}/config" init --quiet
cat > "${AGENC_DIRPATH}/config/config.yml" <<EOF
paletteCommands:
  e2ePlainProbe:
    title: e2eplainprobe
    command: |-
      printf '%s|%s\n' "\$AGENC_CALLING_PANE_ID" "\$AGENC_CALLING_SESSION_NAME" | tee ${plain_probe_filepath}
  e2eMissionProbe:
    title: e2emissionprobe
    command: |-
      printf '%s|%s|%s\n' "\$AGENC_CALLING_MISSION_UUID" "\$AGENC_CALLING_PANE_ID" "\$AGENC_CALLING_SESSION_NAME" | tee ${mission_probe_filepath}
  e2ePopupProbe:
    title: e2epopupprobe
    command: |-
      tmux display-popup -E 'printf "%s|%s\n" "\$AGENC_CALLING_PANE_ID" "\$AGENC_CALLING_SESSION_NAME" > ${popup_probe_filepath}'
EOF

# /bin/sh on both servers, so the developer's shell startup files cannot
# reorder PATH, change TMUX_TMPDIR, or print into the panes being screenshotted.
harness_tmux_conf_filepath="${universe_dirpath}/harness.tmux.conf"
echo "set -g default-shell /bin/sh" > "${harness_tmux_conf_filepath}"

palette_tmux -f "${harness_tmux_conf_filepath}" new-session -d -s "${PALETTE_SESSION_NAME}" -x 160 -y 45 /bin/sh
host_tmux -f "${harness_tmux_conf_filepath}" new-session -d -s "${HOST_SESSION_NAME}" -x 170 -y 50 \
    "env -u TMUX tmux -L ${PALETTE_SOCKET_NAME} attach-session -t =${PALETTE_SESSION_NAME}"

# The AgenC server's own tmux calls name no -L socket, so they look for the
# default socket inside TMUX_TMPDIR, where no server exists; AgenC refuses to
# create one (KEV-168) and runs without a pool session, unable to reach either
# harness server. It is needed for the keybindings file it writes on startup
# and for `agenc tmux resolve-mission`.
"${agenc_binary_filepath}" server start >/dev/null

keybindings_filepath="${AGENC_DIRPATH}/tmux-keybindings.conf"
deadline=$((SECONDS + SCREEN_WAIT_SECONDS))
while [ ! -s "${keybindings_filepath}" ] && [ "${SECONDS}" -lt "${deadline}" ]; do
    sleep 0.2
done
if ! grep -q "^bind-key -n ${PALETTE_KEY} run-shell" "${keybindings_filepath}" 2>/dev/null; then
    echo "FAIL: the AgenC server wrote no palette binding for ${PALETTE_KEY} to ${keybindings_filepath}" >&2
    exit 1
fi
palette_tmux source-file "${keybindings_filepath}"

palette_pane_id="$(palette_tmux display-message -p -t "=${PALETTE_SESSION_NAME}:" '#{pane_id}')"
host_pane_tty="$(host_tmux display-message -p -t "=${HOST_SESSION_NAME}:" '#{pane_tty}')"

# Isolation gate. Nothing popup-shaped happens until the palette server's one
# and only client is the host pane's terminal.
deadline=$((SECONDS + SCREEN_WAIT_SECONDS))
palette_client_ttys=""
while [ "${SECONDS}" -lt "${deadline}" ]; do
    palette_client_ttys="$(palette_tmux list-clients -F '#{client_tty}' 2>/dev/null || true)"
    if [ -n "${palette_client_ttys}" ]; then
        break
    fi
    sleep 0.2
done
if [ "${palette_client_ttys}" != "${host_pane_tty}" ]; then
    echo "FAIL: refusing to open any popup: palette server clients are '${palette_client_ttys}', expected only the host pane '${host_pane_tty}'" >&2
    exit 1
fi
record_pass "palette server's only client is the harness's own terminal"

# ---------------------------------------------------------------------------
# Driving the palette
# ---------------------------------------------------------------------------

# Prints the palette server's terminal as a person would see it, popups included.
screen() {
    host_tmux capture-pane -p -t "=${HOST_SESSION_NAME}:"
}

# The screen is captured into a variable before matching, never piped: under
# pipefail a capture that fails after grep matched would read as "not shown",
# and a failed capture must never count as a popup having closed.
wait_for_screen_to_show() {
    local pattern="${1}"
    local deadline=$((SECONDS + SCREEN_WAIT_SECONDS))
    local screen_text
    while [ "${SECONDS}" -lt "${deadline}" ]; do
        if screen_text="$(screen)" && grep -qE "${pattern}" <<< "${screen_text}"; then
            return 0
        fi
        sleep 0.2
    done
    return 1
}

wait_for_screen_to_drop() {
    local pattern="${1}"
    local deadline=$((SECONDS + SCREEN_WAIT_SECONDS))
    local screen_text
    while [ "${SECONDS}" -lt "${deadline}" ]; do
        if screen_text="$(screen)" && ! grep -qE "${pattern}" <<< "${screen_text}"; then
            return 0
        fi
        sleep 0.2
    done
    return 1
}

wait_for_file() {
    local filepath="${1}"
    local deadline=$((SECONDS + SCREEN_WAIT_SECONDS))
    while [ "${SECONDS}" -lt "${deadline}" ]; do
        if [ -s "${filepath}" ]; then
            return 0
        fi
        sleep 0.2
    done
    return 1
}

# Opens the palette with the real keybinding, types a query into fzf, and
# presses Enter. Returns non-zero if the palette never appeared.
pick_from_palette() {
    local query="${1}"
    # Quick Claude is a builtin shown in every pane, so its title on screen
    # means the palette popup is up and fzf is reading input.
    host_tmux send-keys -t "=${HOST_SESSION_NAME}:" "${PALETTE_KEY}"
    if ! wait_for_screen_to_show "Quick Claude"; then
        return 1
    fi
    host_tmux send-keys -t "=${HOST_SESSION_NAME}:" -l "${query}"
    if ! wait_for_screen_to_show "${query}"; then
        return 1
    fi
    host_tmux send-keys -t "=${HOST_SESSION_NAME}:" Enter
    # The query stays on screen exactly as long as the palette popup is open.
    wait_for_screen_to_drop "${query}"
}

expected_calling_values="${palette_pane_id}|${PALETTE_SESSION_NAME}"

# 1. Plain command from a plain pane: the palette key opens the popup, fzf
#    takes the selection, and the command runs with the calling pane and session.
test_name="plain command receives calling pane and session"
if ! pick_from_palette "e2eplainprobe"; then
    record_fail "${test_name}" "palette never opened or never closed; screen: $(screen | tr -s ' \n' ' ' | cut -c1-200)"
elif ! wait_for_file "${plain_probe_filepath}"; then
    record_fail "${test_name}" "the command never ran; palette log: $(tail -3 "${AGENC_DIRPATH}/logs/palette.log" 2>/dev/null | tr '\n' ' ')"
elif [ "$(cat "${plain_probe_filepath}")" != "${expected_calling_values}" ]; then
    record_fail "${test_name}" "got '$(cat "${plain_probe_filepath}")', expected '${expected_calling_values}'"
else
    record_pass "${test_name}"
fi

# 2. A mission-scoped command is hidden from a pane with no mission: fzf finds
#    no match, the palette closes without dispatching, and the probe never runs.
test_name="mission-scoped command hidden from a pane with no mission"
if ! pick_from_palette "e2emissionprobe"; then
    record_fail "${test_name}" "palette never opened or never closed"
else
    # Give a wrongly dispatched command the same window the passing cases get.
    if wait_for_file "${mission_probe_filepath}"; then
        record_fail "${test_name}" "the probe ran with '$(cat "${mission_probe_filepath}")'"
    else
        record_pass "${test_name}"
    fi
fi

# 3. A display-popup command: the palette injects the calling pane and session
#    into the second popup with -e, since a popup does not inherit run-shell's env.
test_name="popup command receives calling pane and session via -e"
if ! pick_from_palette "e2epopupprobe"; then
    record_fail "${test_name}" "palette never opened or never closed"
elif ! wait_for_file "${popup_probe_filepath}"; then
    record_fail "${test_name}" "the popup command never ran; palette log: $(tail -3 "${AGENC_DIRPATH}/logs/palette.log" 2>/dev/null | tr '\n' ' ')"
elif [ "$(cat "${popup_probe_filepath}")" != "${expected_calling_values}" ]; then
    record_fail "${test_name}" "got '$(cat "${popup_probe_filepath}")', expected '${expected_calling_values}'"
else
    record_pass "${test_name}"
fi

# 4. A mission-scoped command from a mission pane. A real mission needs an
#    authenticated Claude, so a stand-in row maps the palette pane to a mission
#    UUID; the server clears pane mappings only at startup, which has passed.
#    The keybinding's `agenc tmux resolve-mission` then finds it, exactly as it
#    would for a live mission.
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# The busy timeout rides out a write the running server may hold at this moment.
sqlite3 -cmd ".timeout 5000" "${AGENC_DIRPATH}/database.sqlite" \
    "INSERT INTO missions (id, short_id, status, created_at, updated_at, tmux_pane) VALUES ('${FAKE_MISSION_UUID}', '${FAKE_MISSION_UUID:0:8}', 'active', '${now}', '${now}', '${palette_pane_id#%}');"
test_name="mission command receives the resolved mission UUID"
expected_mission_values="${FAKE_MISSION_UUID}|${expected_calling_values}"
rm -f "${mission_probe_filepath}"
resolved_mission_uuid="$("${agenc_binary_filepath}" tmux resolve-mission "${palette_pane_id}" 2>/dev/null || true)"
if [ "${resolved_mission_uuid}" != "${FAKE_MISSION_UUID}" ]; then
    record_fail "${test_name}" "resolve-mission returned '${resolved_mission_uuid}' for pane '${palette_pane_id}', so the stand-in mission is not visible"
elif ! pick_from_palette "e2emissionprobe"; then
    record_fail "${test_name}" "palette never opened or never closed"
elif ! wait_for_file "${mission_probe_filepath}"; then
    record_fail "${test_name}" "the command never ran; palette log: $(tail -3 "${AGENC_DIRPATH}/logs/palette.log" 2>/dev/null | tr '\n' ' ')"
elif [ "$(cat "${mission_probe_filepath}")" != "${expected_mission_values}" ]; then
    record_fail "${test_name}" "got '$(cat "${mission_probe_filepath}")', expected '${expected_mission_values}'"
else
    record_pass "${test_name}"
fi

echo ""
echo "  Palette E2E: ${passed}/${total} passed, ${failed} failed"
if [ "${failed}" -gt 0 ]; then
    exit 1
fi
