![AgenC — AI work factory](./readme-images/cover.png)

<div align="center">
    <h1>AgenC: Minecraft → Starcraft</h1>
</div>

</h1>
  <p align="center">
    AgenC makes you CEO of a self-improving agency of Claudes.</br>
    You no longer code/write/whatever, but guide the org's growth.</br>
    The Claudes do the rest.</br>
    </br>
    <a href="#why-agenc">Why AgenC</a>
    |
    <a href="#quickstart">Quickstart</a>
    |
    <a href="#how-it-works">How It Works</a>
    |
    <a href="https://discord.gg/x9Y8Se4XF3">Discord</a>
  </p>
</p>

Why AgenC
----------
If you're like most people, you use Claude like this:

![](readme-images/common-ai-workflow.png)

Much better is John Rush's philosophy of [Inputs, Not Outputs](https://www.john-rush.com/posts/ai-20250701.html):

![](readme-images/inputs-not-outputs.png)

Each iteration makes all future outputs better.

Unfortunately, it's hard to scale up.

The Claudes start to step on each other, each lesson requires forking a new window, juggling all the windows becomes a circus, and you spend a bunch of time `cd`ing around and getting in and out of Claude.

AgenC fixes this bottleneck:

![](readme-images/agenc-scale-up.png)

Example: this was my terminal today - 16 Claudes each working on different features, bugs, and housekeeping.

![](./readme-images/status-bar.png)

It's like going from Minecraft to Starcraft.

[AgenC demo](https://github.com/user-attachments/assets/d12c5b06-c5db-420a-aaa3-7b8ca5d69ab6)

> ### ⚠️ **ADDICTION WARNING** ⚠️
>
> AgenC **will** increase your output. But you should know it has a videogame-like addictive quality.
>
> Because it's so easy to launch work, you end up with tons of parallel threads. Like Starcraft, you enter this restless wired ADD state where you're managing dozens of things at once.
> 
> In building AgenC, I noticed it was hard to switch off and go to sleep. My brain would be buzzing with ideas, and I'd wake up in the middle of the night wanting to launch new threads.
>
> And it's not just AgenC - here's [Steve Yegge calling it the "AI Vampire"](https://steve-yegge.medium.com/the-ai-vampire-eda6e4f07163).
>
> Please remember to take breaks, and leave sufficient wind-down time before sleep!

Quickstart
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

### 1. 🔧 Initialize
The AgenC directory defaults to `~/.agenc`. Override with `AGENC_DIRPATH` if needed.

Run `agenc init` and answer the prompts. I recommend "yes" to creating a config repo; AgenC will sync it to GitHub automatically.

The AgenC interface is tmux. If you haven't used it before, here's a starter `~/.tmux.conf` you can use:

```tmux
# The -n hotkey means you don't have to use the tmux leader (ctrl-b) first
bind -n C-h previous-window
bind -n C-l next-window
bind -n 'C-;' select-pane -t :.+

set -g extended-keys on
bind -n C-S-h swap-window -t -1\; select-window -t -1
bind -n C-S-l swap-window -t +1\; select-window -t +1
```

It gives you `ctrl-h` and `ctrl-l` to cycle between windows, `ctrl-;` to cycle between panes, and `ctrl-shift-h` and `ctrl-shift-l` to reorder windows.

### 2. 🚀 Launch
Attach to the AgenC interface:

```bash
agenc attach
```

You'll be dropped into the repo selection screen. Select "Github Repo" and enter a repo you're working on.

AgenC will clone it, launch a new **mission**, and drop you into Claude to start your work.

Missions are the main primitive of AgenC: disposable self-contained workspaces with a full copy of the Git repo (not worktrees, so no need for a master merge queue).

You can see your missions with `agenc mission ls`.

### 3. 🎨 Command Palette
Inevitably you'll want to side work while Claude is chunking away.

Press `ctrl-y` to open the command palette and...

- 🚀 Launch a side mission (`ctrl-n`)
- 🐚 Open a shell in your current mission's workspace ("Open Shell" or `ctrl-p`)
- 🦀 Open a quick empty Claude for side questions
- 💬 Send me feedback about AgenC!

The command palette can also have custom commands with custom hotkeys.

One way to do this is through the `agenc config paletteCommand`. For example, this is to open my dotfiles:

```
agenc config paletteCommand add dotfiles \
    --title="🛠️ Open Dotfiles" \
    --command="agenc tmux window new -- agenc mission new mieubrisse/dotfiles" \
```

Another way to do this is through the assistant.

### 5. 💁‍♂️ Assistant
AgenC has an assistant ("AgenC Assistant" on the palette) that knows how to configure AgenC, as well as launch and manage missions.

You _can_ use the `agenc config` commands to configure stuff like palette commands... but now I just talk to the assistant for my AgenC configuration needs.

### 6. Mission Management
You can see all missions with `agenc mission ls`. 

Missions can also be stopped with "Mission Stop" or "Mission Resume" on the palette. Since each mission is an isolated workspace, no work is lost.

Full CLI docs: [docs/cli/](docs/cli/)

### 6. Secrets (optional)

If you create a `.claude/secrets.env` with [1Password CLI secret references](https://developer.1password.com/docs/cli/secret-references/) in it, AgenC will resolve them on mission launch and inject them into Claude. This is useful for MCP server credentials.

For example:

.claude/secrets.env:
```bash
SUBSTACK_SESSION_TOKEN="op://Private/Substack Session Token/credential"
SUBSTACK_USER_ID="op://Private/Substack Session Token/username"
```

.mcp.json:
```
{
    "mcpServers": {
        "substack-api": {
            "command": "npx",
            "args": ["-y", "substack-mcp@latest"],
            "env": {
                "SUBSTACK_PUBLICATION_URL": "https://mieubrisse.substack.com/",
                "SUBSTACK_SESSION_TOKEN": "$SUBSTACK_SESSION_TOKEN",
                "SUBSTACK_USER_ID": "$SUBSTACK_USER_ID"
            }
        }
    }
}
```

### 8. Send feedback

Use "Send Feedback" in the command palette, ask the AgenC Assistant, or [join the Discord](https://discord.gg/x9Y8Se4XF3).

Tips
----
- **Run Claude in sandbox mode.** This cuts cuts a lot of permission request fatigue. Run `/sandbox` from your global Claude Code (not inside a mission) to enable sandboxed command execution. This allows Claude to run commands within defined sandbox restrictions without manual approval prompts on every action. The setting automatically carries into every AgenC mission. This is the recommended alternative to `--dangerously-skip-permissions`.

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
agenc mission nuke -f
agenc daemon stop
brew uninstall agenc
```

This stops the agenc daemon and removes all mission assets. To remove the AgenC data directory itself:

```
rm -rf ~/.agenc
```

If you customized `AGENC_DIRPATH`, remove that directory instead.

Development
-----------

### Running the linter

AgenC uses `golangci-lint` to enforce code quality standards. To run the linter locally:

**Install golangci-lint:**

```bash
brew install golangci-lint
```

**Run the linter:**

```bash
golangci-lint run
```

The linter configuration is defined in `.golangci.yml` and includes checks for:
- Unchecked errors (`errcheck`)
- Security issues (`gosec`)
- Go vet checks (`govet`)
- Advanced static analysis (`staticcheck`)
- Unused code (`unused`)
- Cyclomatic complexity (`gocyclo`, max 15)

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
