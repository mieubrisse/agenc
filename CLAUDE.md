Agent Factory
=============

Running the Binary
------------------

When running or testing the `agenc` binary, **always** use the relative path `./agenc` — never the full absolute path.

```
# Correct
./agenc mission new "my mission"
./agenc mission ls

# Wrong — will trigger unnecessary permission prompts
/Users/odyssey/code/agent-factory/agenc mission new "my mission"
```

The project's `.claude/settings.json` allows `Bash(./agenc:*)`. Using the absolute path does not match this pattern and will cause avoidable permission prompts on every invocation.

Accessing ~/.agenc
------------------

You have unrestricted `Read`, `Glob`, and `Grep` access to `~/.agenc/` (configured in `.claude/settings.json`). When you need to explore or search files under `~/.agenc/`, **always** use the `Glob` and `Grep` tools — never Bash commands like `ls`, `find`, or `grep`.

```
# Correct — use native tools
Glob("~/.agenc/**")
Grep(pattern, path="~/.agenc/")
Read("~/.agenc/some/file.json")

# Wrong — unnecessary Bash when native tools work without prompts
ls ~/.agenc/
find ~/.agenc/ -name "*.json"
grep -r "pattern" ~/.agenc/
```

The native tools run without permission prompts and provide better-structured output. Reserve Bash for operations that genuinely require shell execution.
