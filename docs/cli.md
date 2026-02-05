# CLI Reference

Auto-generated documentation for all agenc commands.

---

## agenc

The AgenC — agent mission management CLI

### Options

```
  -h, --help   help for agenc
```

### SEE ALSO

* agenc config  - Manage agenc configuration
* agenc daemon  - Manage the background daemon
* agenc login  - Log in to Claude (credentials stored in $AGENC_DIRPATH/claude/)
* agenc mission  - Manage agent missions
* agenc repo  - Manage the repo library
* agenc template  - Manage agent templates
* agenc version  - Print the agenc version


---

### agenc config

Manage agenc configuration

#### Options

```
  -h, --help   help for config
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI
* agenc config edit  - Open config.yml in your editor ($EDITOR)


---

#### agenc config edit

Open config.yml in your editor ($EDITOR)

```
agenc config edit [flags]
```

##### Options

```
  -h, --help   help for edit
```

##### SEE ALSO

* agenc config  - Manage agenc configuration


---

### agenc daemon

Manage the background daemon

#### Options

```
  -h, --help   help for daemon
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI
* agenc daemon restart  - Restart the background daemon
* agenc daemon start  - Start the background daemon
* agenc daemon status  - Show daemon status
* agenc daemon stop  - Stop the background daemon


---

#### agenc daemon restart

Restart the background daemon

```
agenc daemon restart [flags]
```

##### Options

```
  -h, --help   help for restart
```

##### SEE ALSO

* agenc daemon  - Manage the background daemon


---

#### agenc daemon start

Start the background daemon

```
agenc daemon start [flags]
```

##### Options

```
  -h, --help   help for start
```

##### SEE ALSO

* agenc daemon  - Manage the background daemon


---

#### agenc daemon status

Show daemon status

```
agenc daemon status [flags]
```

##### Options

```
  -h, --help   help for status
      --json   output in JSON format
```

##### SEE ALSO

* agenc daemon  - Manage the background daemon


---

#### agenc daemon stop

Stop the background daemon

```
agenc daemon stop [flags]
```

##### Options

```
  -h, --help   help for stop
```

##### SEE ALSO

* agenc daemon  - Manage the background daemon


---

### agenc login

Log in to Claude (credentials stored in $AGENC_DIRPATH/claude/)

```
agenc login [flags]
```

#### Options

```
  -h, --help   help for login
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI


---

### agenc mission

Manage agent missions

#### Options

```
  -h, --help   help for mission
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI
* agenc mission archive  - Stop and archive one or more missions
* agenc mission inspect  - Print information about a mission
* agenc mission ls  - List active missions
* agenc mission new  - Create a new mission and launch claude
* agenc mission nuke  - Stop and permanently remove ALL missions
* agenc mission resume  - Unarchive (if needed) and resume a mission with claude --continue
* agenc mission rm  - Stop and permanently remove one or more missions
* agenc mission stop  - Stop one or more mission wrapper processes


---

#### agenc mission archive

Stop and archive one or more missions

```
agenc mission archive [mission-id...] [flags]
```

##### Options

```
  -h, --help   help for archive
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission inspect

Print information about a mission

##### Synopsis

Print information about a mission.

Without arguments, opens an interactive fzf picker to select a mission.
With an argument, inspects the specified mission by ID.

```
agenc mission inspect [mission-id] [flags]
```

##### Options

```
      --dir    print only the mission directory path
  -h, --help   help for inspect
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission ls

List active missions

```
agenc mission ls [flags]
```

##### Options

