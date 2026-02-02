![](./image.png)

AgenC
=========
AgenC (pronounced "agency") is like Docker for your multi-agent vibeworking. It's:

- A runtime for running & juggling agents
- An LLMOps system for rolling fixes back into the agent config
- A plugin system for sharing agents

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

Why AgenC?
----------
You want a swarm of infinite obedient robots, making your every whim a reality.

This requires a LOT of alignment work.

Every time the agents don't get it right, you need to capture the lesson back into your agent config: the [Inputs, Not Outputs](TODO link to factory) doctrine.

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

User workflow
-------------
### Human-in-the-loop, output elsewhere (Todoist inbox processing, calendar processing)
1. Press something to easily fire up a 

### Human-in-the-loop, output in the repo (Substack writing, IG content generation)

### Human-in-the-loop Git edits for solo repository (e.g. dotfiles, checklists-and-templates)

### Autonomous 




How it works
------------
1. You create or install **agent template** repos containing the Claude config you want (CLAUDE.md, skills, MCP, and even 1Password secrets to inject)
1. You send agents on **missions**, which execute inside a sandbox directory created from the agent template
1. When talking to any agent on a mission, with a simple hotkey you can switch to editing the template, making your AgenC better forever
1. When an agent's config changes, the agent is restarted the next time it's idle to use the new config
1. All work is tracked and accessible, so you can run agents to analyze inefficiencies and roll improvements back into your AgenC


Getting started
---------------
TODO brew installation instructions

Conceptual models
-----------------
**Agent templates** instantiate **agents**.

Think of agents as functions, `f(context, agent_config) -> output`, whose output is good some % of the time.

When the agents don't produce good output, lessons should be rolled back into the agent config.

Future work
-----------
- Let you analyze mission results and proactively suggest fixes to agents
- Analyze how effective each agent is
- Execute missions inside Docker containers so `--dangerously-skip-permissions` is allowed
- Build settings.json files for you with AI (even out in your filesystem)
    - E.g. when you're starting a task, it will suggest settings.json's for you, so you don't have to give a bunch of "yes"s

Example Workflows
-----------------

The AgenC is general-purpose. Any task you could give to a Claude Code session, you can give to the AgenC. Some examples:

- **Code changes** — "Clone github.com/myorg/api, add rate limiting to all public endpoints, and open a PR."
- **Research** — "Research the top 5 Golang ORMs and write a comparison."
- **Writing** — "In the substack repo, write a post about the future of AI agents and commit it."
- **Calendar management** — "Add a weekly team sync every Tuesday at 10am to my Google Calendar."

Configuration
-------------

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENC_DIRPATH` | `~/.agenc` | Root directory for all AgenC state |

### config.yml

The file `$AGENC_DIRPATH/config/config.yml` holds project-level settings.

#### defaultAgents

Controls which agent template is auto-selected when creating a new mission. The key chosen depends on the `--git` context:

```yaml
defaultAgents:
  default: github.com/owner/coding-agent       # used when --git is NOT specified
  repo: github.com/owner/repo-agent            # used when --git repo is NOT an agent template
  agentTemplate: github.com/owner/coding-agent  # used when --git repo IS an agent template
```

All three subkeys are optional. Values must be in canonical format (`github.com/owner/repo`) and reference an installed agent template. If the referenced template is not installed, a warning is printed and no agent template is used.

The `--agent` flag always overrides `defaultAgents`.

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

Values must be in canonical format (`github.com/owner/repo`).

Design Goals
------------

- **Mission management** — Create, track, and organize missions with a simple CLI.
- **Mission isolation** — Each mission operates in its own directory with config copied from its agent template.
- **Self-contained** — The AgenC uses its own `CLAUDE_CONFIG_DIR` and never touches the user's existing Claude Code setup.
- **Configurable agents** — Agent templates let you define specialized agents with their own instructions, MCP servers, secrets, and skills.
- **Observable** — Clear logging and SQLite tracking for all missions.
- **Simple interface** — Submit a mission via the CLI. The AgenC handles the rest.

Status
------

This project is in early development.
