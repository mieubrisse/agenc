![](./image.png)

AgenC
=========

The industrial capitalists of the late 1800s were programmers. They "programmed" organizations using the lossy "coding" of English and the unreliable "processors" of humans. The results were revolutionary... but the method was imprecise, slow to iterate, and expensive to scale.

Now we have AI agents. AgenC (pronounced "agency") lets you program your own organization of agents, with yourself as the director - assembling interlocking workers into one cohesive, effective whole.

AI agents are probabilistic functions: they produce good outputs some percentage of the time. That percentage needs constant tuning - refining prompts, adjusting permissions, capturing lessons from failures.

AgenC makes this organization-building and agent-tuning easy, so you can focus on directing your AI workforce rather than wrestling with configuration.

<!--- TODO Debora feedback - why use AgenC? There are a million AIs out there; why do we need this one? -->

How it works
------------

1. Any time you have a negative interaction with an agent (bad output, missing permissions), it's trivial to roll the lesson back into the agent's config so you never hit it again ([Inputs, Not Outputs principle](https://mieubrisse.substack.com/p/inputs-not-outputs)). The agent then hot-reloads to pick up the new config.
2. Sandboxing and session management let you run dozens of agents simultaneously, constantly rolling lesson "exhaust" back into your agents' configs. They become a super team who understand your every whim.

<!-- TODO something about clear separation of "allow just this session" vs "allow always?" via the agent template mechanism and the sandboxing in a mission directory? -->

<!--
> ⚠️ Addiction Warning
>
> Like other agentic work factories, AgenC makes thought -> implemented reality nearly instantaneous.
>
> This is breathtaking, like going from Minecraft Survival -> Creative Mode. But there's a real danger to watch out for.
>
> The system goes as fast as you can tell it what to do, so suddenly the limiting factor is your ability to make decisions.
>
> Meaning, your head is going to be buzzing with a dozen threads at once, constantly deciding, constantly building with no downtime. It's like the deepest flow state you've ever had.
>
> This leaves you really activated, always wanting to implement one more thing. And it's really bad for sleep.
>
> This isn't just AgenC. [Across the board, agentic work factories seem to have this effect](https://steve-yegge.medium.com/steveys-birthday-blog-34f437139cb5#:~:text=This%20week%20the,Even%20for%20him.).
> 
> So please stop for breaks, and remember to make some wind-down time for sleep!
-->

<!--
Why AgenC?
----------
You want a swarm of infinite obedient robots, making your every whim a reality.

This requires a LOT of alignment work.

Every time the agents don't get it right, you need to capture the lesson back into your agent config: the [Inputs, Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs) doctrine.

But vanilla Claude is really bad at this.

Every time an agent gets it wrong, it's on you to find the config file that contributed to the problem.

Then you have to update it (usually requiring another Claude window).

Then, you have to find all your prompts using that config and restart them.

And if you want to do any sort of longitudinal retrospective on how well your prompts are performing overall... good luck.

AgenC solves this:

- All agent conversations are collected & navigable
- All agent config is version-controlled & deployed
- At any point when using an agent, you can fork a new Claude code window editing the agent itself
- When an agent's config is updated, all agents using that config restart with the config the next time they're idle
-->

Workflows
-------------
### AgenC is currently really good at these workflows

- **Human-in-the-loop assistant work with MCP:** Examples: email/Todoist inbox processing, calendar management.
- **Human-in-the-loop editing a repo:** Examples: coding, editing AgenC agents, writing

Basically, [painting with your mind](https://mieubrisse.substack.com/p/be-rembrandt).

It works like this:

1. You create or install **agent template** repos ([example](https://github.com/mieubrisse/agenc-agent-template_agenc-engineer)) containing the agent config you want (CLAUDE.md, skills, MCP, and even 1Password secrets to inject)
1. Agent templates get instantiated into an **agent** on a **mission**, with its own sandbox and repo copies where the agent can write files, commit, etc. without interfering with other missions
1. When an agent doesn't do the right thing, you fire off a new mission to refine the agent's prompt 
1. When an agent's config gets updated, all agents using that config live-reload the next time they're idle so fixes are constantly incorporated
1. All work is tracked and accessible, so you can run agents to analyze inefficiencies and roll improvements back into your AgenC

### AgenC doesn't currently handle these workflows, but will soon

- **Completely autonomous work:** Example: instruct the agent to do a thing without you being connected to the Claude TUI.
- **Dockerized:** Running agents in Docker so they can do `--dangerously-skip-permissions`
- **Cron:** Example: every Wednesday, summarize HackerNews and let me know what you found.
- **Automated lesson capture:** Identifying lessons that need to be rolled back into config proactively, rather than waiting for you.
- **Inter-agent communication:** Exmaple: the Code Writer agent hands off its work to the Code Reviewer agent who hands off to the PR Coordinator agent.

Getting started
---------------

Install via Homebrew:

```
brew tap mieubrisse/agenc
brew install agenc
```

To update:

```
brew update
brew upgrade agenc
```

### Troubleshooting: "Command Line Tools are too outdated"

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

Configuration
-------------

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENC_DIRPATH` | `~/.agenc` | Root directory for all AgenC state |

### config.yml

The file `$AGENC_DIRPATH/config/config.yml` holds project-level settings.

#### defaultFor

Each agent template can declare a `defaultFor` field that makes it the auto-selected template when creating a new mission in a specific context. The three recognized contexts are:

| Value | When used |
|---|---|
| `emptyMission` | `--git` is **not** specified (blank mission) |
| `repo` | `--git` repo is **not** an agent template |
| `agentTemplate` | `--git` repo **is** an agent template |

At most one template may claim each context. Example:

```yaml
agentTemplates:
  github.com/owner/coding-agent:
    nickname: coder
    defaultFor: emptyMission
  github.com/owner/repo-agent:
    defaultFor: repo
  github.com/owner/meta-agent:
    defaultFor: agentTemplate
```

The `defaultFor` field is optional — templates without it are never auto-selected. If the template claiming a context is not installed, a warning is printed and no agent template is used.

The `--agent` flag always overrides `defaultFor`.

#### syncedRepos

A list of repositories the daemon keeps continuously up-to-date (fetched and fast-forwarded every 60 seconds). Agent templates are always synced; use `syncedRepos` for non-template repos you also want kept fresh.

```yaml
syncedRepos:
  - github.com/owner/dotfiles
  - github.com/owner/my-project
```

Manage the list via the CLI:

```
agenc repo add owner/repo --sync   # clone and add to syncedRepos
agenc repo rm owner/repo           # remove from disk and syncedRepos
```

Entries in `config.yml` must use canonical format (`github.com/owner/repo`). The CLI accepts shorthand — `owner/repo`, `github.com/owner/repo`, or a full `https://github.com/owner/repo` URL — and normalizes it automatically.

### Config Auto-Sync

The `$AGENC_DIRPATH/config/` directory can be a Git repository. When it is, the daemon automatically commits and pushes any uncommitted changes every 10 minutes, using a commit message of the form:

```
2026-02-04T15:30:00Z agenc auto-commit
```

This keeps your agenc configuration version-controlled without manual effort. Changes to `config.yml`, `claude-modifications/`, or any other files in the config directory are captured automatically.

The push step is skipped if the repository has no `origin` remote — so a purely local Git repo (e.g. `git init` with no remote) will still get periodic commits for local history without push errors.

#### First-run setup

On the very first run, if stdin is a TTY, agenc prompts you to optionally clone an existing config repo:

```
Welcome to agenc! Setting up for the first time.

Do you have an existing agenc config repo to clone? [y/N]
```

Answering **yes** lets you provide a repo reference (`owner/repo`, `github.com/owner/repo`, or a GitHub URL), which agenc clones into `$AGENC_DIRPATH/config/`. This lets you restore your agenc configuration on a new machine or share it across machines. Answering **no** (or running non-interactively) proceeds with the default empty config.

Theory
------

An AI agent is a probabilistic function. It takes input - context, instructions, tools - and produces a good output some percentage of the time. Not 100%. Never 100%. That's the fundamental constraint of the medium.

This is what makes AI agents different from traditional software. A well-written function returns the correct result every time. An AI agent returns a *useful* result most of the time, and the exact threshold depends on how well you've tuned it.

Your organization is a function too - composed of these agent functions. You have a coding agent, an email agent, a writing agent. Each is a probabilistic function with its own success rate. The org's overall capability is bounded by its weakest agents and degraded by uncertainty compounding across them.

This is what it means to "program an organization." The industrial capitalists could only approximate it - writing policies, training workers, hoping the message got through. You can do it precisely: adjust a prompt, add a permission, provide a better example. The agent updates immediately. The org function improves.

The key insight is that refining the outer function means refining the inner functions. Every time an agent misbehaves, that's signal. Capture it in the agent's config, and you've permanently raised its success rate. Do this systematically across all your agents, and the organization compounds in capability rather than in error.

Design Goals
------------

- **Mission management** — Create, track, and organize missions with a simple CLI.
- **Mission isolation** — Each mission operates in its own directory with config copied from its agent template.
- **Self-contained** — The AgenC uses its own `CLAUDE_CONFIG_DIR` and never touches the user's existing Claude Code setup.
- **Configurable agents** — Agent templates let you define specialized agents with their own instructions, MCP servers, secrets, and skills.
- **Observable** — Clear logging and SQLite tracking for all missions.
- **Simple interface** — Submit a mission via the CLI. The AgenC handles the rest.
