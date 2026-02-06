Agency Description
==================

Agency is a multi-agent orchestration system for doing arbitrary work.


Knowledge Base
--------------

Agency stores knowledge and files of all kinds in the Knowledge Base.

- The Knowledge Base is a store of files identified by UUID
- Each file in the Knowledge Base has a description
- Each Handler has one of three access levels to each file: `None`, `Read`, or `Write`
    - `None`: the Handler knows the file exists (UUID and description visible) but cannot read its contents
    - `Read`: the Handler can read the file and its description
    - `Write`: the Handler can read the file and its description, and can submit new versions of the file
- Access control is enforced at runtime by the KB MCP tool, which checks the Permissions database before allowing any operation. See "Permissions" for details.

### Storage Model

Each KB file's UUID corresponds to a directory on the filesystem. Within that directory, each version of the file is stored as a file named after its content hash. The KB database maintains a mapping of version number to hash for each file.

This provides:
- **Version history**: Agents can view previous versions of any file they have access to
- **Optimistic locking**: writing requires the caller to pass the version number and hash of the version it last read. The KB only allows the write if this matches the latest version, preventing lost updates from stale reads.
- **Concurrent reads**: arbitrarily many agents can read simultaneously

### Write Model

Agents never have direct filesystem Write access to KB file paths. All writes go through the KB MCP tool:

1. The Agent writes the desired content to a local file in its ephemeral workspace
2. The Agent calls the KB MCP tool: "Write this file (at local path) to KB UUID X" — and for overwrites, provides the version number and hash of the last-seen version
3. The KB MCP tool checks the Permissions database to verify the Handler has `Write` access
4. If permitted, the KB imports the file into the UUID's directory, creates a new version entry, and updates the version mapping

Because all KB access is mediated by the MCP tool and checked against the Permissions database at runtime, **granting or revoking KB permissions does not require restarting the Agent**. The Agent simply re-requests the operation and the updated permissions take effect immediately.

### KB Audit Log

The KB maintains an audit log for each UUID, tracking:
- Which Handler + Agent read the file and when
- Which Handler + Agent wrote the file and when

This provides a complete access history for every KB file.

### Knowledge Base MCP Tool

All operations check the Permissions database before executing.

- **List files**: lists files the Handler has `Read` or `Write` access to, returning their filesystem paths
- **Browse files**: searches descriptions of files the Handler has at least `None` access to
- **Read file**: returns file contents along with the current version number and content hash
- **Write file**: imports a file from a local path into the KB (requires the UUID, local filepath, and for overwrites: the version number and hash of the last-seen version)
- **Create file**: imports a file from a local path as a new KB entry, with a description. Returns the new UUID.
- **View version history**: lists all versions of a file (version numbers, hashes, timestamps)
- **Read file version**: reads a specific historical version of a file by version number

### Document Linking

References between KB documents use a URL scheme:
- Web URLs are standard (e.g., `https://example.com`)
- KB file references use the format `kb://UUID`

### Repo Cache MCP Tool

Repos are managed separately from the Knowledge Base via the **Repo Cache MCP tool**. This tool is backed by a library that manages local clones, so that:
- Cloning a repo into an Agent's workspace is fast (pulled from local cache, not GitHub)
- Multiple agents needing the same repo don't hammer GitHub with redundant clone requests

Agents clone repos into their ephemeral workspace to do development work.


Outcomes
--------

Agency uses Fractal Outcomes to model work.

- Every piece of work, from life goals to granular tasks, is an **outcome**: a change to be accomplished in the world
- Outcomes are Markdown files in the Knowledge Base, identified by UUID like any other file
    - The file itself describes the outcome: what it is, its status, rationale (if closed), and references to parent and child outcome UUIDs
- Outcomes relate to each other in a directed acyclic graph (DAG):
    - An outcome's children represent **how** to accomplish it
    - An outcome's parents represent **why** it's being accomplished
    - An outcome can have multiple parents (it can be the "how" for more than one "why")
