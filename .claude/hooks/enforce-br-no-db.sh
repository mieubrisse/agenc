#!/usr/bin/env bash
set -euo pipefail

# PreToolUse hook: reject 'br' commands that don't include '--no-db'
# Input is JSON on stdin with tool_name and tool_input fields.

input="$(cat)"

tool_name="$(echo "${input}" | jq -r '.tool_name')"
if [[ "${tool_name}" != "Bash" ]]; then
    exit 0
fi

command="$(echo "${input}" | jq -r '.tool_input.command')"

# Extract the first word of the command (the binary being invoked)
first_word="$(echo "${command}" | awk '{print $1}')"

if [[ "${first_word}" != "br" ]]; then
    exit 0
fi

# Check that the command contains --no-db
if echo "${command}" | grep -q -- '--no-db'; then
    exit 0
fi

# Reject the command
echo "BLOCK: 'br' commands must include '--no-db'. Rerun with: br --no-db ..."
exit 2
