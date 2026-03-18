---
name: process-conversation-with-other
description: >-
  Processes a conversation the founder had with another person about AgenC into
  structured artifacts: a conversation log in strategy/conversations-with-others/
  and beads capturing actionable work. Invoke when the user says they talked to
  someone, had a meeting, or wants to process a conversation into the repo.
argument-hint: "[freetext description of the conversation, e.g. 'I talked to Omar today about onboarding friction']"
---

Process Conversation With Other
================================

You turn conversations the founder had with other people — about AgenC, its
direction, user feedback, or product strategy — into two artifacts:

1. A **conversation log** in `strategy/conversations-with-others/`
2. **Beads** (new or updated) that capture actionable work with full context

Your job is to extract signal from the conversation and set up future work with
rich context. You do NOT implement changes, write code, or update documentation
yourself. You produce the artifacts that let future agents do that work well.

---

Gathering Source Material
-------------------------

The user provides a freetext description of the conversation. This is your
starting point, but rarely the complete picture. The full details are usually
recorded in two places:

<sources>
<source name="personal-journal">
Invoke `/personal-journal` to search for journal entries related to the
conversation. Search by the other person's name, the date, and any topic
keywords the user mentioned. Journal entries contain the founder's raw notes,
observations, and reflections.
</source>

<source name="grain">
Spawn a mission to the `mieubrisse/grain-manager` repo to search for and
retrieve the meeting recording. Include the participant's name, approximate
date, and any topic keywords in the mission prompt so the Grain agent can find
the right meeting. Ask it to return the transcript and any AI-generated notes.

```bash
agenc mission new mieubrisse/grain-manager --headless --prompt "Find the Grain meeting recording for a conversation with [NAME] around [DATE] about [TOPICS]. Return the full transcript and any AI-generated meeting notes."
```

Wait for the Grain mission to complete (`agenc mission inspect <id>` — look for
IDLE status), then read its output via `agenc mission print <id>`. Grain
transcripts capture what was actually said — they are the highest-fidelity
source.
</source>
</sources>

Gather from both sources before proceeding. If one source yields nothing, that
is fine — work with what you have. If neither source has material beyond the
user's freetext description, proceed with that alone but note the limited source
material in the log.

---

Analysis Pipeline
-----------------

Process the gathered material in two stages:

### Stage 1: Conversation Analysis

Invoke `/conversation-analysis` with the gathered material. This skill extracts
structured insights: summary, action items, themes, points of agreement and
disagreement, and direct quotes.

### Stage 2: Strategic Contextualization

Invoke `/creative-director` with the output from Stage 1. The Creative Director
interprets the conversation through the lens of AgenC's product direction —
what does this conversation mean for positioning, target market, ICP validation,
competitive landscape, or narrative arc? The CD identifies which insights are
strategically significant and how they connect to existing strategy.

The CD operates in subagent mode here: it provides definitive answers from
established conventions rather than entering a debate. If the conversation
reveals something that conflicts with or extends current strategy, the CD flags
it for founder review.

---

Writing the Conversation Log
-----------------------------

Write the analysis to:

```
strategy/conversations-with-others/YYYY-MM-DD_NAME.md
```

Where `YYYY-MM-DD` is the conversation date and `NAME` is a lowercase,
hyphenated identifier for the person or topic (e.g., `2026-03-10_omar-onboarding`).
If a file already exists at that path (e.g., two conversations with the same
person on the same date), append a numeric suffix: `YYYY-MM-DD_NAME-2.md`.

### Required metadata header

Every log starts with this metadata block:

```markdown
Conversation Title
==================

- **Date:** YYYY-MM-DD
- **Participants:** [who was in the conversation and brief context for each]
- **Grain recording:** [URL if available, or "N/A"]
- **Journal entry:** [journal entry identifier if found, or "N/A"]
- **Analysis mission:** `$AGENC_MISSION_UUID` (AgenC mission that produced this analysis — agents can read the full conversation transcript via `agenc mission print`)
```

The analysis mission UUID is the current mission's UUID (`$AGENC_MISSION_UUID`).
This gives future agents a handle to the full analytical context.

### Body structure

The body structure is flexible — adapt it to what the conversation contains.
Aim to capture:

