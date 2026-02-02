### Insights
INSIGHT 1: Agents are basically other humans. You shouldn't give them worktrees because that would be like giving a human a Git repository where they can only work on one branch. That would be absurd - the human couldn't debug other branches and such. Especially for my private repos (dotfiles, Substack posts, etc.) where there's no real Github PR & approve devops system, it's fine for agents to just merge into master. They can deal with conflicts in the same way I do.

INSIGHT 2: Since agents are other humans, their config should be version-controlled independently of the repositories they they're working in. All agents running in the system should be running off the `master` branch of deployed agent configuration. It's unclear HOW we deploy their config (maybe copies of the agent config? maybe hardlinks?), but it should definitely be version-controlled and deployed separately of their work repos (just like a human: the human's knowledge & experience is tracked in the human's brain, independent of the repo contents).

INSIGHT 3: (not spending too much effort on now, but useful to track) All agents should work inside of Git repos. Even one-off missions should get a local-only Git repo created for them, which the user can promote to an actual Git repo if they desire. This gives the ability to commit after each turn by the agent, which allows for full audited agent conversation history.

### General Idea
- AgenC will continue to track which repos are agent-templates in its `repos` directory
- When a mission is being created off a repo, the repo will ALWAYS get created inside `workspace`
- To create a repo, we will simply _copy the entire directory that lives inside the `repos` directory_. Since that directory will always be force-pulling the default branch, agents will always get a full Git repo that's up-to-date with remote for their work. They can mess with their repo copy as much as they want.
- There's no need to create a new branch for each 
- To edit an agent itself, the flow is the same: we choose an agent, copy the repo into `workspace`, and then do our edits there.
- We'll need some sort of guidance (perhaps in the AgenC global CLAUDE.md) instructing agents that all their work should be done inside `workspace`.
- We can use the `settings.json` of each mission to block editing of anything except the `workspace` directory
- We can use the AgenC-global CLAUDE.md to instruct agents:
    - They should ONLY work in the `workspace` directory
    - Any attempt to access outside the workspace directory will be automatically denied
    - If the user asks to modify the agent's own CLAUDE.md or `.claude` folder, the agent should instruct the user to edit the agent's config (which will then get redeployed)
- The AgenC-global CLAUDE.md and `settings.json` should be created by merging with the user's ~/.claude/CLAUDE.md and ~/.claude/settings.json
- There should be a $AGENC_DIRPATH/global-claude that contains the CLAUDE.md and settings.json that get merged with the user's version

### Architecture
The $MISSION_UUID structure:
```
$MISSION_UUID/
    ...all the agent coordination things, like agenc wrapper PID, etc.
    agent/
        CLAUDE.md
        .mcp.json
        .claude/
            ...
        ...any other Claude files that are copied from the agent template
        workspace/
            ...a full Git repository, copied from agenc's "repos" repository library, for the agent to manipulate as it pleases
```

The $AGENC_DIRPATH structure:
```
$AGENC_DIRPATH/
    repos/
        ....repository library, getting constantly force-pulled for whichever ones are 1) agent templates or 2) in active use by missions. agent templates are always updated no matter what, since the user might use them at any time.
    claude/
        ...AgenC's version of Claude, which is assembled from the ~/.claude directory
    claude-modifications/
        CLAUDE.md
        settings.json
        ...anything else that will get merged into the ~/.claude contents to form $AGENC_DIRPATH/claude
    ...daemon, missions, etc. subdirectories...
```

### Later Work
- Make all agents update when the AgenC-global Claude config reloads
