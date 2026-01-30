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