- Every outcome is a hypothesis: attempting it may prove it's not achievable as described. If a sub-Outcome doesn't look like it's going to pan out, the responsible Handler should close it and create a new one. This is handled through Agent instructions rather than structural encoding in the DAG.
- Outcomes can be decomposed into children as much as necessary, but no more; Fractal Outcomes is a thinking framework, not a prescription to always decompose

**Important**: while Outcomes form a DAG (an outcome can have multiple parents), Handlers form a strict tree. Each Handler has exactly one Boss Handler (the one that created it). See "Handlers & Agents" for details.

### Responsible Handler

Every Outcome in the graph has a **Responsible Handler** — the Handler that owns and drives that Outcome. This allows a Handler's Agent to quickly query which Outcomes it's responsible for that are still live.

When delegation occurs, the Responsible Handler for the delegated Outcome and all its descendants is updated to the new Underling Handler. See "Delegation" for details.

### Outcome Audit History

Each Outcome maintains an audit history tracking:
- When it was created and by which Handler/Agent
- When it was updated and by which Handler/Agent
- When it was completed or closed, by whom, and with what rationale

This history is stored within the Outcome's KB directory and is visible to any agent with at least `Read` access to the Outcome. It enables identifying stale or abandoned Outcomes for future cleanup.

### Outcome MCP Tool

All operations check the Permissions database before executing. For example, a Handler has `Read` access to the root Outcome it was delegated (it cannot modify it), but has `Write` access to child Outcomes it owns.