```
  -a, --all    include archived missions
  -h, --help   help for ls
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission new

Create a new mission and launch claude

##### Synopsis

Create a new mission and launch claude.

Positional arguments select a repo or agent template. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your library ("my repo")

Without --agent, both repos and agent templates are shown. Selecting an agent
template creates a blank mission using that template. Selecting a repo clones
it into the workspace and uses the default agent template.

With --agent, only repos are shown. The flag value specifies the agent template
using the same format as positional args (git reference or search terms).

Use --clone <mission-uuid> to create a new mission with a full copy of an
existing mission's workspace. Override the agent template with --agent or a
positional search term.

```
agenc mission new [search-terms...] [flags]
```

##### Options

```
      --agent string    agent template (URL, shorthand, local path, or search terms)
      --clone string    mission UUID to clone workspace from
  -h, --help            help for new
      --prompt string   initial prompt to start Claude with
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission nuke

Stop and permanently remove ALL missions

```
agenc mission nuke [flags]
```

##### Options

```
  -f, --force   skip confirmation prompt
  -h, --help    help for nuke
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission resume

Unarchive (if needed) and resume a mission with claude --continue

##### Synopsis

Unarchive (if needed) and resume a mission with claude --continue.

Without arguments, opens an interactive fzf picker showing stopped missions.
Positional arguments act as search terms to filter the list. If exactly one
mission matches, it is auto-selected.

```
agenc mission resume [search-terms...] [flags]
```

##### Options

```
  -h, --help   help for resume
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission rm

Stop and permanently remove one or more missions

```
agenc mission rm [mission-id...] [flags]
```

##### Options

```
  -h, --help   help for rm
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

#### agenc mission stop

Stop one or more mission wrapper processes

##### Synopsis

Stop one or more mission wrapper processes.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, stops the specified missions by ID.

```
agenc mission stop [mission-id...] [flags]
```

##### Options

```
  -h, --help   help for stop
```

##### SEE ALSO

* agenc mission  - Manage agent missions


---

### agenc repo

Manage the repo library

#### Options

```
  -h, --help   help for repo
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI
* agenc repo add  - Add a repository to the repo library
* agenc repo edit  - Edit a repo via a new mission with a repo copy
* agenc repo ls  - List repositories in the repo library
* agenc repo rm  - Remove a repository from the repo library
* agenc repo update  - Fetch and reset repos to match their remote


---

#### agenc repo add

Add a repository to the repo library

##### Synopsis

Add a repository to the repo library by cloning it into $AGENC_DIRPATH/repos/.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

You can also use search terms to find an existing repo in your library:
  agenc repo add my repo               - searches for repos matching "my repo"

For shorthand formats, the clone protocol (SSH vs HTTPS) is auto-detected
from existing repos in your library. If no repos exist, you'll be prompted
to choose.

Use --sync to keep the repo continuously synced by the daemon.

```
agenc repo add <repo> [flags]
```

##### Options

```
  -h, --help   help for add
      --sync   keep this repo continuously synced by the daemon
```

##### SEE ALSO

* agenc repo  - Manage the repo library


---

#### agenc repo edit

Edit a repo via a new mission with a repo copy

##### Synopsis

Edit a repo via a new mission with a repo copy.

Positional arguments select a repo. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your repo library ("my repo")

The --agent flag specifies the agent template using the same format as
positional args (git reference or search terms). Without it, the default
agent template for repos is used.

```
agenc repo edit [search-terms...] [flags]
```

##### Options

```
      --agent string   agent template (URL, shorthand, local path, or search terms)
  -h, --help           help for edit
```

##### SEE ALSO

* agenc repo  - Manage the repo library


---

#### agenc repo ls

List repositories in the repo library

```
agenc repo ls [flags]
```

##### Options

```
  -h, --help   help for ls
```

##### SEE ALSO

* agenc repo  - Manage the repo library


---

#### agenc repo rm

Remove a repository from the repo library

##### Synopsis

Remove one or more repositories from the repo library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
syncedRepos list in config.yml if present.

Refuses to remove agent template repos. Use 'agenc template rm' instead.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

You can also use search terms to find a repo in your library:
  agenc repo rm my repo                - searches for repos matching "my repo"

```
agenc repo rm [repo...] [flags]
```

##### Options

```
  -h, --help   help for rm
```

