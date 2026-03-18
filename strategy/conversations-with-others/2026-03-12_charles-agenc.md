Charles — AgenC Market Research Conversation
=============================================

- **Date:** 2026-03-12
- **Participants:** Kevin (AgenC creator), Charles (engineer, AI-assisted development practitioner, Cursor power user)
- **Grain recording ID:** `5b5b8ddb-92f6-4f28-8da0-cfd24f178696`
- **Journal entry:** `charles-agenc-market-research~2026-03-12_15-40-11~.md`
- **Analysis mission:** `3142b0af-6f40-421a-a71e-834614f5b71d` (initial), `d64edbe6-63f4-476f-9caf-a6effed7793a` (strategic contextualization + beads)

---

Summary
-------

- Charles is a sophisticated AI-assisted developer who has independently built many of the same concepts Kevin has (skill composition, prompt refinement loops, meta-prompts for self-improvement) — but inside Cursor's ecosystem with a custom database-backed prompt management tool
- Kevin demoed AgenC's full stack: tmux-based mission management, the command palette, multi-agent orchestration, repos-as-agents, MCP server integrations (Todoist, Google Calendar), and the permissions/hooks system
- Charles was genuinely engaged but hit cognitive saturation twice during the demo — explicitly asking Kevin to slow down. The volume of new concepts (tmux, missions, MCP, settings.json, hooks) was too much for a single session
- The "not looking at code" paradigm was the biggest philosophical tension — Charles's entire workflow centers on verifying code correctness, while Kevin advocates treating agents like employees and checking outcomes instead of implementation
- Charles committed to trying Claude Code and learning tmux as prerequisites to evaluating AgenC, but expressed the classic "I don't want to learn something new because what I have works" resistance before recognizing the pattern from his own past tool transitions
- Both strongly aligned on the "inputs not outputs" philosophy — fix the prompt, not the AI's output. Charles was surprised this isn't obvious to everyone
- Kevin offered to onboard Charles once he has Claude Code + tmux basics

---

Action Items
------------

### Kevin

- [ ] Share all prompts/skills with Charles (Kevin offered broader than just prompt-engineer: "I'm happy to give you all my prompts if you want")
- [ ] Share the Superpowers repo reference specifically
- [ ] Onboard Charles to AgenC once he has Claude Code + tmux basics
- [ ] Consider a structured demo sequence that introduces concepts incrementally (tmux first, then missions, then MCP — not all at once)
- [ ] Facilitate three-way skill cross-pollination between Kevin, Charles, and Yannick

### Charles

- [ ] Try Claude Code on a new project
- [ ] Learn tmux fundamentals
- [ ] Share access to his prompt factory web app with Kevin
- [ ] Look into MCP servers — specifically Gmail MCP and iMessage MCP for his assistant project (instead of building from scratch)
- [ ] Explore Claude Code (currently using OpenAI API for his assistant POC — no model lock-in)

### Unassigned

- [ ] Follow-up conversation after Charles has used Claude Code ("Let's talk again once I get into Claude")

---

Themes
------

**1. Convergent evolution of AI development practices.** Charles and Kevin independently arrived at nearly identical abstractions — skill composition, prompt refinement loops, meta-prompts for self-improvement, and the "inputs not outputs" philosophy. This is strong signal that these patterns are fundamental to effective AI-assisted development, not just personal preference.

**2. The IDE comfort zone is the adoption barrier.** Charles is deeply embedded in Cursor's visual workflow (GitHub Desktop for diffs, Cursor for editing, multiple windows for context-switching). The terminal-only paradigm is a psychological barrier even for a sophisticated user. His quote: "This feels like one of those moments that like, I don't want to learn something new because I have what I have like works well in my brain."

**3. Trust and control vs. autonomy.** Charles explicitly prefers defined, constrained routes for what AI can do — encoding all allowed actions as explicit routes. Kevin's approach is the opposite: broad permissions with deny lists for dangerous operations. Charles runs Cursor in "YOLO mode" but paradoxically wants more control when building his own AI assistant. This reveals that trust is context-dependent.

**4. Code review as identity vs. code review as overhead.** Charles's instinct to review every line is deeply ingrained and tied to professional identity ("getting the code right so I can scan it"). Kevin frames code review as overhead that agents should handle. The legitimate counterpoint Charles raised — bad habits propagating across the codebase — remains unresolved.