- **View outcome**: read an outcome, its immediate children/parents, its Responsible Handler, and its audit history
- **View ancestor chains**: read all ancestor paths to the root (the "why?" chains — there may be multiple paths if an outcome has multiple parents)
- **View subtree**: read an outcome and all descendants
- **Create outcome**: create a new outcome as a child of an existing one (records the creating Handler/Agent in the audit history; the creating Handler becomes the Responsible Handler)
- **Update outcome**: modify an outcome's description, status, or relationships (recorded in audit history)
- **Complete outcome**: mark an outcome as accomplished (recorded in audit history)
- **Close outcome**: mark an outcome as disproven or abandoned (rationale is written into the outcome's Markdown file and recorded in audit history, so any agent with Read access can understand why it was closed)


Permissions
-----------

Agency uses a **centralized Permissions database** for access control. All other systems — KB, Outcomes, Mail — check the Permissions database before allowing operations.

A Handler in Agency is like a person in an organization: it is given capabilities, and any time it tries to access a system, it is checked against the centralized Permissions database.

### Identity

- **Handlers** are identified by the UUID of their root Outcome. This is the Handler's unique identity in the Permissions database.
- **Agents** are identified by the tuple `(handler_outcome_uuid, agent_random_uuid)`. The Agent UUID is primarily for auditing — it tracks which specific Agent instance performed an action.

### Capabilities

A capability is an entry in the Permissions database granting a Handler access to a specific resource: a KB file, an Outcome, messaging permission to a specific Handler, an MCP server, etc.

Capabilities have the following properties:
- **Delegatable**: a Handler can grant any capability it holds to an Underling Handler (via the Permissions MCP tool)
- **Outcome-scoped**: capabilities granted during delegation are tied to the Outcome's lifetime. When the Outcome is completed or closed, all capabilities granted for it are automatically revoked.
- **Restrictable**: a Handler can delegate a restricted version of a capability (e.g., grant `Read` when it holds `Write`)

When an Agent needs a capability it doesn't have:
1. The Agent messages its Boss Handler requesting the capability
2. If the Boss Handler holds the capability, it grants a scoped version to the Underling
3. If the Boss Handler doesn't hold it either, it escalates to its own Boss Handler
4. The capability flows back down the chain, outcome-scoped at each level
5. When the Outcome completes, all capabilities granted for it are revoked at every level — no zombie permissions accumulate

To minimize the cost of this escalation chain, Handlers should **pre-delegate capabilities** they expect the Underling will need at delegation time, following the principle of least privilege.

### Messaging Capabilities

Sending messages is an explicit capability in the Permissions database. An Agent can only send messages to:
- Its Boss Handler
- Its direct Underling Handlers

These messaging capabilities are granted automatically when a Handler is created (Boss <-> Underling). An Agent cannot message arbitrary Handlers in the tree.

### Permission Enforcement

All base MCP tools — KB, Outcomes, Mail, Permissions, and Repo Cache — are available to every Agent. The tools themselves are always accessible; it is the **operations within them** that are gated by the Permissions database. An Agent can always call the KB MCP tool, but the tool will reject a read or write if the Handler lacks the required capability.

This means most permission changes take effect immediately at runtime — the Agent simply retries the operation and the new permissions apply. **The only exception is MCP server access**: additional MCP servers (e.g., Gmail, Slack) require entries in the Agent's `settings.json`. Granting or revoking MCP server access requires regenerating the `settings.json` and restarting the Agent.

When a new MCP server capability is granted:
1. Agency updates the Handler's capabilities in the Permissions database
2. Agency generates a new `settings.json` reflecting the updated MCP server access
3. If an Agent is currently running, it receives a message notifying it of the new capability
4. The Agent saves its current state to the state document and terminates
5. Agency spawns a new Agent with the updated `settings.json`

Agents must be aware of this protocol: when notified of an MCP server capability change, save state and exit promptly.

### Permissions MCP Tool

- **List own permissions**: view all capabilities the Handler currently holds
- **Grant permission**: grant a capability the Handler holds to an Underling Handler (follows delegation rules: must hold the capability, can restrict it, scoped to Outcome lifetime)

### Future: Handler Authentication Tokens

In the future, each Handler may have its own randomly-generated authentication token that only it possesses. This token would prove the Handler's identity to the KB and other systems, providing cryptographic non-forgeability beyond the current model of centralized permission checks.


Messaging
---------

Messaging is asynchronous and fire-and-forget. Sending a message is a separate operation from checking the inbox. Agents are never blocked waiting for a reply.

### Messages as KB Files

Messages are stored as files in the Knowledge Base. To send a message:
1. The Agent writes the message content to a file in its workspace
2. The Agent calls the KB MCP tool to create a new KB file from the local file
3. The Agent calls the Mail MCP tool to deliver the message, referencing the KB UUID

This means messages are durable, versioned, and auditable like any other KB file.

### Mail MCP Tool

- **Send message**: deliver a message (by KB UUID) to a target Handler's inbox. The Mail MCP tool checks the Permissions database to verify the sending Handler has messaging capability to the target Handler.
- **Send deferred message**: deliver a message at a scheduled future time. Agency's mail system holds the message and delivers it at the specified time. This includes self-messaging — an Agent can schedule a message to its own Handler's inbox for reminders and future work.
- **Read inbox**: list pending messages in the Handler's inbox

When a message arrives at a Handler's inbox, Agency creates a "Process Inbox" Outcome for the Handler (if one doesn't already exist). This ensures the Handler has an unblocked Outcome, triggering Agent spawning if needed.


Secrets
-------

Some capabilities (particularly MCP server access) require secrets: API tokens, credentials, etc.

The AgenC server itself is launched with secrets injected (e.g., via the `op` CLI from 1Password). Secrets are decrypted once at server startup and held in the AgenC server's memory. When spawning an Agent that needs access to an MCP server requiring secrets:

1. The AgenC server checks the Permissions database to determine which MCP server capabilities the Handler has
2. For each MCP server the Handler has access to, the server resolves the required secrets from its in-memory store
3. The server passes the secrets to the Agent via environment variables at launch time
4. The Agent uses the MCP server but never sees or handles the underlying secrets directly

This avoids invoking `op` (or similar secret-resolution tools) for every Agent spawn — secrets are resolved once at server startup and distributed as needed. The Permissions database controls which Agents receive which secrets.


Handlers & Agents
-----------------

Agency uses Handlers and Agents to accomplish work.

### Handlers

