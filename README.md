![AgenC — AI work factory](./readme-images/cover.png)

AgenC
========
AgenC (pronounced "agency") is an approachable orchestrator for taming multiple Claude agent chaos. Think: mission control for your AI work factory. Starcraft instead of Minecraft.

TODO demo video

### The Problem
Claude can run longer and better since Opus 4.5, so now you're trying to drive multiple windows at once.

But driving multiple Claudes sucks:

You have to set up Git worktrees to ensure they don't step on each other

Juggling terminal windows to keep them all fed is a circus

Claude's poor refresh token handling means they invalidate each other

And if you're following [Inputs, Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs) then each main Claude results in exponential side quest Claudes as you roll "lesson exhaust" back into your factory config. Which if you don't run `--dangerously-skip-permissions` is a _lot_ of `settings.json` config.

### The Solution
Treat your Claude agents like cattle, not pets.

AgenC:

- 📦 Gives each Claude session its own repo copy
- 🙋‍♂️ Shows when Claudes need attention

![All of these are Claude sessions. Yellow ones need my attention, blue ones are bubbling still.](./readme-images/status-bar.png)
- 🔐 Handles auth propagation automatically
- 🚀 Makes it instant to spawn, stop, and resume missions
- 🎮 Provides an access-anywhere command palette
- 💁‍♂️ Gives you an assistant agent so AgenC can drive itself

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
By default AgenC will install all its config to `~/.agenc`. This can be overridden with the `AGENC_DIRPATH` environment variable.

Run `agenc config init` to set up AGenC:

- when asked if you have an existing agenc config repo, say "no"
- when asked if you'd like to create one, I recommend "yes" since agenc will keep this synced up to Github for you.

### 1. Enter the interface

```
agenc attach
```

- the user will get dropped into tmux with the "new mission" dialog loaded

### 2. Launch a mission
- select "New repo"; recommend starting with the user's dotfiles (wherever they keep their global Claude config, because they'll be updating it a lot)
- start doing normal work in the Claude session
- Mission status can be seen with `agenc mission ls`
    - TODO show screenshot
- windows can be cycled through with `Ctrl-b → p` and `Ctrl-b → n`
    - TIP: I've rebound these to just `Ctrl-h` and `Ctrl-l` in my ~/.tmux.conf to make these easier:
      ```
      bind -n C-h previous-window
      bind -n C-l next-window
      ```

### 3. Explore alongside Claude
Press `Ctrl-b + %` to split a shell pane in the mission's workspace directory. Handy for checking git status, or poking around while Claude works.

- give a tip that I've added tmux keybindings to make panes easier to work with:
    - Ctrl-p opens a new pane: `bind -n C-p split-window -h -c "#{pane_current_path}"`
    - Ctr-; swaps between panes: `bind -n 'C-;' select-pane -t :.+`

### 4. Roll back lessons
- inevitably they'll hit something that needs a new Claude session - a separate bug to investigate, or config that needs changing
- press `prefix + a + k` to bring up the command palette (they can type to filter), and choose "New Mission"
    - TIP: the palette command hotkey can be changed; see "use the assistant" below

### 5. End the mission
- When done, simply exit Claude
- The mission goes to "STOPPED" status, and can be resumed later with `agenc mission resume`
- If you `/rename` inside Claude, agenc will show this in the `agenc mission ls` output
- Missions can be removed with `agenc mission rm`
- All CLI docs can be found in TODO link to ./docs/cli

### 5. Use the assistant
- Inevitably they'll want to change AgenC config: add custom commands to the command palette, change hotkeys, change the window titles that repos spawn with, etc.
- This is best done through the AgenC Assistant menu from the command palette: it knows how to configure AgenC
    - For example, I've added a custom palette command to open my dotfiles repo: `agenc tmux window new -- agenc mission new github.com/mieubrisse/dotfiles`
- This can also be used to spawn, stop, resume, and rm missions!

### 7. Secrets
- AgenC is for all agentic work, not just coding
    - E.g. I also use it for Todoist, Notion, and Google workspace management
- This requires tokens for my MCP servers
- A `.claude/secrets.env` containing 1Password secret references in the projects file will trigger AgenC 
    - Requires the 1Password `op` CLI installed

Example:

Project .claude/secrets.env
```
SUBSTACK_SESSION_TOKEN="op://Private/Substack Session Token/credential"
SUBSTACK_USER_ID="op://Private/Substack Session Token/username"
```

Project .mcp.json
```
SUBSTACK_SESSION_TOKEN="op://Private/Substack Session Token/credential"
SUBSTACK_USER_ID="op://Private/Substack Session Token/username"
```

### 6. Send feedback
- AgenC is very new, so I'd love to hear how they're using it
- This can be done through the "Send Feedback" command palette entry, or asking the assistant to send feedback
- You can also [join the Discord](https://discord.gg/x9Y8Se4XF3)

Tips
----
- **Enable sandbox mode in Claude.** Run `/sandbox` from your global Claude Code (not a mission) to enable sandboxed command execution. This lets Claude run commands within sandbox restrictions without manual approval prompts on every action, and the setting carries into every AgenC mission. This is the recommended alternative to `--dangerously-skip-permissions`.
- **Rename missions when you stop them.** Use `/rename` in Claude to rename a mission beofre you stop it. This helps in finding it later.
- **Tell your agents to always commit and push.** Unpushed work sits stranded on the mission's local filesystem. Add instructions to your CLAUDE.md telling agents to `git push` after every commit, which allows you to fire-and-forget instructions to your agents.
- **Bind friendlier tmux hotkeys**
    - TODO copy from above for pane creation, pane swapping, and window left & right


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
