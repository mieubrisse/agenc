May — Claude Cowork User Interview
===================================

- **Date:** 2026-03-06
- **Participants:** Kevin (AgenC founder, interviewing), May (Product Marketing Manager, personal Claude subscriber, AI power user)
- **Grain recording:** ID `13721a22-a9e0-4aa5-89d3-2059af46a032` — "Kevin Today <> May & Kevin Claude Cowork" (63 min)
- **Journal entry:** `may-claude-cowork-user-interview~2026-03-06_15-57-34~.md`
- **Analysis mission:** `e8fc5a56-0891-4260-89dc-7a2b6c91986c` (AgenC mission that produced this analysis — agents can read the full conversation transcript via `agenc mission print`)

---

Summary
-------

- May is a Product Marketing Manager who uses Claude extensively for both personal development and work, with a personal subscription she manages carefully due to credit consumption concerns
- She finds Claude Cowork to be "an awkward in-between" — not autonomous enough to be a real agent, not simple enough to justify over vanilla Claude. Its core value prop (file access and modification) is something she doesn't need
- May independently developed a sophisticated meta-refinement practice: end-of-day sessions where she asks Claude to summarize what it learned about her and output improved instructions for future sessions. This is structurally identical to AgenC's `/self-refine` workflow
- Her deepest unmet need is prioritization, not execution — "I don't have a good enough algorithm for structuring everything that's in my head into a logical, tactical manner"
- She wants to build a personal assistant called "April" that captures random thoughts via SMS/WhatsApp, categorizes them P0-P3, puts them on her calendar, and sends reminder notifications
- She expressed burnout from the pace of AI feature releases: "I want to feel good about my ability to use AI, but if you keep moving the goalposts..."
- She hopes an agent can "help me destress" — she doesn't think all the things she's doing are the right things to get where she wants to go
- Kevin showed a brief AgenC demo at the end; May asked about credit usage and context refinement efficiency before needing to leave

---

Action Items
------------

### Kevin
- [ ] Follow up with May after developing the personal assistant / prioritization angle further
- [ ] Consider May as a potential early user for a "download and customize" agent offering

---

Themes
------

**1. The sophistication gap.** May is doing remarkably advanced things with vanilla Claude — persistent context across sessions, meta-refinement loops, Projects as long-term AI partnerships — yet doesn't consider herself proficient: "I don't think I'm at a point where I'm the most proficient at driving the LLM to perform to a 9/10." She independently arrived at the "inputs not outputs" approach without any exposure to AgenC or its philosophy.

**2. AI as trusted partner, not productivity tool.** May's primary use case isn't task completion — it's building a relationship with an AI that understands her increasingly well over time. Her Projects (Mental Health Advisors, Investment Advisor) are long-running partnerships. She records call transcripts and asks Claude "what did you learn about me from this." This is fundamentally different from the engineer-focused "agent as employee" framing.

**3. The formatting vs. content trap.** May repeatedly described getting stuck on formatting requirements (headings, layouts, Notion templates, hyperlinks) before content exists. She recognizes this: "It's like you're giving instruction on formatting, when the content doesn't even exist yet." This tension between precision and productivity is a core friction point in her AI usage.

**4. The integrity hesitation.** May feels uncomfortable completing tasks entirely through AI: "I don't feel completely comfortable if I finish an entirely task without fine-tuning something myself... it feels a little 'cheaty'... it's almost an integrity thing." This psychological barrier limits how much she delegates to AI tools.

**5. Overwhelm and feature fatigue.** May explicitly expressed burnout from AI release pace: "I'm quite overwhelmed with all these new features... part of me feels burnt-out by it... it's like it's too fast!" She wants to feel competent with AI tools, but the goalposts keep moving.

**6. The prioritization crisis.** May's deepest need isn't a better AI tool — it's help deciding what to focus on. "I don't think all the things I'm doing right now are the RIGHT things to get me where I want to go." Her personal TODO management is scattered across Apple Notes, and she's "not super great" at maintaining it. She needs something that helps her prioritize, not just execute.

---

Agreement and Disagreement
--------------------------

### Aligned On

- AI interactions should produce compounding value over time (May's meta-refinement loop = Kevin's "inputs not outputs" philosophy)
- There's a gap between what current AI tools offer and the "trusted personal assistant" experience
- Plugins and pre-built workflows are less valuable than raw prompting ability for sophisticated users