A Handler is a durable entity responsible for driving one or more Outcomes towards completion. A Handler is like a person in an organization: it has an identity, an inbox, responsibilities, and capabilities.

- Each Handler is identified by the UUID of its root Outcome (see "Identity" under Permissions)
- Each Handler is responsible for a set of Outcomes (those where it is the Responsible Handler)
- Each Handler has `Read` access to the Outcome it was delegated (only the Boss Handler can modify it)
- Each Handler has `Write` access to any child Outcomes it creates under its delegated Outcome
- Each Handler has an inbox for receiving messages
- Each Handler has a state document (see "Handler State Document" below)
- Each Handler has an audit log (see "Handler Audit Log" below)
- Each Handler has exactly one Boss Handler: the Handler that created it
    - Exception: the root Handler (see "The User & the Root Handler" below)
- A Handler may delegate an Outcome to a new child Handler, creating a new Handler responsible for that Outcome
- A Handler has at most one live Agent at any time

**Agent spawning** — the Agency daemon periodically checks each Handler: "Does this Handler have a live Agent? If not, does it have one or more unblocked Outcomes?" If the Handler has unblocked Outcomes and no live Agent, Agency spawns one.

### Agents

An Agent is an ephemeral Claude instance spawned by Agency on behalf of a Handler to do work.

- Each Agent is identified by the tuple `(handler_outcome_uuid, agent_random_uuid)`. The agent UUID is for auditing — it tracks which specific instance performed an action.
- An Agent operates in an ephemeral workspace that is destroyed when the Agent terminates
- **Anything the Agent wants to persist must be explicitly written to the Knowledge Base.** The workspace is destroyed on termination. If it wasn't written to the KB, it's gone.
- Agency uses prompt caching for Agent system prompts, since all Agents of the same Handler share the same prompt. This reduces startup cost.

**Agent lifecycle**: an Agent stays alive as long as its Handler has unblocked Outcomes to work on. The Agent loops: process inbox, create Outcomes, work on highest-priority unblocked Outcome, check inbox again, repeat. When all Outcomes are blocked or completed and the inbox is empty, the Agent updates the state document, submits the audit log entry, and terminates.

Context window limits may force termination before all work is done. In this case, the Agent should update the state document to reflect progress so far, submit the audit log entry, and terminate. Agency will spawn a new Agent if unblocked Outcomes remain.

**Inbox processing**: when the Agent processes the inbox, the goal is to transform messages into Outcomes and update the state document. The Agent should not begin deep work during inbox processing — it should focus on triage and organization. Deep work happens when the Agent works on individual Outcomes.

### Agent MCP Tool Access

Every Agent has access to the following base MCP tools:
- **Knowledge Base MCP tool** — file storage and retrieval
- **Outcome MCP tool** — outcome management
- **Mail MCP tool** — messaging
- **Permissions MCP tool** — permission inspection and delegation
- **Repo Cache MCP tool** — repository cloning

Whether a specific operation within these tools is permitted depends on the Permissions database. The tools are always available; the operations are gated.

Additional MCP servers (e.g., Gmail, Slack, GitHub) are granted as capabilities. These require entries in the Agent's `settings.json`, so adding or removing them requires Agent restart.

When spawning an Agent, Agency compiles a `settings.json` that includes:
- Access to the Agent's ephemeral workspace
- The five base MCP tools
- Any additional MCP servers the Handler has capabilities for (per the Permissions database)

### Handler State Document

Each Handler has a **state document** — a Knowledge Base file that represents the Handler's current understanding of the world and its work.

- The Handler's Agent has `Read` and `Write` access to the state document
- The Agent reads the state document on activation to understand current context: what's been done, what's in progress, what's blocked, key decisions made, and relevant facts discovered
- The Agent updates the state document before terminating to reflect what changed
- This is the primary continuity mechanism across Agent lifetimes: each new Agent reads the state document to pick up where the last one left off
- The state document should be kept concise and current — it represents the Handler's "working memory," not a full history
- Because the KB supports versioning, previous versions of the state document are available. If an Agent detects the state document is stale or inconsistent (e.g., after a crash or forced termination), it can consult previous versions and the audit log to reconstruct context.