**5. Information overload during demos.** Kevin's demo covered too many layers at once. Charles hit cognitive saturation at two distinct points (~1:04 and ~1:19) and explicitly flagged it. This mirrors Omar's experience and suggests the demo/onboarding flow needs to be restructured into progressive layers.

**6. Prompt management: database vs. Git.** Charles stores prompts in a database with a custom web UI and sync script. Kevin uses Git repos. Both approaches have merit, but the transition between them is a friction point for adoption. Git-based distribution is a potential unifier.

---

Agreement and Disagreement
--------------------------

### Aligned On

- The "inputs not outputs" philosophy is fundamental — fix prompts, not AI output
- Skill composition (skills calling sub-skills) is the right abstraction for complex workflows
- Iterative prompt refinement with feedback loops is essential
- The new engineer skillset is product management and architecture, not just coding
- Git is the right distribution mechanism for skills/prompts
- The cost of LLM usage is negligible compared to productivity gains
- AI-assisted development is evolving rapidly (IDE -> co-gen -> YOLO -> agent orchestration)

### Diverged On

- **Code review**: Charles reviews every diff; Kevin doesn't look at code at all. Charles's counterpoint about bad habits propagating is legitimate.
- **IDE vs. terminal**: Charles needs visual tools (GitHub Desktop, Cursor); Kevin is fully terminal-native. This is the primary adoption barrier.
- **Trust model**: Charles wants explicit, enumerated permissions for AI actions; Kevin uses broad permissions with deny lists. Different risk tolerances.
- **Test coverage**: Charles relies on structural correctness enforced by rules/skills instead of tests; Kevin emphasizes test-driven development as a guardrail for agent-written code.
- **Prompt storage**: Charles uses a database with a custom web UI; Kevin uses Git repos. Different infrastructure philosophies.

---

Insights
--------

1. **Charles is the ideal early adopter profile — and he's resistant.** He's already doing sophisticated AI-assisted development, understands the concepts, and independently built parallel tools. If *he* finds the transition daunting, less sophisticated users will find it impossible. His resistance is diagnostic, not dismissive.

2. **The demo-to-adoption pipeline is broken.** Kevin showed everything AgenC can do in one session, which is impressive but overwhelming. Charles needed the concepts introduced in layers: first Claude Code alone, then tmux, then missions, then MCP. The "wow factor" approach risks producing admiration without adoption.

3. **Charles's prompt factory validates a missing AgenC feature.** His web-based prompt management tool with diff visualization, feedback loops, and database-backed versioning is something AgenC doesn't have. The "refine prompt" command is a CLI equivalent, but the visual diff and reasoning display that Charles built is more accessible. This could inform AgenC's prompt management UX.

4. **The "repos as agents" metaphor didn't land.** Kevin introduced this framing, but Charles didn't engage with it. It may be too abstract without hands-on experience. The concrete demo of missions and MCP servers was more compelling.

5. **Charles's six-project context-switching is exactly the problem AgenC solves.** He's manually command-tabbing between six Cursor windows. AgenC's mission model with backgrounding and the command palette directly addresses this — but he can't see it yet because tmux is unfamiliar.

6. **The "not looking at code" paradigm needs a bridge.** Going from "I review every line" to "I never look at code" is too large a leap. A middle ground — agent-written code with automated review (linting, tests, another agent reviewing) — would be more palatable for users like Charles.

7. **MCP servers are the killer feature Charles doesn't know about yet.** His planned assistant project (analyzing conversations, suggesting calendar events, creating to-dos) is exactly what Kevin already built with MCP integrations. Once Charles discovers MCP, it could be the hook that pulls him into the ecosystem.

8. **Charles's "bottom-up" building philosophy conflicts with AgenC's "trust the agent" approach.** Charles ensures every building block is correct before composing. Kevin trusts agents to handle building blocks and checks outcomes. These are fundamentally different engineering philosophies, and AgenC's onboarding needs to acknowledge and bridge this gap.

9. **Kevin's "Inputs Not Outputs" blog post directly catalyzed Charles's prompt factory.** Charles said: "That might have been the thing that triggered me to be like, okay, I got to build some way to manage my prompts." This is direct evidence that Kevin's thought leadership content drives adoption behaviors — content is not just marketing, it's a conversion mechanism.

