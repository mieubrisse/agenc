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

<redaction-rules>

Sensitive Information Redaction
--------------------------------

All artifacts produced by this skill — conversation logs, bead descriptions,
and any intermediate documents — are checked into a repository that agents and
collaborators read. They exist to capture **product-relevant insights** — not
to identify individuals or expose confidential business details.

Redact aggressively. Source material (user input, journal entries, Grain
transcripts) will contain full names, company names, and confidential details.
Your job is to extract the insight and discard the identifying wrapper,
regardless of what the source contains.

The rules:

- **Use first names only.** Never include last names, email addresses, phone
  numbers, or social media handles. "Omar" is sufficient; "Omar Al-Rashid" is
  not.
- **Generalize company details.** Write "a Series B dev-tools startup" instead
  of the company's name, unless the company is already unambiguously public
  context (e.g., a well-known open-source project the person publicly
  represents, or a major tech company where the association is publicly known).
  When in doubt, generalize — over-redacting is safer than under-redacting.
- **Strip confidential business information.** Revenue figures, customer names,
  internal roadmap details, pricing, headcount, or anything the other party
  would reasonably expect to stay private. Capture the **insight** those details
  support, not the details themselves.
- **Preserve the signal.** Redaction removes identifying details, not
  analytical value. "A senior engineer at a mid-size SaaS company said
  onboarding took 3 hours" preserves the insight. "Someone said it was hard"
  does not.

<example>
<less_effective>
John Smith, CTO of Acme Corp ($12M ARR), said their team of 15 engineers
found the tmux requirement confusing. He's evaluating us against Cursor.
</less_effective>

<more_effective>
John, CTO at a mid-size dev-tools company, said his engineering team found
the tmux requirement confusing. They are evaluating alternatives in the
AI-assisted coding space.
</more_effective>
</example>

</redaction-rules>

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

Source Redaction
----------------

Before any analysis, create a **redacted source brief** — a single document
that contains all gathered material with sensitive details replaced by
descriptive tokens.

Replace each sensitive detail with a bracketed token that preserves the
analytical role of the information:

| Raw source | Redacted token |
|---|---|
| "John Smith" | "John" (first name only — drop the last name) |
| "Acme Corp" | "a mid-size dev-tools company" (generalized description) |
| "$12M ARR" | remove entirely, or "a growing company" if the scale matters |
| "their 15-person engineering team" | "their engineering team" (headcount removed) |
| "they pay $50k/year for Datadog" | "they have significant observability costs" |

The redacted brief is your working document for the rest of the pipeline. All
downstream processing — conversation analysis, strategic contextualization, log
writing, and bead creation — operates on this redacted brief, not the raw
source material. This makes it structurally impossible for sensitive details to
leak into any artifact.

Do not write the redacted brief to disk. Hold it in your working context only.