### Diverged On

- **Agent autonomy**: Kevin's AgenC vision involves autonomous background agents running missions. May explicitly said "I don't have anything in my life that I need to optimize for" regarding agents, and "I'm not buying a Macbook Mini to run OpenClaw." She wants a simpler, more personal tool — not an agent factory.
- **Target user assumption**: May's needs point toward personal life management, not developer productivity. This challenges AgenC's current positioning.

---

Insights
--------

1. **May independently built AgenC's self-refine workflow.** Her end-of-day routine — asking Claude to summarize what it learned and output improved instructions — is structurally identical to `/self-refine`. She built this without any exposure to AgenC. Combined with Charles building a similar "prompt factory," this is now three independent data points that the meta-refinement concept is fundamental, not niche.

2. **May might be an ideal user for a different product than AgenC's current form.** Kevin's own note says "She might be my ideal user!!!" — but her needs point toward a personal life management assistant (prioritization, thought capture, behavioral nudges), not agent orchestration for code. The product she wants is "April," not AgenC.

3. **The "April" concept is remarkably well-defined.** May described: SMS/WhatsApp capture → categorization by P0-P3 → calendar integration → reminder notifications. This is a concrete product spec that maps well to MCP integrations (Todoist, calendar) plus persistent Claude context. The gap isn't knowing what she wants — it's knowing how to build it.

4. **"I need to keep surfacing things to ask, 'is this worth prioritizing right now?'" is the killer insight.** May doesn't need help doing things — she needs help deciding what to do. This is a fundamentally different product category from task execution. It's closer to a life coach than a coding assistant.

5. **Cowork's failure to land is diagnostic.** May — a paying Claude subscriber who tried Cowork voluntarily — summarized it as "an awkward in-between." Its core feature (file modification) is something she doesn't need. If AgenC's non-technical offering lands similarly — more powerful than Claude but requiring too much setup — it will face the same rejection.

6. **May has a trust ceiling she's aware of.** "I have a tendency to favor Claude's response" — she explicitly worries about over-relying on Claude. This self-awareness about AI dependence is unusual and suggests she'd value guardrails against over-delegation.

7. **Feature fatigue is a positioning opportunity.** May's burnout from AI releases suggests a differentiation angle: position as "the one thing you need" rather than "another AI tool." This aligns with the anti-complexity positioning.

---

Notable Quotes
--------------

- "Before I go to bed, asking Claude to figure out what it's learning, outputting a set of instructions, that would make the next chat experience a little better" — on her meta-refinement practice
- "It's working... it's getting to the point where it would understand me a little better" — on the compounding value of her approach
- "Cowork is an awkward in-between... it's neither an agent that runs things in the background, and I think I could get a majority of what I need from vanilla Claude" — on why Cowork doesn't land
- "I don't have a good enough algorithm for structuring everything that's in my head into a logical, tactical manner" — her core unmet need
- "I'm hoping to have an agent to help me destress" — the emotional core of what she wants
- "I don't think all the things I'm doing right now are the RIGHT things to get me where I want to go" — the prioritization crisis
- "I want to feel good about my ability to use AI, but if you keep moving the goalposts..." — feature fatigue
- "I don't feel completely comfortable if I finish an entirely task without fine-tuning something myself... it feels a little 'cheaty'" — the integrity hesitation
- "Every single follow-up prompt you provide, requires you having gone through the output very thoroughly" — on the cognitive cost of AI interaction

---

Kevin's Private Reactions
-------------------------

- **"She might be my ideal user!!!"** — triggered by May's meta-refinement practice (recording calls, asking Claude to learn from them)
- **"This is the thesis for AgenC that I had earlier!!!! Target the lack-of-time & stress that people are feeling!"** — triggered by May saying she hopes an agent can help her destress
- **"I think I can provide the FRAMEWORK for doing Inputs, Not Outputs"** — seeing May's desire for tactical behavior change as a product opportunity
- **"Maybe we can say 'this is the only thing you need'"** — response to May's feature fatigue, suggesting simplicity as positioning

---

Strategic Implications (Creative Director Analysis)
----------------------------------------------------

**May does not fit the current ICP** (technical professional, terminal-comfortable, writes code). She fits the earlier "tech-comfortable, non-terminal" target market hypothesis (the Dan/Khan persona cluster).