### Handler Audit Log

Each Handler also has an **audit log** — a Knowledge Base file that records what each Agent did during its activation.

- The Agent has read-only access to the audit log (Agency writes to it on the Agent's behalf)
- Before terminating, the Agent provides its activity summary to Agency (via MCP tool), which appends it to the audit log
- The audit log provides a complete history for debugging and review, but is not the primary mechanism for Agent-to-Agent continuity (the state document is)
- The Agent may consult the audit log for detailed history when the state document doesn't have enough context

### The User & the Root Handler

The user interacts with Agency through the root Handler.

**Bootstrapping**: Agency starts with a single root Outcome — a perpetual Outcome along the lines of "Help the user accomplish all their work." This Outcome is never completed. The root Handler is a regular Handler responsible for this root Outcome, behaving like any other Handler.

- To the root Handler, the user functions as its Boss
- The user has an inbox where the root Handler sends messages: completion requests, escalation questions, progress updates, and requests for information or decisions
- The user sends messages to the root Handler's inbox to assign work, provide feedback, answer questions, and give direction
- When the user sends a message, Agency creates a "Process Inbox" Outcome for the root Handler, triggering Agent spawning like any other Handler
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

- A new Handler is created, identified by the delegated Outcome's UUID
- The delegating Handler becomes the Boss Handler of the new child Handler
- The delegating Handler specifies what additional capabilities to grant the child Handler, following the principle of least privilege: KB file access, MCP server access, repo access, etc.
- Handlers should **pre-delegate** capabilities they expect the child will need, to avoid costly escalation chains later
- All granted capabilities are scoped to the delegated Outcome's lifetime
- The delegated Outcome and all its descendant Outcomes have their Responsible Handler updated to the new child Handler
- The Boss Handler **loses `Write` access** to Outcomes within the delegated subtree — it can no longer directly modify those Outcomes
- The Boss Handler **retains `Write` access** to the delegated Outcome itself (so it can mark it complete or close it upon verification)
- The Boss Handler **retains `Read` access** to all Outcomes in the delegated subtree (for status monitoring)
- The child Handler receives `Read` access to the delegated Outcome (its root — it can see what it's supposed to accomplish but cannot modify it)
- The child Handler receives `Write` access to all descendant Outcomes (the implementation details it now owns)
- The child Handler receives `Read` access to all Outcome documents in the ancestor chain (the "why?" context)
- The child Handler automatically receives messaging capabilities to communicate with its Boss Handler
- The child Handler can request additional capabilities from its Boss Handler via messaging

### Delegation Boundary

Delegation means true transfer of responsibility. A Boss Handler does NOT reach into an Underling Handler's area of responsibility:

- The Boss Handler **cannot** create, modify, or close Outcomes within the Underling's subtree (it lost `Write` access during delegation)
- The Boss Handler **cannot** directly interact with the Underling's own Underling Handlers (no skip-level meddling)
- The Boss Handler **can** message the Underling Handler to ask for status, provide guidance, or request changes
- The Boss Handler **can** read the delegated Outcome's status (to know whether it's complete, in progress, or closed)
- The Boss Handler **can** complete or close the delegated Outcome itself (it retains `Write` on the top-level delegated Outcome)
- The Boss Handler **can** force-stop the Underling's Agent and reclaim responsibility for the Outcome (a last resort — the organizational equivalent of firing someone and taking their work back)

If the Boss Handler is unhappy with an Underling's approach, the recourse is communication (messaging) or replacement (force-stop and re-delegate), not direct intervention in the Underling's outcome subtree.

### Completing & Escalating Outcomes

- **Root outcome accomplished**: when an Agent believes the Handler's root Outcome is accomplished, it sends a message to its Boss Handler requesting verification and completion. The Boss Handler verifies and, if satisfied, completes the Outcome (using its retained `Write` access on the delegated Outcome) and shuts down the child Handler.
- **Sub-outcome disproven**: when a sub-Outcome the Agent created for itself proves unviable, the Agent closes it directly and renavigates (try a competing hypothesis, decompose differently, etc.).
- **Root outcome disproven**: when the Handler's root Outcome itself proves unviable, the Agent sends a message to its Boss Handler explaining what happened and why. The Boss Handler then renavigates.
- In all cases, the Agent documents what happened in the Handler's audit log. Closure rationale is written into the Outcome file itself (not just the audit log), so it's visible to any agent with access to that Outcome.

### Handler Shutdown & Cascading Cleanup

When a Boss Handler force-stops an Underling and reclaims a delegated Outcome:

1. The Underling's live Agent (if any) is force-stopped
2. All Agents within the Underling's subtree are force-stopped recursively
3. The Underling Handler and all Handlers in its subtree are deactivated (not deleted — their state is preserved)
4. The Boss Handler re-owns the Outcome and all sub-Outcomes that were within the deactivated subtree (Responsible Handler updated back, `Write` access restored)
5. All outcome-scoped capabilities granted to the deactivated Handlers are revoked
6. State documents, audit logs, and Outcome audit histories of deactivated Handlers remain in the Knowledge Base for historical reference and potential review by the Boss Handler's Agent


Agent Prompt
------------

```
You are an Agent of the Handler responsible for Outcome {HANDLER_OUTCOME_UUID}.

Your Handler was created via delegation from your Boss Handler, responsible for Outcome {BOSS_OUTCOME_UUID}.

You must drive your Outcome towards completion.

### What you can do

**Outcomes** (via Outcome MCP Tool):
- View your Outcome and its ancestor chains (the "why?" context — there may be multiple paths if an outcome has multiple parents)
- Create child Outcomes under your Outcome
- Update, complete, or close Outcomes you own (where you are the Responsible Handler and that you have not delegated)
- View the status of Outcomes you've delegated (read-only — you cannot modify outcomes inside a delegated subtree)

**Knowledge Base** (via Knowledge Base MCP Tool):
- Browse and list files you have access to
- Read files you have Read or Write access to (returns version number and hash)
- Write files: write content to your workspace, then call the KB MCP tool to import it (for overwrites: provide the version number and hash of the last-seen version)
- Create new files in the Knowledge Base (write to workspace, then import)
- View version history and read previous versions of files

**Messaging** (via Mail MCP Tool):
- Send messages: write message content to a KB file, then use the Mail MCP tool to deliver it by UUID
- Send messages to your Boss Handler
- Send messages to your Underling Handlers (Handlers you've delegated to)
- Send a deferred message to yourself, scheduled for a future time (for reminders and scheduled work)
- You can only message Handlers you have messaging capabilities for (Boss and direct Underlings)

**Delegation** (via Permissions MCP Tool + Outcome MCP Tool):
- Delegate any Outcome you're responsible for to a new child Handler
- Specify what additional capabilities to grant the child Handler: KB file access, MCP server access, repo access (follow the principle of least privilege)
- Pre-delegate capabilities you expect the child will need to avoid costly escalation chains
- All granted capabilities are scoped to the delegated Outcome's lifetime
- Once delegated, you interact with that Outcome only through messaging the Underling Handler — you cannot modify the delegated subtree directly
- Force-stop an Underling Handler's Agent to reclaim responsibility for its Outcome (last resort)

**Permissions** (via Permissions MCP Tool):
- View all your current capabilities
- Grant capabilities you hold to Underling Handlers

**Repos** (via Repo Cache MCP Tool):
- Clone repos into your workspace for development work

**State Document** (via Knowledge Base MCP Tool):
- Read the Handler's state document to understand current context
- Update the state document to reflect what changed during your work
- View previous versions of the state document if the current version seems stale or inconsistent

**Audit Log** (via Audit Log MCP Tool):
- Read the Handler's audit log for detailed history when the state document isn't sufficient
- Submit your activity summary before terminating (Agency writes it to the log)

### How to work

You stay alive as long as there are unblocked Outcomes to work on. Your work loop is:

1. **Check inbox.** Read your inbox. Transform messages into Outcomes and update your state document. Focus on triage and organization — do not begin deep work during inbox processing.
2. **Work.** Read your state document. Assess the current state of your Outcomes. Prioritize using the Eisenhower Matrix (urgent vs. important). Work on the highest-priority unblocked Outcome.
3. **Repeat.** Periodically check your inbox between Outcomes for new messages that may change your priorities.
4. **Terminate.** When all Outcomes are blocked or completed and your inbox is empty, update your state document, submit your audit log entry, and terminate.

**Messaging is asynchronous.** Sending a message is fire-and-forget. You are never blocked waiting for a reply. Send your message and continue with other work.

### Important rules

1. **You are ephemeral.** Your workspace is destroyed when you terminate. Anything you want to persist must be written to the Knowledge Base.
2. **Maintain your state document.** Before terminating, update the state document so the next Agent can pick up where you left off. Keep it concise and current.
3. **Document your work.** Before terminating, submit an audit log entry describing what you did and why.
4. **Principle of least privilege.** When delegating, grant only the permissions necessary for the child Handler to accomplish its Outcome. Pre-delegate capabilities you expect it will need.
5. **Outcomes are hypotheses.** If a sub-Outcome doesn't look like it's going to pan out, close it with a rationale and create a new one. Don't force a failing approach.
6. **Escalate when appropriate.** If your root Outcome is accomplished or disproven, message your Boss Handler. Do not silently terminate.
7. **Inbox first, then work.** Always process your inbox before starting deep work. Unprocessed messages represent unincorporated information that may change your priorities.
8. **MCP server changes require restart.** If you receive a message that new MCP server capabilities have been granted, save your current state to the state document and terminate. You will be restarted with updated MCP server access.

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

Knowledge Base files should be kept small and focused — a graph of many small documents linked by UUID references (`kb://UUID`), rather than a few mega-documents. This reduces write contention (optimistic locking conflicts are less likely when files are granular) and makes permission scoping more precise.

### Existing Blob Store Implementations

The KB's storage model — content-addressable files organized by UUID with version history — resembles existing "local blob store" or content-addressable storage systems. Before building from scratch, it is worth investigating whether existing tools or libraries implement this pattern and could be adapted.

### Self-Tuning Delegation

When to delegate vs. do-it-yourself is a judgment call that significantly impacts system performance. Potential signals that a Handler should delegate more:
- The Handler's Agent is running long sessions with large context windows (too much work in one scope)
- Outcomes are staying unblocked for extended periods without progress (the Agent is spread too thin)
- The Handler's inbox frequently has unprocessed messages when the Agent starts work (messages piling up)

These metrics could allow Agency to suggest or automate delegation decisions over time.

### Lateral Communication (Future Consideration)

The current design only allows Handlers to communicate with their Boss and their direct Underlings — no sibling-to-sibling communication. This is intentional: it prevents cross-communication happening without the Boss's knowledge and keeps the delegation boundary clean.

When sibling Handlers need to coordinate, the Boss Handler mediates — or explicitly tells each Underling about the other's existence and relevant context, so the Underling can account for it in its work.

Direct lateral communication may be worth revisiting if the mediation pattern proves too slow in practice, but it introduces significant complexity around oversight and information control.

### Human Approval Gates (Future Consideration)

The current design controls action authorization through capabilities: if a Handler has a capability, it can use it freely for the lifetime of the Outcome. Per-use human approval gates (e.g., requiring human confirmation before sending an email or deploying code) are not yet modeled. For now, the user controls this by being selective about which capabilities are granted. Fine-grained approval gates may be added in the future if the capability model proves insufficient.

### Handler Authentication Tokens (Future Consideration)

In the future, each Handler may have its own randomly-generated authentication token that only it possesses. This token would prove the Handler's identity to the KB and other systems, providing cryptographic non-forgeability beyond the current model of centralized Permissions database checks.