Note the redaction decisions you made (e.g., "replaced 2 company names, removed
1 revenue figure, dropped 3 last names") — you will report these during
self-verification.

---

Analysis Pipeline
-----------------

Process the **redacted source brief** in two stages:

### Stage 1: Conversation Analysis

Invoke `/conversation-analysis` with the redacted brief. This skill extracts
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

Cross-Conversation Pattern Detection
--------------------------------------

Before writing the conversation log, read all existing files in
`strategy/conversations-with-others/`. Look for patterns:

- Themes that recur across multiple conversations (e.g., "this is the third time
  someone mentioned tmux confusion")
- Points where this conversation contradicts a previous one
- Insights that reinforce or validate earlier hypotheses

Include these patterns in the conversation log under a "Cross-Conversation
Patterns" section when they exist. This turns the conversation directory from a
flat archive into a cumulative evidence base.

---

Writing the Conversation Log
-----------------------------

Write the analysis to:

```
strategy/conversations-with-others/YYYY-MM-DD_NAME.md
```

Where `YYYY-MM-DD` is the conversation date and `NAME` is a lowercase,
hyphenated identifier for the person or topic (e.g., `2026-03-10_omar-onboarding`).
Use first name only in the filename — no last names.
If a file already exists at that path (e.g., two conversations with the same
person on the same date), append a numeric suffix: `YYYY-MM-DD_NAME-2.md`.

### Required metadata header

Every log starts with this metadata block:

```markdown
Conversation Title
==================

- **Date:** YYYY-MM-DD
- **Participants:** [first names only, with generalized role/context — e.g., "Omar (senior engineer at a mid-size SaaS company)" not "Omar Al-Rashid (CTO of Acme Corp)"]
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
- Cross-conversation patterns (if detected)

Use the structure that best serves the content. A short casual conversation
needs less structure than a deep product strategy session.

---

Privacy Review
--------------

After writing the conversation log and drafting bead descriptions (but before
creating beads), spawn an independent agent using the Agent tool to audit all
artifacts for sensitive information. This agent has fresh eyes — it did not
participate in the analysis pipeline and has no sunk-cost attachment to the
content.

Use the Agent tool with this prompt (replace `<LOG_FILE_PATH>` with the actual
path and `<BEAD_DESCRIPTIONS>` with the drafted bead text):

> You are a Privacy Review Agent. Audit the following artifacts for sensitive
> information that should not appear in a checked-in repository.
>
> **Conversation log file:** Read the file at `<LOG_FILE_PATH>`.
>
> **Proposed bead descriptions:**
> <BEAD_DESCRIPTIONS>
>
> Apply these redaction rules — flag ANY violation:
>
> - **Last names, email addresses, phone numbers, or social handles.** First
>   names alone are fine.
> - **Specific company names.** Companies should be generalized to descriptive
>   phrases (e.g., "a mid-size dev-tools company") unless the company is
>   unambiguously public context (a major tech company or well-known open-source
>   project the person publicly represents). When in doubt, flag it.
> - **Confidential business details.** Revenue figures, customer names, internal
>   roadmap details, pricing, headcount, or anything the conversation
>   participant would reasonably expect to stay private.
> - **Any other identifying information** that could be combined to identify
>   a specific individual or company.
>
> Public information (open-source projects, published blog posts, conference
> talks) is fine. The goal is to keep product-relevant insights while removing
> identifying details.
>
> For each issue found, output:
> 1. Which artifact (log file or which bead description)
> 2. The exact text that should be changed
> 3. A suggested replacement that preserves the analytical value
>
> If everything is clean, say so in one sentence.
>
> Do NOT modify any files. Report only.

If the privacy agent flags issues, fix every one in both the conversation log
and the bead descriptions. Re-run the privacy review after fixes to confirm all
artifacts are clean.

---

Creating and Updating Beads
----------------------------

After the privacy review passes, create beads for each actionable item.

### User confirmation gate

Before creating or updating any beads, present the full list to the user:

> Here are the beads I plan to create/update:
>
> 1. **[NEW]** "Title here" (P2, feature) — one-line rationale
> 2. **[UPDATE beads-xyz]** "Existing title" — adding context from this conversation
>
> Proceed?

Wait for confirmation before executing. This prevents noisy beads from
accumulating in the backlog.

### When to create vs. update

Search existing beads (`bd list`) before creating new ones. If an
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
bd create \
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

Use `bd update` to append context to existing beads. Add a note
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

- [ ] Source redaction was performed before analysis (note what was redacted:
      how many names, company names, confidential details were generalized)
- [ ] Conversation log exists at the correct path with the metadata header
- [ ] Metadata header uses first names only in participants field
- [ ] `$AGENC_MISSION_UUID` is recorded in the log's metadata
- [ ] Privacy review agent ran on both the conversation log AND bead descriptions,
      and confirmed all artifacts are clean
- [ ] Every actionable item has a corresponding bead (new or updated)
- [ ] Every bead references the conversation log file path
- [ ] Every bead references the analysis mission UUID
- [ ] No duplicate beads were created for work that already had a bead
- [ ] The log captures the strategic significance of the conversation, not just
      a mechanical summary