- What happened (summary)
- What was decided or agreed upon
- Where there was disagreement or tension
- Actionable items that emerged
- Strategic implications (from the Creative Director's analysis)
- Notable direct quotes that capture sentiment or insight
- Themes and patterns

Use the structure that best serves the content. A short casual conversation
needs less structure than a deep product strategy session.

---

Cross-Conversation Pattern Detection
--------------------------------------

Before writing the new log, read all existing files in
`strategy/conversations-with-others/`. Look for patterns:

- Themes that recur across multiple conversations (e.g., "this is the third time
  someone mentioned tmux confusion")
- Points where this conversation contradicts a previous one
- Insights that reinforce or validate earlier hypotheses

Surface these patterns in the new conversation log under a "Cross-Conversation
Patterns" section when they exist. This turns the conversation directory from a
flat archive into a cumulative evidence base.

---

Creating and Updating Beads
----------------------------

After writing the conversation log, prepare beads for each actionable item.

### User confirmation gate

Before creating or updating any beads, present the full list to the user:

> Here are the beads I plan to create/update:
>
> 1. **[NEW]** "Title here" (P2, feature) — one-line rationale
> 2. **[UPDATE agenc-xyz]** "Existing title" — adding context from this conversation
>
> Proceed?

Wait for confirmation before executing. This prevents noisy beads from
accumulating in the backlog.

### When to create vs. update

Search existing beads (`br --no-db list`) before creating new ones. If an
existing bead already covers the work item, update it with the new context
rather than creating a duplicate.

### Signal strength

Annotate each bead's description with the evidence strength behind it:

- `[VALIDATED]` — multiple independent conversations or users confirmed this
- `[SINGLE-SIGNAL]` — one person's observation or opinion (most common)
- `[HYPOTHESIS]` — inferred from the conversation but not directly stated

This helps downstream agents prioritize work backed by strong evidence over
speculative items.

### Priority mapping

Map strategic significance from the Creative Director's analysis to bead
priority:

| CD assessment | Bead priority |
|---------------|---------------|
| Blocks adoption or retention | P0 |
| Strategically significant, clear next step | P1 |
| Valuable but not urgent | P2 |
| Nice-to-have or speculative | P3 |

### Bead template

Use this structure for every new bead:

```bash
br --no-db create \
  --title "Concise, actionable title" \
  --type feature \
  --priority 2 \
  --labels "relevant,labels" \
  --description "$(cat <<'EOF'
[What]: What needs to be done, specifically.
[Why]: Why this matters — the user pain, strategic value, or risk it addresses.
[Signal]: SINGLE-SIGNAL | VALIDATED | HYPOTHESIS
[Source]: strategy/conversations-with-others/YYYY-MM-DD_name.md
[Analysis mission]: <$AGENC_MISSION_UUID>
EOF
)"
```

The `[Source]` and `[Analysis mission]` fields are mandatory. They give the
future executing agent a direct handle to the full context.

### Bead updates

Use `br --no-db update` to append context to existing beads. Add a note
referencing the new conversation that reinforced or expanded the work item.
If a bead's signal strength should be upgraded (e.g., from `SINGLE-SIGNAL`
to `VALIDATED` because a second person confirmed it), update that too.

---

Scope Boundary
--------------

This skill produces conversation logs and beads. It does NOT:

- Write or modify code
- Update documentation or README files
- Directly edit strategy files (the Creative Director may update its own strategy
  files during Stage 2 — that is its domain, not yours)
- Create plans or implementation specs
- Spawn missions to do the work

The actionable work lives in the beads. Other agents pick up those beads and
execute. Your job is to make those beads so well-contextualized that the
executing agent can work autonomously.

---

Self-Verification
-----------------

Before finishing, verify:

- [ ] Conversation log exists at the correct path with the metadata header
- [ ] `$AGENC_MISSION_UUID` is recorded in the log's metadata
- [ ] Every actionable item has a corresponding bead (new or updated)
- [ ] Every bead references the conversation log file path
- [ ] Every bead references the analysis mission UUID
- [ ] No duplicate beads were created for work that already had a bead
- [ ] The log captures the strategic significance of the conversation, not just
      a mechanical summary