##### SEE ALSO

* agenc repo  - Manage the repo library


---

#### agenc repo update

Fetch and reset repos to match their remote

##### Synopsis

Update one or more repositories in the repo library by fetching from
origin and resetting the local default branch to match the remote.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

You can also use search terms to find a repo in your library:
  agenc repo update my repo            - searches for repos matching "my repo"

```
agenc repo update [repo...] [flags]
```

##### Options

```
  -h, --help   help for update
```

##### SEE ALSO

* agenc repo  - Manage the repo library


---

### agenc template

Manage agent templates

#### Options

```
  -h, --help   help for template
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI
* agenc template add  - Add an agent template from a GitHub repository
* agenc template edit  - Edit an agent template via a new mission with a repo copy
* agenc template ls  - List installed agent templates
* agenc template new  - Create a new agent template repository
* agenc template rm  - Remove an installed agent template
* agenc template update  - Update properties of an installed agent template


---

#### agenc template add

Add an agent template from a GitHub repository

##### Synopsis

Add an agent template from a GitHub repository.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

You can also use search terms to find an existing repo in your library:
  agenc template add my repo           - searches for repos matching "my repo"

The clone protocol is auto-detected: explicit URLs preserve their protocol,
while shorthand references (owner/repo) use the protocol inferred from
existing repos in the library. If no repos exist, you'll be prompted to choose.

```
agenc template add <repo> [flags]
```

##### Options

```
      --default string    make this template the default for a mission context; valid values: emptyMission, repo, agentTemplate
  -h, --help              help for add
      --nickname string   optional friendly name for the template
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

#### agenc template edit

Edit an agent template via a new mission with a repo copy

```
agenc template edit [search-terms...] [flags]
```

##### Options

```
  -h, --help   help for edit
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

#### agenc template ls

List installed agent templates

```
agenc template ls [flags]
```

##### Options

```
  -h, --help   help for ls
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

#### agenc template new

Create a new agent template repository

##### Synopsis

Create a new agent template repository on GitHub.

If no repo is specified and stdin is a TTY, prompts interactively for the repo name.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/my-agent)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL

Behavior depends on the repository state:
  - If the repo does NOT exist on GitHub: prompts to create it, then initializes
    it with template files (CLAUDE.md, .claude/settings.json, .mcp.json)
  - If the repo exists but is EMPTY: clones it and initializes with template files
  - If the repo exists and is NOT empty: fails with an error

Use --clone to copy files from an existing template in your library. The --clone
flag accepts the same formats as above, or search terms to match against your
template library.

The new template is automatically added to your template library and a mission
is launched to edit it (same as 'template edit').

```
agenc template new [repo] [flags]
```

##### Options

```
      --clone string      copy files from an existing template (accepts repo reference or search terms)
      --default string    make this template the default for a mission context; valid values: emptyMission, repo, agentTemplate
  -h, --help              help for new
      --nickname string   optional friendly name for the template
      --public            create a public repository (default is private)
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

#### agenc template rm

Remove an installed agent template

##### Synopsis

Remove one or more agent templates from the template library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
agentTemplates list in config.yml.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/my-template)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL
  nickname                             - template nickname

You can also use search terms to find a template:
  agenc template rm my template        - searches for templates matching "my template"

```
agenc template rm [template...] [flags]
```

##### Options

```
  -h, --help   help for rm
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

#### agenc template update

Update properties of an installed agent template

```
agenc template update [template] [flags]
```

##### Options

```
      --default string    set or clear the mission context this template is the default for; valid values: emptyMission, repo, agentTemplate
  -h, --help              help for update
      --nickname string   set or clear the template nickname
```

##### SEE ALSO

* agenc template  - Manage agent templates


---

### agenc version

Print the agenc version

```
agenc version [flags]
```

#### Options

```
  -h, --help   help for version
```

#### SEE ALSO

* agenc  - The AgenC — agent mission management CLI


---

