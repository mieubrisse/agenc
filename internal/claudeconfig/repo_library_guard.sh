#!/usr/bin/env bash
# AgenC PreToolUse hook: replaces the bare permission denial Claude sees when
# trying to Write/Edit/NotebookEdit a file in the AgenC repo library with
# explicit guidance about how to make changes via a new mission.
#
# The repo library at $AGENC_DIRPATH/repos is treated as read-only by mission
# agents — local edits there don't persist. When an agent doesn't know that,
# it sees only "denied by your permission settings" and goes hunting for
# workarounds (e.g. writing via Bash + python). This hook replaces that vague
# message with directions to spawn a new mission scoped to the target repo.
#
# Fails open: if jq is missing or any unexpected condition occurs, exits 0 so
# Claude proceeds to the permission-deny layer in settings.json (which still
# blocks the write, just without the friendlier message).

set -euo pipefail

input="$(cat)"

if ! command -v jq >/dev/null 2>&1; then
    exit 0
fi

tool_name="$(printf '%s' "${input}" | jq -r '.tool_name // empty')"
case "${tool_name}" in
    Write|Edit|NotebookEdit) ;;
    *) exit 0 ;;
esac

file_path="$(printf '%s' "${input}" | jq -r '.tool_input.file_path // empty')"
if [ -z "${file_path}" ]; then
    exit 0
fi

agenc_dirpath="${AGENC_DIRPATH:-${HOME}/.agenc}"
repos_dirpath="${agenc_dirpath}/repos"

# Expand a leading ~/ in file_path so paths like ~/.agenc/repos/foo also match.
# The single-quoted '~/' pattern is required: ${var#~/} would tilde-expand the
# pattern itself, which would not match a literal "~/" prefix in the file path.
case "${file_path}" in
    "~/"*) file_path="${HOME}/${file_path#'~/'}" ;;
esac

case "${file_path}" in
    "${repos_dirpath}/"*) ;;
    *) exit 0 ;;
esac

# Compute a friendly repo identifier for the guidance message. The repo
# library layout is typically <host>/<owner>/<name>/... (e.g.
# github.com/mieubrisse/foo); fall back to the first path segment if not.
relative="${file_path#${repos_dirpath}/}"
if [[ "${relative}" =~ ^[^/]+/[^/]+/[^/]+ ]]; then
    repo_label="${BASH_REMATCH[0]}"
else
    repo_label="${relative%%/*}"
fi

reason="The path '${file_path}' is in the AgenC repo library, which mission agents treat as read-only — local edits there will not persist. The repo library is a shared resource for cross-repo exploration, not a writable workspace.

To make changes to '${repo_label}', spawn a new mission scoped to it:

  agenc mission new ${repo_label} --prompt \"<your task>\"

The new mission gets its own writable clone, isolated from this one. List available repos with 'agenc repo ls'. Track the spawned mission with 'agenc mission inspect <id>' and 'agenc mission print <id>'."

jq -n --arg reason "${reason}" '{
    hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: $reason
    }
}'

exit 0
