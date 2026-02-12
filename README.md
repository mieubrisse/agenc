![AgenC — AI work factory](./readme-images/cover.png)

AgenC: From Minecraft to Starcraft
==================================
AgenC (pronounced "agency") is a self-upgrading AI work factory that will make you absurdly productive.

### Wait what?
If you're like most people, you use Claude like this:

![](readme-images/common-ai-workflow.png)

Much better is John Rush's philosophy of [Inputs, Not Outputs](https://www.john-rush.com/posts/ai-20250701.html):

![](readme-images/inputs-not-outputs.png)

Each iteration of the loop makes all future outputs better.

Unfortunately, it's hard to scale this up. The Claudes start to step on each other, each lesson requires forking a new window, juggling all the windows becomes a circus, and you spend a bunch of time `cd`ing around and getting in and out of Claude.

That's where AgenC comes in:

![](readme-images/agenc-scale-up.png)

Example: this was my terminal today - 16 Claudes each working on different features, bugs, and housekeeping.

![](./readme-images/status-bar.png)

It's like going from Minecraft to Starcraft.


Quick Start
-----------

### Prerequisites

- **macOS** (won't work on Linux yet)
- **Claude Code** installed and in your PATH ([installation guide](https://docs.anthropic.com/en/docs/claude-code/getting-started))

### Install

```
brew tap mieubrisse/agenc
brew install agenc
```

This automatically installs required dependencies (`gh`, `fzf`, `tmux`).

### 1. Set AgenC config

By default AgenC stores all its configuration in `~/.agenc`. You can override this location by setting the `AGENC_DIRPATH` environment variable.

Run the initialization wizard:

```bash
agenc config init
```

When prompted:
- **"Do you have an existing agenc config repo?"** → Answer "no" if this is your first time
- **"Would you like to create one?"** → Recommend "yes" — AgenC will keep your config synced to GitHub automatically, making it portable across machines and recoverable if something goes wrong

### 2. Enter the interface

Launch the AgenC tmux session:

```bash
agenc attach
```

You'll be dropped into tmux with the "New Mission" dialog ready. The tmux session persists in the background — you can detach anytime with `Ctrl-b d` and reattach later with `agenc attach`.

### 3. Launch a mission

The "New Mission" picker offers several options:
- **New repo** — Start a mission in a GitHub repository (recommended: start with your dotfiles repo, as you'll be updating your global Claude config frequently)
- **Existing repo** — Resume work in a repository you've used before
- **New directory** — Work in an arbitrary local directory

Once inside the Claude session, work normally — write code, ask questions, iterate. Your mission is running.

**View mission status:**

```bash
agenc mission ls
```

This shows all active missions with their status, repository, and session title.

**Navigate between missions:**

Use standard tmux window commands:
- `Ctrl-b n` — next window
- `Ctrl-b p` — previous window
- `Ctrl-b [0-9]` — jump to window by number

**Tip:** Rebind these in your `~/.tmux.conf` for faster navigation:

```tmux
bind -n C-h previous-window
bind -n C-l next-window
```

### 4. Explore alongside Claude

Split a shell pane in the mission's workspace directory:

```
Ctrl-b %
```

This gives you a shell alongside Claude, handy for checking `git status`, running commands, or exploring files while Claude works.

**Tip:** Add these to your `~/.tmux.conf` for friendlier pane management:

```tmux
bind -n C-p split-window -h -c "#{pane_current_path}"
bind -n 'C-;' select-pane -t :.+
```

Now `Ctrl-p` creates a new pane and `Ctrl-;` swaps between them.

### 5. Start a new mission

Eventually you'll need a separate Claude session — a bug to investigate, config to change, or a different task entirely.

Open the command palette:

```
prefix + a, k
```

(By default, `prefix` is `Ctrl-b`, so the full sequence is `Ctrl-b`, then `a`, then `k`)

Type to filter, then select **"New Mission"**. This launches a fresh mission without disturbing your current work.

**Tip:** You can change the palette keybinding — see "Use the assistant" below.

### 6. End the mission

When you're done, exit Claude normally (`/exit` or Ctrl-D). The mission stops and enters "STOPPED" status.

**Resume later:**

```bash
agenc mission resume
```

This opens a picker showing stopped missions. Select one to resume with `claude --continue`, picking up exactly where you left off.

**Rename for easier recall:**

Use `/rename` inside Claude before exiting. AgenC shows this name in `agenc mission ls`, making missions easier to identify later.

**Remove completed missions:**

```bash
agenc mission rm
```

Select one or more missions to permanently remove.

**Full CLI reference:** See [docs/cli/](docs/cli/) for complete command documentation.

### 7. Use the assistant

You'll inevitably want to customize AgenC — add palette commands, change hotkeys, adjust window titles, or configure repo-specific settings.

The easiest way: use the **AgenC Assistant** from the command palette (`prefix + a, k`). It knows how to configure AgenC and can:
- Add custom palette commands (e.g., "Open dotfiles" → `agenc mission new github.com/mieubrisse/dotfiles`)
- Update keybindings
- Spawn, stop, resume, and remove missions
- Modify `config.yml` settings

**Example custom command:**

```bash
agenc config paletteCommand add \
  --name="open-dotfiles" \
  --title="📝 Open dotfiles" \
  --command="agenc tmux window new -- agenc mission new github.com/mieubrisse/dotfiles" \
  --keybinding="f"
```

Now `prefix + a, f` instantly opens your dotfiles in a new mission.

### 8. Secrets (optional)

AgenC isn't just for coding — it works for any agentic task: Todoist management, Notion organization, Google Workspace automation, and more. Many of these require API tokens for MCP servers.

To handle secrets securely, create a `.claude/secrets.env` file in your project using 1Password secret references:

**Example `.claude/secrets.env`:**

```bash
SUBSTACK_SESSION_TOKEN="op://Private/Substack Session Token/credential"
SUBSTACK_USER_ID="op://Private/Substack Session Token/username"
```

**Example `.mcp.json`:**

```json
{
  "mcpServers": {
    "substack": {
      "command": "npx",
      "args": ["-y", "@substack/mcp-server"],
      "env": {
        "SUBSTACK_SESSION_TOKEN": "op://Private/Substack Session Token/credential",
        "SUBSTACK_USER_ID": "op://Private/Substack Session Token/username"
      }
    }
  }
}
```

**Prerequisites:** Install the [1Password CLI (`op`)](https://developer.1password.com/docs/cli/get-started/) and authenticate.

When AgenC detects 1Password references in `.claude/secrets.env`, it automatically resolves them before launching Claude.

### 9. Send feedback

AgenC is new and actively evolving — your feedback shapes its direction. Share how you're using it, what's working, and what's not.

**Ways to send feedback:**
- Use the **"Send Feedback"** entry in the command palette (`prefix + a, k`)
- Ask the AgenC Assistant to send feedback for you
- [Join the Discord](https://discord.gg/x9Y8Se4XF3) and share your experience directly

Tips
----
- **Enable sandbox mode in Claude.** Run `/sandbox` from your global Claude Code (not inside a mission) to enable sandboxed command execution. This allows Claude to run commands within defined sandbox restrictions without manual approval prompts on every action. The setting automatically carries into every AgenC mission. This is the recommended alternative to `--dangerously-skip-permissions`.

- **Rename missions when you stop them.** Use `/rename` inside Claude to give a mission a descriptive name before exiting. This makes finding and resuming the right mission much easier later when you run `agenc mission resume` or `agenc mission ls`.

- **Tell your agents to always commit and push.** Unpushed work sits stranded on the mission's local filesystem. Add instructions to your CLAUDE.md telling agents to `git push` immediately after every commit. This lets you fire-and-forget instructions to your agents, confident the work will persist even if the mission ends.

- **Bind friendlier tmux hotkeys.** Add these to your `~/.tmux.conf` for faster workflow:
  ```tmux
  # Window navigation
  bind -n C-h previous-window
  bind -n C-l next-window

  # Pane creation and swapping
  bind -n C-p split-window -h -c "#{pane_current_path}"
  bind -n 'C-;' select-pane -t :.+
  ```

  After editing `~/.tmux.conf`, reload with: `tmux source-file ~/.tmux.conf`


How It Works


### Authentication
Each mission gets a clone of your global Claude Code credentials token at launch. This token expires roughly once a day. With a single Claude Code instance, it refreshes seamlessly. The problem comes with multiple simultaneous instances: they all try to refresh the same token at once, invalidating each other in a thrashing loop that causes auth failures across all missions.

TODO:
- AgenC will show Claude commandline sttu

See [docs/authentication.md](docs/authentication.md) for the full details on credential flow and MCP OAuth tokens.

Configuration
-------------

AgenC stores its state in `$AGENC_DIRPATH` (defaults to `~/.agenc`). The central configuration file is `$AGENC_DIRPATH/config/config.yml`.

Key features:

- **Synced repos** — keep repositories continuously up-to-date in a shared library
- **Cron jobs** — spawn headless missions on a schedule
- **Palette commands** — customize the tmux command palette and keybindings
- **Config auto-sync** — optionally back the config directory with Git for automatic versioning

See [docs/configuration.md](docs/configuration.md) for the full reference.

Troubleshooting
---------------

Run `agenc doctor` to check for common configuration issues.

### "Command Line Tools are too outdated"

If you see this error during installation:

```
Error: Your Command Line Tools are too outdated.
```

This is a [Homebrew requirement](https://docs.brew.sh/Common-Issues#homebrew-is-slow), not an AgenC issue. Homebrew requires up-to-date Xcode Command Line Tools to function, even when installing pre-built binaries.

To fix, update your Command Line Tools:

```
xcode-select --install
```

If that doesn't work, remove and reinstall:

```
sudo rm -rf /Library/Developer/CommandLineTools
xcode-select --install
```

Then retry `brew install agenc`.

Uninstall
---------

```
brew uninstall agenc
```

This removes the `agenc` binary. To also remove AgenC's data directory:

```
rm -rf ~/.agenc
```

If you customized `AGENC_DIRPATH`, remove that directory instead.

CLI Reference
-------------

Run `agenc --help` for available commands, or see [docs/cli/](docs/cli/) for complete documentation.

Theory
------

An AI agent is a probabilistic function. It takes input - context, instructions, tools - and produces a good output some percentage of the time. Not 100%. Never 100%. That's the fundamental constraint of the medium.

This is what makes AI agents different from traditional software. A well-written function returns the correct result every time. An AI agent returns a *useful* result most of the time, and the exact threshold depends on how well you've tuned it.

Your organization is a function too - composed of these agent functions. You have a coding agent, an email agent, a writing agent. Each is a probabilistic function with its own success rate. The org's overall capability is bounded by its weakest agents and degraded by uncertainty compounding across them.

This is what it means to "program an organization." The industrial capitalists could only approximate it - writing policies, training workers, hoping the message got through. You can do it precisely: adjust a prompt, add a permission, provide a better example. The agent updates immediately. The org function improves.

The key insight is that refining the outer function means refining the inner functions. Every time an agent misbehaves, that's signal. Capture it in the agent's config, and you've permanently raised its success rate. Do this systematically across all your agents, and the organization compounds in capability rather than in error.
