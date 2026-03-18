Omar AgenC Status Interview
============================

- **Date:** 2026-03-18
- **Participants:** Kevin (AgenC creator), Omar (friend, software engineer, AgenC power user since 2026-03-10)
- **Grain recording ID:** `f6973026-86d5-4b12-9f65-cd3a9a786f88`
- **Journal entry:** `omar-user-interview~2026-03-18_08-48-12~.md`
- **Analysis mission:** `53a6ed09-0f64-43ed-b1e2-0b6b00d1206d`

---

Summary
-------

- Omar has been using AgenC for ~8 days since his onboarding session and has accelerated dramatically — building out a full Obsidian-based GTD system, creating three meta-skills for Claude, and completing real projects (school website, garden planning, Antarctica game)
- Omar built three self-reflective skills: `/retrospective` (session friction analysis), `/capture-session` (writes session results to Obsidian), and `/summarize-mission` (shows what's been done and what's next for a mission). These align closely with AgenC's `/self-refine` skill and point toward automating session lifecycle management.
- The biggest pain point is **mission overload** — too many missions open, losing context about what each one is doing and whether it pushed to Git. Omar resolved this by converging on a single-mission workflow, opposite to Yannik who religiously restarts sessions.
- Omar validated the "Attach Mission" unified verb — he tested it and liked that it handles resume/foreground/switch as one concept
- Omar was very happy about orange tabs being limited to permission requests only — "not seeing those orange lozenges" was a genuine relief
- The Claude config editing confusion persists — Claude wastes tokens trying to modify its own config, hitting deny rules, retrying, etc.
- Omar explicitly requested that `/retrospective` and `/summarize-session` run automatically via AgenC rather than requiring manual invocation
- Omar completed a school website project end-to-end using the "PM not coder" workflow: 20 min of upfront prompt engineering, then 15 min of autonomous Claude work. He found `/prompt-engineer` highly valuable for this.
- Omar is having fun and finds the workflow addictive. He's exploring monetization ideas (school website business, teaching kids to code) and considering whether to take employment vs. building independently.

---

Action Items
------------

### Kevin

- [ ] Build automatic session lifecycle hooks: auto-summarize on idle, auto-retrospective on close, auto-capture session results
- [ ] Add "what's needed next" summary to the mission switcher so users can see mission status at a glance
- [ ] Elevate Claude config editing to a first-class concept with a hook that blocks direct edits and redirects to proper flow
- [ ] Add green tab color when Claude is done (requires building a polling loop to detect "user looked at tab")
- [ ] Design pluggable task management interface (`agenc task`) backed by Obsidian, Markdown, or Todoist
- [ ] Verify/enforce auto-commit and auto-push to prevent the "did it push to Git?" confusion
- [ ] Document idle-killing flow more clearly — Omar wasn't aware missions auto-kill after 30 min
- [ ] Document Ctrl+D (detach) vs Ctrl+C (close) distinction and the intended mission lifecycle
- [ ] Communicate about "Workspace Stash" palette commands and `agenc stash pop` for upgrades

### Omar

- [ ] Continue iterating on his Obsidian + Claude workflow
- [ ] Consider letting Claude bleed more into the full Obsidian system as trust builds
- [ ] Explore auto-tagging tasks via Claude

---

Themes
------

**1. Session lifecycle management is the emergent user need.** Omar independently built three skills (/retrospective, /capture-session, /summarize-mission) to solve the same core problem: "what happened, what's next, and how do I not lose context?" This is the strongest signal yet that AgenC should automate session lifecycle — summarize on idle, retrospect on close, capture results persistently. This isn't a nice-to-have; users are building it themselves.

**2. Single-mission vs. multi-mission is a workflow preference, not a design flaw.** Omar converged on single-mission because side missions introduced Git sync overhead. Yannik restarts sessions religiously for clean context. Both are valid. AgenC should support both patterns well rather than imposing one. The key enabler for multi-mission is reliable auto-commit + auto-push so users never wonder "did it push?"