**What May validates:**
- The "Inputs Not Outputs" philosophy is universal — now confirmed across technical (Omar, Charles) AND non-technical (May) users
- The "stress reduction" thesis has a real user articulating the Before state clearly
- The "download and customize" model resonates — May's "April" concept is exactly what it would serve

**What May challenges:**
- The current ICP may be too narrow — May represents the broader market the product was originally designed for
- "Prioritization, not execution" is a different product category than AgenC's current delegation value prop
- Cowork's failure to land is a warning for AgenC's non-technical offering

**CD recommendation:** Document May as a "Phase 2 ICP validation" signal. Her profile and the "April" concept belong in the ICP document as evidence the broader non-technical market exists. Do not change the current beachhead ICP — broadening before the beachhead is established risks losing focus. Revisit when the technical beachhead is proven and the "download and customize" model is ready.

---

Cross-Conversation Patterns
----------------------------

Comparing with [Omar onboarding (2026-03-10)](./2026-03-10_omar-onboarding.md), [Charles (2026-03-12)](./2026-03-12_charles-agenc.md), and [Omar status (2026-03-18)](./2026-03-18_omar-status-interview.md):

### Pattern: "Inputs Not Outputs" is Universal (4/4 conversations)

- **May**: Independently built end-of-day meta-refinement loops with Claude
- **Omar onboarding**: Loved the prompt-engineer skill immediately
- **Charles**: Already practices iterative prompt refinement independently; was surprised it's not universal
- **Omar status**: 20-min prompt investment → 15-min autonomous high-quality work

**Signal:** This philosophy resonates with every user regardless of technical level. It should be front and center in all messaging. This is now the strongest validated signal across all user conversations.

### Pattern: Meta-Refinement is Independently Invented (3/4 conversations)

- **May**: End-of-day Claude sessions to output improved instructions
- **Charles**: Built a database-backed prompt factory with feedback loops
- **Omar status**: Built `/retrospective` and `/capture-session` skills

**Signal:** Sophisticated users independently build the self-refine loop. This validates it as a fundamental need, not an AgenC-specific feature. The product should make this effortless rather than requiring users to build it themselves.

### Pattern: The "Overwhelm" Before State (2/4 conversations)

- **May**: "I'm quite overwhelmed with all these new features... too fast!" and "I don't think all the things I'm doing right now are the RIGHT things"
- **Omar status**: Mission overload, losing context about what each mission is doing

**Signal:** Both technical and non-technical users feel overwhelmed — by different things (May by AI feature pace, Omar by mission proliferation), but the emotional state is the same. The narrative arc's "Before" state ("overwhelmed, stressed, doing everything yourself") is validated across user types.

### Pattern: Cowork / Existing Tools Feel Insufficient (3/4 conversations)

- **May**: Cowork is "an awkward in-between" — not agent enough, not simple enough
- **Charles**: Cursor works but he recognizes he's hitting a ceiling with context-switching across 6 windows
- **Omar onboarding**: Standard Claude Code lacked the orchestration and persistence he needed

**Signal:** There's a gap in the market between "vanilla AI chat" and "full agent orchestration." Users feel existing tools don't solve their real problems. The question is whether AgenC fills this gap or creates a new awkward in-between for non-technical users.

### Pattern: The "Working for Themselves" Profile Extends Beyond Engineers (2/4 conversations)

- **May**: Using Claude for personal development, building personal projects, managing her own priorities — even though she has a day job, her AI usage is deeply personal and self-directed
- **Omar status**: Building personal projects (school website, garden, games) independently

**Signal:** The ICP's "working for themselves" pattern isn't limited to solo engineers. May is employed but her most sophisticated AI usage is personal and self-directed. The "working for themselves" pattern may be about mindset (autonomy, self-optimization) rather than employment status.

### New Pattern: Prioritization > Execution

- **May**: "I need to keep surfacing things to ask, 'is this worth prioritizing right now?'" and wants "April" to help categorize and prioritize
- **Omar status**: Built a full GTD system with task categorization and weekly reviews
- **Khan** (from market research): "I want an active companion in my life that doesn't feel invasive"

**Signal:** Multiple users and prospects express a need for AI that helps them *decide what to do*, not just *do things*. This is adjacent to but distinct from AgenC's current execution-focused value prop. The Adjutant triage bead (agenc-181j.1) addresses this for the technical user; May validates that the need exists in the broader market too.
