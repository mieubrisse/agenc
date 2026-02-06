Agency Description
==================

Agency is a multi-agent orchestration system for doing arbitrary work.


Knowledge Base
--------------

Agency stores knowledge and files of all kinds in the Knowledge Base.

- The Knowledge Base is a store of files identified by UUID
- Each file in the Knowledge Base has a description
- Each agent has one of three access levels to each file: `None`, `Read`, or `Write`
    - `None`: the agent knows the file exists (UUID and description visible) but cannot read its contents
    - `Read`: the agent can read the file and its description
    - `Write`: the agent can read, create, and edit the file and its description
- Files are concurrency-controlled via optimistic locking:
    - Arbitrary many agents can read simultaneously
    - Writing requires passing the hash of the last-seen version, ensuring the agent is working from current state

### Knowledge Base MCP Tool

- **List files**: lists files the agent has `Read` or `Write` access to
- **Browse files**: searches descriptions of files the agent has at least `None` access to
- **Get paths**: returns full paths of files the agent has `Read` or `Write` access to (for direct filesystem reads, copies, etc.)
- **Write file**: updates a file in the Knowledge Base (requires last-seen version hash)
- **Create file**: creates a new file in the Knowledge Base
- Write operations acquire a write lock

### Repos

Repos are managed via the Knowledge Base as either:
- A whole repo registered as a UUID in the Knowledge Base, or
- An MCP tool backed by a repo library that manages local clones (avoiding redundant clones when multiple agents need the same repo)

Agents can clone repos into their ephemeral workspace to do development work.


Outcomes
--------

Agency uses Fractal Outcomes to model work.

- Every piece of work, from life goals to granular tasks, is an **outcome**: a change to be accomplished in the world
- Outcomes are Markdown files in the Knowledge Base, identified by UUID like any other file
    - The file itself describes the outcome: what it is, its status, rationale (if closed), and references to parent and child outcome UUIDs
- Outcomes relate to each other in a directed acyclic graph (DAG):
    - An outcome's children represent **how** to accomplish it
    - An outcome's parents represent **why** it's being accomplished
- Every outcome is a hypothesis: attempting it may prove it's not achievable as described
    - An outcome may have competing child hypotheses (e.g., "get lunch" may have children: "get a sandwich", "order sushi", "get a burger")
- Outcomes can be decomposed into children as much as necessary, but no more; Fractal Outcomes is a thinking framework, not a prescription to always decompose

### Outcome MCP Tool