10. **Charles built a human-in-the-loop approval queue for AI actions.** His POC has a front-end where AI-suggested actions queue for human approval/rejection, and rejections feed back into prompt improvement. This is a concrete UX pattern worth studying — it solves the trust problem through explicit consent rather than broad permissions.

11. **The autonomy gap is quantifiable.** Charles's agent runs last "minutes" (max). Kevin's longest autonomous run was 7 hours, with a standard of ~10 minutes. This quantifies the trust/experience gap between the two approaches and suggests a progression curve for onboarding.

12. **Charles's bottom-up preference is experiential, not just philosophical.** He tried top-down with his prompt factory and it "became unmanageable — I couldn't make any updates to it." This is a data point that the "just trust the agent" approach has real failure modes that need to be addressed, not dismissed.

13. **60% of engineers Kevin talks to aren't even using skills.** This is a critical market signal: the target audience mostly hasn't adopted the prerequisite concepts that Charles and Kevin take for granted. AgenC is selling to a market that doesn't yet have the mental model for what AgenC does.

14. **Kevin explicitly self-segments AgenC as stages 6-8 of agentic evolution** ("Agency is meant to help with this stuff six to eight... you don't need agency for just stage five"). This has positioning implications — AgenC should not market to beginners.

---

Notable Quotes
---------------

> "For all these AI tools, it's very black-boxed. And I want to know, like if I don't like what it outputted, how is it going to fix it? And what's the diff in its logic or instructions that would make it better the next time." — Charles (29:53), articulating the core transparency need driving his prompt factory

> "This feels like one of those moments that like, I don't want to learn something new because I have what I have like works well in my brain. But it seems like this isn't like one of those things that I should invest some time into." — Charles (1:00:25), the exact conversion-moment psychology

> "I'm kind of approaching my limit on the processing of information that you're giving me." — Charles (1:19:36), flagging cognitive overload during the demo

> "That is cool. And I want that. And the problem that kind of arises from that is like, how do you transfer skills to other people so that it works for one person to another?" — Charles (1:36:13), reacting to skill composition and immediately asking about distribution

> "That might have been the thing that triggered me to be like, okay, I got to build some way to manage my prompts or I'm just going to make it the same as the norm." — Charles (1:38:49), on Kevin's "Inputs Not Outputs" blog post catalyzing his prompt factory work

> "They're just correcting the output every single time and then they're like, ah AI isn't that good. And I'm like, dude, fix your prompts. It's not the AI is trash. Your prompts are trash." — Kevin (1:38:16), on 60% of engineers not using skills

> "You should be treating your work as the CEO of an organization of robots, a company of robots." — Kevin (1:18:01)

> "One counterpoint: if there's bad habits that it copies, it'll tend to [replicate] those all across the code base." — Charles (54:36), the legitimate code review concern

---

Followups
---------

- **Follow up with Charles in 1-2 weeks** to see if he's tried Claude Code and tmux. Don't push AgenC until he has those basics. The adoption path is: Claude Code -> tmux familiarity -> AgenC demo of just missions -> MCP servers.
- **Share the prompt-engineer skill immediately** — this is the most transferable piece and gives Charles immediate value without requiring tool migration.
- **Ask Charles for access to his prompt factory** — his diff visualization and feedback loop UX could inform AgenC's prompt refinement workflow.
- **Explore MCP as the onboarding hook for Charles** — his assistant project (message analysis, calendar, to-dos) maps directly to existing MCP integrations. Showing him this specific use case would be more compelling than a general AgenC demo.
- **Consider a "progressive onboarding" design** — based on both Charles and Omar's experiences, the onboarding should be layered: (1) Claude Code basics, (2) tmux orientation, (3) first mission, (4) config refinement loop, (5) MCP and multi-agent. Never show everything at once.
- **Address the code review concern directly** — Charles raised a real concern about bad habits propagating. Prepare a concrete answer: TDD, linting, agent-reviewed PRs, architectural guardrails. This will come up with every experienced engineer.

---

Strategic Contextualization (Creative Director)
------------------------------------------------

*Added by mission `d64edbe6-63f4-476f-9caf-a6effed7793a` on 2026-03-18.*