**3. The GTD wedge is a real product opportunity.** Omar built his own full task management system (recurring tasks, weekly reviews, today's work, inbox, voice capture via Siri). Yannik wants something similar with his exobrain. Kevin uses Todoist. The convergent need for structured task management inside agent workflows suggests a pluggable task interface could be a differentiating feature — especially if AgenC bakes in GTD methodology, which is hard for competitors to replicate.

**4. Claude config confusion is a persistent, multi-user pain point.** Omar, Yannik, and Kevin all struggle with Claude trying to edit its own config, wasting tokens on deny-rule loops. This has appeared in every user interview. Elevating config editing to a first-class concept with proper guardrails (hooks, redirect flows) should be high priority.

**5. Prompt engineering upfront pays off dramatically.** Omar's school website project demonstrated the pattern: 20 minutes of thoughtful prompting, then 15 minutes of autonomous high-quality work. He credits `/prompt-engineer` specifically. This validates the "inputs not outputs" philosophy and suggests AgenC should make prompt engineering the default entry point for new missions.

**6. Orange-only-for-permissions is a validated UX win.** Omar was visibly relieved to not have orange tabs screaming at him. This confirms the design decision and suggests further investment in meaningful tab state (green for done, clearing after user views).

---

Agreement and Disagreement
--------------------------

### Aligned On

- Single-mission workflow is valid and should be well-supported
- Auto-summarize and auto-retrospective would be highly valuable
- Claude config editing needs to be a first-class, guarded concept
- Orange only for permissions is the right UX
- Prompt engineering upfront is worth the investment
- The "PM not coder" workflow is the aspirational model

### Diverged On

- **Mission cloning**: Kevin sees value in forking missions for retrospectives; Omar doesn't feel the need — inline retrospectives work fine for him
- **Task management approach**: Omar likes flexible Markdown files he can edit from anywhere; Kevin leans toward structured databases with dependency graphs. The answer may be "both" via a pluggable interface.

---

Insights
--------

1. **Omar's acceleration is remarkable and diagnostic.** In 8 days he went from zero to building meta-skills, completing real projects, and developing a sophisticated workflow. This validates AgenC's learning curve — once past initial onboarding friction, power users accelerate fast. The bottleneck is the first session, not the nth session.

2. **The three skills Omar built are a product roadmap.** `/retrospective` = auto-improve. `/capture-session` = persistent state. `/summarize-mission` = mission awareness. These are the three pillars of session lifecycle management. AgenC should absorb all three as built-in, automatic behaviors.

3. **"I was making side missions, but then I was losing context about whether it had pushed to Git" is the key multi-mission pain.** The fix isn't fewer missions — it's reliable auto-push. If every mission auto-commits and auto-pushes, the mental overhead of tracking Git state disappears, and multi-mission becomes viable again.

4. **Omar's Siri inbox → Claude triage pipeline is a glimpse of the future.** Voice capture → inbox file → Claude processes and categorizes → tasks appear in the system. This is the kind of ambient-capture + agent-processing workflow that AgenC could enable out of the box.

5. **Omar doesn't need mission cloning, but that's because his workflow avoids the problem.** He stays in one mission, so there's no need to fork. Users who do heavy multi-mission work (like Kevin) may still need cloning. Don't remove the feature based on one user's preference.

6. **The school website project is the best case study so far.** Real problem (9 months of governor discussion), concrete output (working website on Cloudflare for free), clear methodology (prompt engineer → autonomous work → iterative review), and a non-technical stakeholder audience. This should be featured in marketing/onboarding materials.

---

Cross-Conversation Patterns
----------------------------

Comparing with [Omar's onboarding (2026-03-10)](./2026-03-10_omar-onboarding.md) and [Charles's conversation (2026-03-12)](./2026-03-12_charles-agenc.md):

### Pattern: Claude Config Confusion is Universal (3/3 conversations)

- **Omar onboarding**: Needed hand-holding to understand config repo vs claude-config
- **Charles**: Independently built his own prompt management system partly because of config complexity
- **Omar status**: Claude wastes tokens trying to modify its own config, hitting deny rules repeatedly

**Signal:** This is the #1 recurring friction point across all users. It must be solved structurally, not with documentation.

### Pattern: Session Lifecycle Management is a Universal Need (2/3 conversations)

- **Omar status**: Built three skills to manage session state
- **Charles**: Built a "capture session" equivalent in his prompt factory

**Signal:** Users need persistent awareness of what happened, what's next, and how to resume. AgenC should provide this automatically.

### Pattern: "Inputs Not Outputs" Philosophy is Validated (3/3 conversations)

- **Omar onboarding**: Loved prompt-engineer skill
- **Charles**: Already practices this independently; was surprised it's not universal
- **Omar status**: 20-min prompt investment → 15-min autonomous high-quality work

**Signal:** This is the core philosophy that resonates with every user. It should be front and center in messaging.

### Pattern: Tab/Visual State Matters (2/3 conversations)

- **Omar status**: "Not seeing those orange lozenges" was a genuine relief; wants green for done
- **Charles**: Not discussed (he was watching a demo, not using the tool)
- **Yannik** (referenced): Also wants tab color to indicate state and clear after viewing

**Signal:** Visual mission state is a small feature with outsized UX impact. Green-for-done should be prioritized.

### Pattern: Multi-Mission Overhead is a Barrier (2/3 conversations)

- **Omar status**: Converged on single-mission because Git sync was confusing
- **Omar onboarding**: Not yet relevant (first session)
- **Charles**: His six-window Cursor setup is the same problem domain

**Signal:** Multi-mission is powerful but has a tax. Auto-push eliminates the biggest cost (Git uncertainty). Mission summaries in the switcher eliminate the second biggest (context loss).

### Evolution: Omar's Journey from Onboarding to Power User

Comparing Omar's two sessions shows clear growth:

| Dimension | Onboarding (Mar 10) | Status (Mar 18) |
|-----------|---------------------|-----------------|
| Skill level | Fighting PATH and tmux | Building meta-skills |
| Pain points | Installation, environment, tmux | Mission overload, Git sync |
| Workflow | Following Kevin's guidance | Independent, opinionated |
| Attitude | Excited but confused | Addicted, building real projects |
| Feedback | "Unclear on the workflow" | Specific feature requests |

This 8-day transformation is strong evidence that AgenC's learning curve, while steep initially, rewards investment quickly.
