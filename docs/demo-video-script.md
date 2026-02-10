Demo Video Script
=================

**Target runtime:** 3–5 minutes
**Format:** Loom screen recording with voiceover
**Tone:** Conversational — like showing a friend a tool you're excited about
**Style:** All real terminal output. No slides, no mockups. Let the terminal speak.

Related beads: agenc-ce0, agenc-278, agenc-279, agenc-280, agenc-281

Pre-Recording Setup
-------------------

- Clean agenc state (fresh-install feel, but with 2–3 repos already added so the library isn't empty)
- A real repo ready that demonstrates value (e.g., a web app with a known bug to fix)
- Agenc tmux session running
- At least one custom palette command pre-configured (e.g., "Open dotfiles" bound to `prefix + f`)
- Terminal font size large enough for Loom recording clarity

Script
------

### 1. The Problem (15–20s)

Voiceover: "You're using Claude Code. You have multiple projects. Switching between them means losing context, managing configs, juggling terminal windows. What if every Claude session just had its own workspace?"

Optional: briefly show a messy terminal with multiple unorganized Claude sessions.

### 2. Install & First Look (20–30s)

Show the install:

```
brew tap mieubrisse/agenc
brew install agenc
```

Then launch:

```
agenc tmux attach
```

Quick visual tour of the tmux environment: "This is your command center. Everything runs inside here."

### 3. Repos — Your Library (30–40s)

Add a repo:

```
agenc repo add owner/some-repo
```

List repos:

```
agenc repo ls
```

Voiceover: "These are repos agenc knows about. It clones them locally and keeps them synced automatically. They're shared across all your missions — always up to date."

### 4. Missions — Isolated Workspaces (45–60s)

Create a new mission:

```
agenc mission new
```

The fzf picker appears. Select a repo. Claude launches in its own isolated sandbox.

Give Claude a quick task — something visible, like "list the routes in this app" or "find the auth bug." Let it start working.

Voiceover: "Every mission gets its own repo copy, its own config, its own credentials. Nothing bleeds between sessions. You can have ten missions running and they'll never interfere with each other."

### 5. Renaming Missions (20–30s) [agenc-281]

Show that the mission got auto-named from the initial prompt.

Rename it to something memorable:

> (Show whatever the rename mechanism is — tmux window rename, or agenc CLI if available)

Then show the mission list:

```
agenc mission ls
```

Voiceover: "Name your missions so you can come back to them. All context is preserved — just resume where you left off."

### 6. Sidebar Terminal (20–30s) [agenc-279]

While Claude is working in one pane, open a sidebar:

> `prefix + %` to split the pane

In the sidebar, run some commands alongside Claude:

```
git log --oneline -5
ls src/
```

Voiceover: "You're not locked into Claude's terminal. Open a pane, browse files, run tests, check git — all while Claude keeps working."

### 7. agenc do — Plain English (30–40s) [agenc-278]

This is the hero moment. Back in a clean window:

```
agenc do "Fix the login bug in web-app"
```

Claude interprets the request, picks the right repo, shows the planned action. Press ENTER to confirm. A new mission launches.

Voiceover: "Just describe what you want. Agenc figures out which repo, writes the prompt, and launches a mission. The gap between 'I need to do this' and 'I'm doing this' is basically zero."

### 8. Custom Commands (20–30s) [agenc-280]

Open the command palette:

> `prefix + a, k`

Browse the available commands. Select one to run it.

Then show a custom keybinding in action — e.g., press `prefix + a, f` to instantly open a dotfiles mission.

Briefly show how to create one:

```
agenc config palette-command add myCmd \
  --title="🚀 Deploy staging" \
  --command="agenc mission new deploy-repo --prompt 'Deploy to staging'" \
  --keybinding="d"
```

Voiceover: "Make agenc yours. One-key shortcuts for anything you do repeatedly."

### 9. Wrap-Up (15–20s)

Quick recap while showing `agenc mission ls` with several named missions running:

Voiceover: "Repos, missions, plain-English commands, custom shortcuts. Every time you fix a config issue, every future mission benefits. Your agents compound in capability over time."

Point to the README / GitHub link for install instructions.

Notes
-----

- Keep each section punchy. If narration is running long, cut words and let the terminal output tell the story.
- Don't explain every flag or option. Show the happy path. Link to docs for the rest.
- If a command takes a moment to run, use the wait time for voiceover rather than cutting.
- Rehearse the full flow at least once before recording to catch any hiccups in the demo environment.
