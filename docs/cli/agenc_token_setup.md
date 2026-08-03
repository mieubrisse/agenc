## agenc token setup

Interactive wizard to obtain and store a long-lived OAuth token

### Synopsis

Run the interactive token-setup wizard.

This walks you through running 'claude setup-token' and storing the
resulting long-lived token. Requires a TTY (interactive terminal).

Use this when native Claude authentication doesn't work for your
multi-session AgenC workflow. For most users, 'claude auth login'
followed by normal AgenC usage is sufficient.

```
agenc token setup [flags]
```

### Options

```
  -h, --help   help for setup
```

### SEE ALSO

* [agenc token](agenc_token.md)	 - Manage the long-lived OAuth token (opt-in fallback for headless or multi-session use)