### ICP Validation

Charles is a near-perfect match for the current ICP *philosophically* — he thinks in systems, practices "inputs not outputs" independently, and builds compounding tools. But he's a mismatch on the *interface dimension*: he's deeply visual (GitHub Desktop for diffs, Cursor's GUI, multiple windows) and explicitly flagged terminal-unfamiliarity as a barrier.

**Implication (high confidence):** The ICP's "comfortable writing code and working in a terminal" clause may be too narrow. Charles demonstrates that the *philosophy* is the qualifying trait, not the terminal comfort. Users who think in systems but prefer visual interfaces are being excluded by the tmux dependency. This reinforces the GUI investment case (bead `agenc-c805`).

### Positioning Signal

Charles's convergent evolution — independently building skill composition, prompt refinement loops, meta-prompts — is the strongest validation yet that AgenC's core abstractions are correct. Two independent practitioners arriving at the same patterns is strong signal that these are *fundamental* rather than idiosyncratic.

**Implication (high confidence):** The positioning should lean into "we codified the patterns that expert AI practitioners have already discovered independently." This frames AgenC as the formalization of proven practices, not an opinionated experiment. Charles's quote — "this feels like one of those moments that like, I don't want to learn something new because I have what I have works well in my brain... but it seems like this isn't like one of those things that I should invest some time into" — captures the exact conversion moment the product needs to manufacture at scale.

### Competitive Positioning: Cursor Ecosystem Stickiness

Charles's setup is deeply Cursor-native: custom rules, project-specific skills, a database-backed prompt management tool, GitHub Desktop integration. The migration cost from Cursor to AgenC is substantial even for a philosophically-aligned user.

**Implication (medium confidence):** AgenC should not position as a Cursor replacement. The winning play is to position as the *orchestration layer above* the IDE — you keep your Cursor/VS Code setup for individual coding sessions, but AgenC handles multi-project coordination, MCP integrations, session lifecycle, and skill distribution. Charles's pain point of command-tabbing between six Cursor windows is the entry wedge.

### MCP as the Adoption Hook

Charles is building a personal assistant that analyzes conversations, suggests calendar events, and creates to-dos — using a custom-built local store because he doesn't know about MCP. When Kevin mentioned MCP servers, Charles's reaction was immediate: "That is cool. And I want that."

**Implication (high confidence):** For sophisticated users who already have agent workflows, MCP is the *differentiation hook*. They've solved the coding-agent problem in their own way; what they haven't solved is the *integration problem* — connecting agents to external services. AgenC's MCP ecosystem is the thing Cursor cannot offer. This should be the lead in demos for users at Charles's level.

### The "Not Looking at Code" Bridge

Charles's code review instinct is identity-level: "getting the code right so I can scan it." Kevin's "I don't look at code" paradigm is a hard sell. Charles raised a real concern: "If there's bad habits that it copies, it'll tend to [replicate] those all across the code base."

**Implication (high confidence):** The messaging around code review needs a middle path. The current framing ("you're the CEO, don't review code") alienates experienced engineers. A better frame: "You verify *outcomes and standards*, not *implementation*. TDD, linting, agent code review, and architectural guardrails replace manual diff review." This preserves the efficiency gain without asking engineers to abandon professional identity.

### Skill Portability as Network Effect

Charles asked: "How do you transfer skills to other people so that it works for one person to another?" This is the exact question that, if AgenC solves well, creates a moat. A skill marketplace or Git-based distribution network creates network effects that no competitor currently has.

**Implication (medium confidence):** Skill sharing/distribution (bead `agenc-yu0u`) should be elevated in priority. Charles's question reveals demand for this from the *producer* side — he wants to share what he's built. This is the supply-side validation the feature needs. The consumption side ("download experts and customize" from Dan and Khan) is already validated.

---

Cross-Conversation Patterns
----------------------------

Comparing with [Omar's onboarding session (2026-03-10)](./2026-03-10_omar-onboarding.md):

### Pattern 1: Information Overload During First Exposure

Both Charles and Omar hit cognitive saturation during their first exposure to AgenC.

- **Omar**: Got lost in the technical friction of setup (Xcode, PATH, tmux) and needed hand-holding through each step
- **Charles**: Explicitly asked Kevin to slow down twice (~1:04 and ~1:19) and said "I'm kind of approaching my limit on the processing of information"

**Signal:** AgenC's concept surface area is too large for a single session. Both a hands-on onboarding (Omar) and a demo-style walkthrough (Charles) overwhelmed users at roughly the same point. The onboarding needs progressive disclosure — introduce one layer at a time.

### Pattern 2: tmux as the Primary UX Barrier

- **Omar**: tmux was "the single biggest UX hurdle" — unfamiliar hotkeys, confusing server/session model, environment inheritance issues
- **Charles**: "I need to look at TMUX. And then I think what you're talking about will make more sense to me." He flagged it as a prerequisite he doesn't yet have.

**Signal:** tmux is a hard prerequisite that blocks comprehension of everything built on top of it. Both users — one doing a live install, one watching a demo — identified it as the barrier. This reinforces the case for either (a) a comprehensive tmux onboarding layer, or (b) the GUI investment to eliminate tmux as a dependency.

### Pattern 3: The Config Refinement Loop is Not Self-Evident

- **Omar**: "I am unclear on the workflow as a whole... things like when to be writing skills, or just a Claude md. How to bake lessons back into its own overall or skill behaviours"
- **Charles**: Built his own parallel system (prompt factory with database, feedback loops, meta-prompts) because the concept of iterative prompt refinement is core to his workflow — but he didn't connect Kevin's "refine prompt" command to his own practice until late in the conversation

**Signal:** The config refinement loop is AgenC's core value proposition, but it requires explicit teaching. Omar didn't understand it after a full onboarding. Charles understood the concept (he built his own version) but didn't immediately see it in AgenC's implementation. The refinement workflow needs to be the *first* thing demonstrated, not buried after infrastructure concepts.

### Pattern 4: Immediate Value Items vs. Infrastructure

- **Omar**: The prompt-engineer skill was the thing that stuck ("using prompt eng to generate the Claude so far has been dope"). He did *not* mention tmux, command palette, or Adjutant afterward.
- **Charles**: Got most excited about MCP server integrations ("That is cool. And I want that.") and the prompt refinement command — concrete capabilities, not orchestration infrastructure.

**Signal:** Users remember and value concrete capabilities that produce immediate results. Infrastructure (tmux, missions, sandboxing) is invisible plumbing that should be. The onboarding should lead with a "wow moment" capability (skill that does something impressive, MCP integration that solves a real problem) and backfill the infrastructure explanation later.

### Pattern 5: "Not Looking at Code" is a Hard Sell

- **Omar**: Not directly discussed, but Omar's background as an engineer suggests he would also resist
- **Charles**: Explicitly pushed back: "Man, it's going to be like a change for me like of reviewing code." Raised the legitimate concern about bad habits propagating. His previous bad experience with vibe coding (9 months ago) is still in his memory.

**Signal:** The "CEO of an agent org" framing requires a paradigm shift that experienced engineers resist. The alternative framing — "you review outcomes and tests, not implementation" — combined with concrete guardrails (TDD, linting, agent code review) may be more palatable. Need a bridge narrative, not a cliff.

### Pattern 6: Both Users Are Excited Despite Friction

- **Omar**: "It really is great to use" (post-session WhatsApp)
- **Charles**: "This is super fun to nerd out with you" and committed to trying Claude Code + tmux

**Signal:** The core value proposition is compelling even through significant friction. Both users left wanting more, not less. The product doesn't have a value problem — it has an accessibility problem. Reducing friction would convert interest into adoption.

### Synthesis: The Onboarding Must Be Restructured

Both conversations point to the same conclusion: AgenC's onboarding needs progressive layers, not a firehose.

**Proposed sequence based on both conversations:**
1. **Hook** — Show one impressive capability (prompt-engineer skill, MCP integration) that produces immediate value
2. **Foundation** — Claude Code basics (for users who don't have it yet)
3. **tmux orientation** — Dedicated, focused introduction to just the tmux concepts AgenC needs
4. **First mission** — A guided, single-purpose mission that works end-to-end
5. **Config refinement loop** — Explicitly teach the feedback loop with a concrete example
6. **Advanced features** — Multi-agent, MCP servers, repos-as-agents, permissions

This sequence is informed by where both Omar and Charles got stuck, what excited them, and what they remembered afterward.
