---
name: config-key-checklist
description: Use when adding or modifying a field in AgencConfig (internal/config/agenc_config.go), or when changing config.yml schema. Ensures the new config key has config get/set CLI support, README documentation, and architecture doc updates.
---

Config Key Checklist
====================

When adding a new field to `AgencConfig` in `internal/config/agenc_config.go`, complete all of the following before considering the work done.


Checklist
---------

### 1. `config get` support

Add the key to all three locations in `cmd/config_get.go`:

- `supportedConfigKeys` slice (alphabetical order)
- `getConfigValue()` switch statement
- `Long` help text in the command definition

### 2. `config set` support

Add the key to both locations in `cmd/config_set.go`:

- `setConfigValue()` switch statement (with type conversion if not a plain string)
- `Long` help text in the command definition

### 3. README documentation

In `README.md`:

- Add the key (commented out) to the annotated `config.yml` example block
- If the key introduces a new concept, add a brief explanation in the relevant section (e.g. under `paletteCommands` for palette-related keys)

### 4. Architecture doc

Update the `agenc_config.go` description line in `docs/system-architecture.md` if the key introduces a new concept or pattern.

### 5. Validation

If the key can conflict with other config values (e.g. keybinding collisions), add validation in `ReadAgencConfig()`.


Reference Patterns
------------------

### String key

```go
// config_get.go — getConfigValue()
case "myNewKey":
    if cfg.MyNewKey == "" {
        return "unset", nil
    }
    return cfg.MyNewKey, nil

// config_set.go — setConfigValue()
case "myNewKey":
    cfg.MyNewKey = value
    return nil
```

### Bool key

```go
// config_get.go — getConfigValue()
case "myBoolKey":
    if !cfg.MyBoolKey {
        return "unset", nil
    }
    return "true", nil

// config_set.go — setConfigValue()
case "myBoolKey":
    b, err := strconv.ParseBool(value)
    if err != nil {
        return stacktrace.NewError("invalid value '%s' for %s; must be true or false", value, key)
    }
    cfg.MyBoolKey = b
    return nil
```