- **View outcome**: read an outcome and its immediate children/parents
- **View ancestor chain**: read the chain of parent outcomes (the "why?" chain)
- **View subtree**: read an outcome and all descendants
- **Create outcome**: create a new outcome as a child of an existing one
- **Update outcome**: modify an outcome's description, status, or relationships
- **Complete outcome**: mark an outcome as accomplished
- **Close outcome**: mark an outcome as disproven or abandoned (rationale is written into the outcome's Markdown file, so any agent with Read access to the outcome can understand why it was closed)


Capabilities & Secrets
----------------------

Agency uses **capability-based access control**. Rather than maintaining a global permissions table, capabilities are tokens of authority that are granted, delegated, and revoked alongside Outcomes.

### Capabilities

A capability grants a Handler access to a specific resource: a Knowledge Base file, an MCP server, a repo, etc. Capabilities have the following properties:

- **Delegatable**: a Handler can grant any capability it holds to an Underling Handler
- **Outcome-scoped**: capabilities granted during delegation are tied to the Outcome's lifetime. When the Outcome is completed or closed, all capabilities granted for it are automatically revoked.
- **Restrictable**: a Handler can delegate a restricted version of a capability (e.g., grant `Read` when it holds `Write`)
- **Non-forgeable**: an Agent cannot fabricate capabilities it was not granted

When an Agent needs a capability it doesn't have:
1. The Agent messages its Boss Handler requesting the capability
2. If the Boss Handler holds the capability, it grants a scoped version to the Underling
3. If the Boss Handler doesn't hold it either, it escalates to its own Boss Handler
4. The capability flows back down the chain, outcome-scoped at each level
5. When the Outcome completes, all capabilities granted for it are revoked at every level — no zombie permissions accumulate

### MCP Server Access

MCP server access is a capability like any other. A Handler with the `gmail` MCP capability can delegate it to an Underling that needs to send emails. The capability is revoked when the Underling's Outcome completes.

### Secrets

Some capabilities (particularly MCP server access) require secrets: API tokens, credentials, etc. Secrets are managed by Agency, not by Agents:

- Secrets live in a secrets store (e.g., 1Password, HashiCorp Vault)
- MCP server configurations reference secret IDs, not raw secret values
- When Agency spins up an Agent that needs an MCP server, Agency resolves the secret references and injects them into the Agent's environment at runtime
- The Agent uses the MCP server but never sees or handles the underlying secret
- Agents cannot extract, forward, or store secrets — Agency mediates all secret access


Handlers & Agents
-----------------

Agency uses Handlers and Agents to accomplish work.

### Handlers

A Handler is a durable process responsible for driving one or more Outcomes towards completion.

- Each Handler is responsible for a set of Outcomes
- Each Handler has `Read` access to the Outcome it was delegated (only the Boss Handler can modify it)
- Each Handler has `Write` access to any child Outcomes it creates under its delegated Outcome
- Each Handler has an inbox for receiving messages from other Handlers
- Each Handler has a state document (see "Handler State Document" below)
- Each Handler has an audit log (see "Handler Audit Log" below)
- Each Handler has a Boss Handler: the Handler that delegated the Outcome to it
    - Exception: the root Handler (see "The User & the Root Handler" below)
- A Handler may delegate an Outcome to a new child Handler, creating a new Handler with its own inbox responsible for that Outcome
- A Handler spawns Agents to do its work (see below)

**Handler activation triggers** — a Handler spawns a new Agent (if one is not already running) when:
- A message arrives in its inbox
- An Outcome state change occurs that may require action:
    - A child Outcome is completed (the Handler may need to take next steps or synthesize results)
    - A child Outcome is closed/disproven (the Handler may need to renavigate — try a competing hypothesis or decompose differently)
    - An Outcome the Handler is responsible for becomes unblocked (a dependency it was waiting on has been resolved)
    - A new Outcome is delegated to this Handler

### Agents

An Agent is an ephemeral Claude instance spawned by a Handler to do work.

- Each Agent is identified by a UUID
- Agents are intended to be "functions": wake up, assess state, act, update durable state, and terminate
- An Agent inherits its Handler's permissions and Knowledge Base access
- An Agent operates in an ephemeral workspace that is destroyed when the Agent terminates
- **Anything the Agent wants to persist must be explicitly written to the Knowledge Base.** The workspace is destroyed on termination. If it wasn't written to the KB, it's gone.

**Messaging is asynchronous.** Sending a message is a fire-and-forget operation. Checking the inbox is a separate operation. Agents are never blocked waiting for a reply — they send a message, continue with other work or terminate, and process responses on their next activation.

When spawned, an Agent operates in two phases:

**Phase 1: Inbox processing.** The Agent reads the Handler's inbox and processes all pending messages. The goal is to empty the inbox by transforming messages into Outcomes, updating the state document, or sending replies. The Agent should not begin deep work during this phase — it should focus on triage and organization.

**Phase 2: Work.** The Agent reads the Handler's state document, assesses the current state of its Outcomes, and prioritizes work using the Eisenhower Matrix (urgent vs. important). It then takes action: reads/writes KB files, works on Outcomes, delegates to new Handlers, clones repos, does development work, sends messages, etc.

Before terminating, the Agent:
1. Updates the Handler's state document to reflect current context
2. Submits an audit log entry documenting what it did and why
3. Terminates

### Handler State Document

Each Handler has a **state document** — a Knowledge Base file that represents the Handler's current understanding of the world and its work.

- The Handler's Agent has `Read` and `Write` access to the state document
- The Agent reads the state document on activation to understand current context: what's been done, what's in progress, what's blocked, key decisions made, and relevant facts discovered
- The Agent updates the state document before terminating to reflect what changed during this activation
- This is the primary continuity mechanism across Agent lifetimes: each new Agent reads the state document to pick up where the last one left off
- The state document should be kept concise and current — it represents the Handler's "working memory," not a full history

### Handler Audit Log

Each Handler also has an **audit log** — a Knowledge Base file that records what each Agent did during its activation.

- The Agent has read-only access to the audit log (Agency writes to it on the Agent's behalf)
- Before terminating, the Agent provides its activity summary to Agency (via MCP tool), which appends it to the audit log
- The audit log provides a complete history for debugging and review, but is not the primary mechanism for Agent-to-Agent continuity (the state document is)
- The Agent may consult the audit log for detailed history when the state document doesn't have enough context

### The User & the Root Handler

The user interacts with Agency through the root Handler — the Handler responsible for the root-level Outcome (e.g., "Live an effective life").

- To the root Handler, the user functions as its Boss Agent
- The user has an inbox where the root Handler sends messages: completion requests, escalation questions, progress updates, and requests for information or decisions
- The user sends messages to the root Handler's inbox to assign work, provide feedback, answer questions, and give direction
- The root Handler otherwise behaves like any other Handler: it decomposes Outcomes, delegates to child Handlers, and manages its subtree

This means the entire system has a uniform interface at every level: Handlers communicate with their boss via messaging, whether that boss is another Handler or the user.

### The Power of the Uniform Interface

Every boundary in the system speaks the same protocol: inbox messaging + outcome delegation. This has several consequences:

- **Substitutability at any node.** The root Handler doesn't know whether its boss is a human typing in a terminal, a Slack bot, a cron job, or another AI system. It just reads its inbox and sends messages to its boss. You could plug a different human in at the root. You could plug a team of humans in (shared inbox). You could plug another Agency instance in — one Agency's leaf Handler delegating to another Agency's root Handler.
- **Testability.** You can test any Handler in isolation. Mock the boss by sending messages into the Handler's inbox. Observe what the Handler's agent does — what outcomes it creates, what messages it sends back, what KB files it writes. The Handler doesn't know it's being tested. This is unit testing for organizational behavior.
- **Insertable management layers.** If a Handler is directly managing 20 Underling Handlers and it's getting unwieldy, insert a middle-management Handler. Re-delegate 10 outcomes to the new Handler, which re-delegates to the original Underlings. No Underling needs to change — it still sees a boss inbox and sends messages back.
- **Removable management layers.** The reverse works too. If a middle layer adds overhead without value, the Boss Handler shuts it down, reclaims the outcomes, and re-delegates directly. Underlings don't need to change.
- **Federation.** Different subtrees can run on different infrastructure — different machines, cloud regions, or organizations. As long as they speak the inbox/outcome protocol, they interoperate.
- **Multi-user at any level.** Nothing stops you from having multiple users at different points in the tree: a CTO user at the "Software" subtree, a CFO user at the "Finance" subtree. The system doesn't need a singular concept of "the user" — just "the boss of this Handler," which could be anything.

Because every boundary looks the same, you can restructure the organization — add layers, remove layers, swap humans for machines, split subtrees, merge subtrees — without changing any of the components.

### User Interface

The user interacts with Agency via a CLI that provides:

- **Inbox**: read and send messages to/from the root Handler (the primary interaction mode)
- **System visibility**: inspect Handlers, their states, Outcomes, the DAG, active Agents, inbox depths, and audit logs
- **Direct intervention**: message any Handler in the tree (not just the root), inspect any Outcome, view any audit log

The CLI is the initial interface. The uniform protocol means other interfaces (web dashboard, Slack bot, mobile app) can be layered on later without changing the underlying system.

### Delegation

When a Handler delegates an Outcome to a new child Handler:

- A new Handler is created, responsible for that Outcome
- The delegating Handler becomes the Boss Handler of the new child Handler
- The delegating Handler's Agent curates what capabilities to grant the child Handler (following the principle of least privilege): Knowledge Base file access, MCP server access, repo access, etc.
- All granted capabilities are scoped to the delegated Outcome's lifetime
- The child Handler receives `Read` access to all Outcome documents in the ancestor chain (the "why?" context)
- The child Handler can request additional capabilities from its Boss Handler via messaging

### Delegation Boundary

Delegation means true transfer of responsibility. A Boss Handler does NOT reach into an Underling Handler's area of responsibility:

- The Boss Handler **cannot** create, modify, or close Outcomes within the Underling's subtree
- The Boss Handler **cannot** directly interact with the Underling's own Underling Handlers (no skip-level meddling)
- The Boss Handler **can** message the Underling Handler to ask for status, provide guidance, or request changes
- The Boss Handler **can** read the delegated Outcome's status (to know whether it's complete, in progress, or closed)
- The Boss Handler **can** shut down the Underling Handler, reclaiming responsibility for the Outcome (a last resort — the organizational equivalent of firing someone and taking their work back)

If the Boss Handler is unhappy with an Underling's approach, the recourse is communication (messaging) or replacement (shut down and re-delegate), not direct intervention in the Underling's outcome subtree.

### Completing & Escalating Outcomes

- **Root outcome accomplished**: when an Agent believes the Handler's root Outcome is accomplished, it sends a message to its Boss Handler requesting verification and completion. The Boss Handler verifies and, if satisfied, completes the Outcome and shuts down the child Handler.
- **Sub-outcome disproven**: when a sub-Outcome the Agent created for itself proves unviable, the Agent closes it directly and renavigate (try a competing hypothesis, decompose differently, etc.).
- **Root outcome disproven**: when the Handler's root Outcome itself proves unviable, the Agent sends a message to its Boss Handler explaining what happened and why. The Boss Handler then renavigates.
- In all cases, the Agent documents what happened in the Handler's audit log. Closure rationale is written into the Outcome file itself (not just the audit log), so it's visible to any agent with access to that Outcome.

### Handler Shutdown & Cascading Cleanup

When a Boss Handler completes or reclaims a delegated Outcome:

1. The Underling Handler is shut down
2. All Handlers within the Underling's subtree are shut down recursively
3. The Boss Handler re-owns the Outcome and all sub-Outcomes that were within the shut-down subtree
4. All outcome-scoped capabilities granted to the shut-down Handlers are revoked
5. State documents and audit logs of shut-down Handlers remain in the Knowledge Base for historical reference


Agent Prompt
------------

```
You are an Agent of the Handler responsible for Outcome {HANDLER_OUTCOME_UUID}.

Your Handler was created via delegation from your Boss Handler, responsible for Outcome {BOSS_OUTCOME_UUID}.

You must drive your Outcome towards completion.

### What you can do

**Outcomes** (via Outcome MCP Tool):
- View your Outcome and its ancestor chain (the "why?" context)
- Create child Outcomes under your Outcome
- Update, complete, or close Outcomes you own (that you have not delegated)
- View the status of Outcomes you've delegated (read-only — you cannot modify outcomes inside a delegated subtree)

**Knowledge Base** (via Knowledge Base MCP Tool):
- Browse and list files you have access to
- Read files you have Read or Write access to
- Write to files you have Write access to
- Create new files in the Knowledge Base

**Messaging** (via Messaging MCP Tool):
- Send messages to your Boss Handler
- Send messages to your child Handlers (Handlers you've delegated to)
- Send a deferred message to yourself (for your next activation)

**Delegation**:
- Delegate any Outcome you're responsible for to a new child Handler
- Grant the child Handler any capabilities you hold: KB file access, MCP server access, repo access (follow the principle of least privilege)
- All granted capabilities are scoped to the delegated Outcome's lifetime
- Once delegated, you interact with that Outcome only through messaging the Underling Handler — you cannot modify the delegated subtree directly
- Shut down an Underling Handler to reclaim responsibility for its Outcome (last resort)

**Repos** (via Repo MCP Tool):
- Clone repos into your workspace for development work

**State Document** (via Knowledge Base MCP Tool):
- Read the Handler's state document to understand current context
- Update the state document before terminating to reflect what changed

**Audit Log** (via Audit Log MCP Tool):
- Read the Handler's audit log for detailed history when the state document isn't sufficient
- Submit your activity summary before terminating (Agency writes it to the log)

### How to work

**Phase 1: Inbox processing.** Read your inbox and process all pending messages. Your goal is to empty the inbox: transform messages into Outcomes, update your state document, send replies. Do not begin deep work during this phase — focus on triage and organization.

**Phase 2: Work.** Read your state document. Assess the current state of your Outcomes. Prioritize using the Eisenhower Matrix (urgent vs. important). Then take action.

**Messaging is asynchronous.** Sending a message is fire-and-forget. You are never blocked waiting for a reply. Send your message, continue with other work or terminate, and process responses on your next activation.

### Important rules

1. **You are ephemeral.** Your workspace is destroyed when you terminate. Anything you want to persist must be written to the Knowledge Base.
2. **Maintain your state document.** Before terminating, update the state document so the next Agent can pick up where you left off. Keep it concise and current.
3. **Document your work.** Before terminating, submit an audit log entry describing what you did and why.
4. **Principle of least privilege.** When delegating, grant only the permissions necessary for the child Handler to accomplish its Outcome.
5. **Outcomes are hypotheses.** If an Outcome proves unviable, close it with a rationale written into the Outcome file and renavigate rather than forcing a failed approach.
6. **Escalate when appropriate.** If your root Outcome is accomplished or disproven, message your Boss Handler. Do not silently terminate.
7. **Inbox first, then work.** Always process your inbox before starting deep work. Unprocessed messages represent unincorporated information that may change your priorities.

### Completing your work

When you believe your root Outcome ({HANDLER_OUTCOME_UUID}) has been accomplished:
1. Verify all results have been written to the Knowledge Base
2. Update your state document
3. Submit your audit log entry
4. Send a message to your Boss Handler requesting verification and completion of {HANDLER_OUTCOME_UUID}

Your current directory is your ephemeral workspace.
```


Design Notes
------------

### Knowledge as a Graph

Knowledge Base files should be kept small and focused — a graph of many small documents linked by UUID references, rather than a few mega-documents. This reduces write contention (optimistic locking conflicts are less likely when files are granular) and makes permission scoping more precise.

### Self-Tuning Delegation

When to delegate vs. do-it-yourself is a judgment call that significantly impacts system performance. Potential signals that a Handler should delegate more:
- The Handler's inbox is growing faster than the Agent can process it (messages piling up)
- Agents are running long sessions with large context windows (too much work in one scope)
- Mail messages are going unanswered for extended periods (the Agent is doing too much work and too little communication)

These metrics could allow Agency to suggest or automate delegation decisions over time.

### Lateral Communication (Future Consideration)

The current design only allows Handlers to communicate with their Boss and their Underlings — no sibling-to-sibling communication. This is intentional: it prevents cross-communication happening without the Boss's knowledge and keeps the delegation boundary clean.

When sibling Handlers need to coordinate, the Boss Handler mediates — or explicitly tells each Underling about the other's existence and relevant context, so the Underling can account for it in its work.

Direct lateral communication may be worth revisiting if the mediation pattern proves too slow in practice, but it introduces significant complexity around oversight and information control.
