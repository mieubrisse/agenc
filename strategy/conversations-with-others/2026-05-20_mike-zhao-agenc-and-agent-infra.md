Mike Zhao — AgenC Onboarding + Agent-Infra Investor Conversation
================================================================

- **Date:** 2026-05-20
- **Participants:** Kevin Today (AgenC founder); Mike Zhao (recently joined an early-stage seed-only VC firm as Principal; previously co-founder/engineering lead at a venture-backed startup for 8 years; building a side-project recruiting tool on Claude Code)
- **Duration:** ~1h17m
- **Grain recording:** private — recording id `fa9a8dc7-2c28-4fc8-8039-56c322f6fc10` (fetch via the Grain MCP)
- **Journal entry:** N/A (not separately captured)
- **Analysis mission:** `fbae7e47-7cf5-490c-abf6-047877da1d08` (run `agenc mission print fbae7e47-7cf5-490c-abf6-047877da1d08` for the full analytical context)

---

Source Material
---------------

Full Grain transcript (~70K chars) and AI-generated meeting notes. The conversation had three distinct phases:

1. **Personal/career catch-up (~15 min)** — Mike's transition out of his startup, joining venture as a Principal, his side-project recruiting tool, his read of multi-stage VC behavior.
2. **AgenC pitch + agent-infrastructure discussion (~25 min)** — Kevin's pitch of AgenC as a "work factory" / "CEO of clods" platform; Mike's investor-lens questions about which parts of the agent stack will accrue long-term value; Kevin's framing of S-work vs. unique work.
3. **Live AgenC onboarding (~30 min)** — Mike installed AgenC during the call, hit multiple friction points, eventually got to tab-switching working ("let's fucking go").

The conversation is unusual in this corpus because Mike is **simultaneously a candidate user AND a venture investor evaluating the agent-infra category**. His questions toggle between "would I use this" and "would I fund this." Both signal types are valuable; this log keeps them separate.

---

Summary
-------

Kevin pitched AgenC to Mike and onboarded him live. Mike — recently transitioned from operator to investor at a seed-only firm with an explicit agent-infrastructure thesis — pressure-tested the positioning with the question that should haunt every agent-infra founder: **"What's your point of view on the longevity of a lot of this infrastructure?"** His worked example was observability: a category he watched go from zero-to-low-single-digit-millions ARR in roughly half a year and then stall as models improved and OSS caught up. He framed memory-management as the next observability — a category that will get squeezed between improving foundation models and "good enough" custom builds.

Kevin's response — **S-work vs. unique work, with AgenC betting on the unique-work side via knowledge structuring** — landed enough that Mike asked to install. Mike's pushback on the positioning came in two phrases worth pinning: *"isn't this just memory management?"* and *"what makes you different from memory-management companies?"* Kevin answered with the Obsidian framing — AgenC doesn't replace your second brain, it teaches Claude how to think about your second brain — but the answer is hypothesis-grade and Mike did not test it further.

The onboarding then ran into the same wall every onboarding has run into: install-time friction. New friction surfaces appeared (token-line-break, color readability, the "name your config repo" prompt with no default) on top of recurring ones (PATH issues, tmux unfamiliarity). Mike got to the "let's fucking go" moment when tab-switching worked — but he also had to leave for family — and the session ended without him driving real work through AgenC.

The strategically heaviest moment was Mike's cold-start question: **"What's the right project to apply this to?"** Kevin's answer — *"literally just download the thing and start driving all your cloud usages through it"* — is the right philosophical answer (AgenC is a wrapper, not a replacement) but does not solve the cold-start problem for someone who doesn't already have a structured workflow to wrap.

---

Mike's Investor-Lens Pressure Tests
-----------------------------------

This is the most strategically valuable content in the conversation. Mike is now sitting on the buy-side of the agent-infra market and has explicit thesis-development questions. His critiques are not naive-user critiques; they are the critiques AgenC will hear from every other investor it talks to.

### 1. The observability cautionary tale (DIRECT QUOTE)

> Mike: "What's your point of view on the longevity of a lot of this infrastructure? […] Observability — one area where, when LLMs first came out, when agents first came out, you needed a ton of observability. […] As these underlying models get better, they essentially get priced out. There's also so much open source that is just as good, if not better. That ends up eating these observability companies' lunch. […] I invested in a company that was doing it — they went from zero to low-single-digit-millions ARR in roughly half a year. And then they were basically stuck there."

The implicit claim: **categories that solve "what's the model doing" lose value as models become legible.** Mike extends this to memory-management as the next likely victim.

### 2. Memory-management as a positioning trap

> Mike: "If now essentially like memory management, right? There are like a whole sort of startups entire thing — it's just like memory management. Like, I guess what makes what you're building unique compared to those other memory management companies?"

This is the single most important sentence Mike said. **If AgenC reads as a memory-management product to a sharp investor in the category, the differentiation is not yet sharp enough at the positioning surface.** This conversation does not validate that Kevin's "skills as codified expertise, not memory" framing in `strategy/creative-direction.md` lands — Mike heard the pitch and still mapped it to memory management.

### 3. Build-vs-buy as the real benchmark

> Mike: "I basically built my own observability version just through Claude Code that's custom to what I need. And it was like so easy to build. […] If I can do it in 10 minutes, why pay someone money to do it continuously?"

This is the **Claude-as-substitute** threat from the buyer side of a deeply technical user. Mike — a Principal at a VC, not an engineer-for-hire — built his own observability stack in Claude Code. The implication: **the AgenC ICP can in-house anything AgenC does that isn't load-bearing differentiation.** This is consistent with the `strategy/2026-03-31-product-roadmap.md` framing that the methodology is the moat, not the runtime — but it sharpens the urgency: if a user can vibe-code 80% of AgenC's session-management surface in an afternoon, then **only the parts that compound over time matter.**

### 4. Solopreneurship as the alternative path (mutual validation)

> Mike: "I feel like one of the advantages of being a solopreneur is that I didn't know that you don't need a venture outcome to create generational wealth. […] If you were doing venture, you're going to need like probably a 10x exit compared to what you need personally. And even if the labs do exactly what you're doing, you can always carve up a small niche of the market. And that small niche is probably worth double digit millions. […] Versus double digit millions as a market — it's not feasible if you're trying to do venture."

> Kevin: "Dude, like for me, my biggest hero right now is Peter Levels."

> Mike: "If I had any energy on you left for building, I would do the exact same thing as you."

This is **investor-validation of the solopreneur path for AgenC**. Mike — whose new job is finding venture outcomes — is explicitly saying he thinks solopreneur is the right shape for what Kevin is building. This is not a casual remark; he repeated it twice. It is also consistent with `2026-03-31-product-roadmap.md`'s monetization plan (charge for founder access, manual onboarding, no payment platform yet).

The implication for fundraising: **even Mike, who could in principle write Kevin a seed check, is signaling this isn't a venture-shaped business.** That is a clarifying datapoint, not a discouraging one. It rhymes with Khan's earlier "niche solution, not platform" guidance in `creative-direction.md`.

---

AgenC's Positioning Under Pressure: What Mike Heard
---------------------------------------------------

### What landed

- **The "work factory" framing.** Kevin opened with "imagine you're the CEO of a company of clods" and Mike immediately mapped it: *"got it, so it's like a multi-agent... agent harness, but multi."* The mental model is intuitive at this altitude.
- **The S-work / unique-work distinction.** Kevin's frame that "the labs will build the universally needed stuff; AgenC bets on the unique work only you can do" was the move that pivoted Mike from "isn't this getting commoditized" to "interesting, tell me more." This is the highest-leverage piece of language in the pitch and it should be elevated in `strategy/creative-direction.md` — currently the unique-work thesis is there but the S-work counterpart isn't named.
- **Provenance principle as a tangible compounding mechanism.** Kevin's "you must leave trails for yourself; future agents reading past sessions creates a compounding flywheel" was the moment Mike got most engaged. The breadcrumb idea is more legible than the abstract "skills compound" framing.
- **"It's a strict upgrade" framing as the onboarding promise.** Kevin's "literally just download and drive all your Cloud usages through it — it's a strict upgrade" got Mike to start installing in real time. This is the right one-liner for users who already use Claude Code. It will not work for users who don't.

### What did not land

- **Skills as the user-facing differentiator.** Kevin did not use the word "skills" once in the pitch portion. He led with the work-factory / TMUX-based session manager framing, not the codified-expertise framing. Mike accordingly heard a session-management/orchestration product, not a knowledge-compounding product, until the very end. This is a mis-load of the message Kevin presumably wants leading.
- **The "AgenC vs. memory-management" boundary.** As noted above — the Obsidian framing was offered but not tested. Treat the boundary as unproven.
- **The product name.** When Kevin said "Agency" / "AgenC" / "the platform / the cockpit", Mike's mapping was muddled. He asked clarifying questions about what the thing actually is throughout the pitch. This is consistent with `creative-direction.md`'s own flag that the name has not been validated.

---

Onboarding Friction Inventory
-----------------------------

In ~30 minutes of live install, Mike hit the following friction points. Listed in order of severity (highest first), each with the verbatim signal:

| # | Friction | Verbatim | Signal strength |
|---|---|---|---|
| 1 | **Config-repo prompt has no default** | Mike: *"this is a little confusing for me — I feel like… I don't, I don't know. It may just be back. I feel like I'm a little confused by like what repo named."* and *"you should just default it. And then if I want to change it, I can change it by myself later."* Kevin: *"that's good feedback."* | NEW — first time this surfaces in the corpus |
| 2 | **Cold-start: what to apply AgenC to** | Mike: *"what do you feel like is a good intro task? Because… my thing is like the cold start problem. It's like, I almost don't know what is the right project to apply this to. And oftentimes it's like, well, it's good for a new project. But I don't have an idea to test this on."* | NEW (first explicit articulation; implicit in past sessions) |
| 3 | **Token had a line break in it on copy** | Kevin: *"oh, line break — go ahead and copy that token again, looks like there might've been a line break in it."* Required manual file editing to fix. | NEW |
| 4 | **`brew install` of multiple things wasn't clear** | Mike: *"I need to do both of these? I think I only brew-installed one. Oh, okay. That's my fault."* | NEW (recurs with Omar 2026-03-10 in different form) |
| 5 | **PATH issue after install** | Mike: *"I might need to add it to my path. I always forget how to, how to do this."* | RECURRING (Omar 2026-03-10) |
| 6 | **Default tmux text color unreadable** | Mike: *"really, this blue thing is really hard to read. Oh, they can't even — do you see this?"* Kevin: *"oh yeah, we can change that."* | NEW |
| 7 | **Tmux unfamiliarity** | Mike has heard of tmux, has not used it. Kevin had to walk him through Ctrl-H/Ctrl-L for tab switching, copy mode, the status bar. | RECURRING (universal across all onboarding logs) |
| 8 | **Auto mode hotkey was not the one Kevin thought** | Kevin: *"I think it's control shift or something. I forget exactly what the hotkey is or shift tab maybe."* — Kevin himself did not remember the hotkey AgenC ships with. | NEW (founder-side, not user-side, but it's a signal that the default-hotkey story is not crisp) |

**Onboarding ended at the "let's fucking go" moment** when Ctrl-H/Ctrl-L tab switching finally worked — but Mike had to leave to take care of his daughter before driving any real work through AgenC. So the onboarding got to *infrastructure-works*, not to *first-value-delivered*. This is the same shape as Omar (2026-03-10), Pedro, and Yannik onboardings.

---

Mike's Side-Project ("Ream") as a Diagnostic Mirror
----------------------------------------------------

Mike built a recruiting product on Claude Code as a side project — automated SEO-driven blog generation, cost calculators, GitHub-sourced candidate ranking, AI-generated outreach emails. He has acquired paying customers essentially passively. He has eval suites running, a custom observability stack he built himself in Claude Code, and a deliberate "no support / hands-off" stance.

The diagnostic insight is **what Mike's stack is missing that AgenC could be**:

- He runs schedules in two places (one hosted scheduler + a separate VM cron job) because there is no unified scheduler for "Claude Code work that should run on a cadence."
- He built his own observability stack because off-the-shelf wasn't custom enough to be worth paying for.
- He has no concept of skills, missions, or session-as-a-first-class-thing — everything is ad-hoc.
- He has no provenance trail across sessions.

**He is the ICP** — a technically capable solopreneur running Claude Code on a daily basis to drive a one-person business, who values automation, who has eval discipline, who would pay for the right tool. He hit the install wall in the same way every other ICP user has. **This is the most direct ICP datapoint in the corpus to date** because it's not a market-research conversation — it's a real user with a real Claude-Code-driven workflow already running.

---

Cross-Conversation Patterns
---------------------------

Comparing with [Raj (2026-02-13)](./2026-02-13_raj.md), [May (2026-03-06)](./2026-03-06_may-user-interview.md), [Omar onboarding (2026-03-10)](./2026-03-10_omar-onboarding.md), [Charles (2026-03-12)](./2026-03-12_charles-agenc.md), [Omar status (2026-03-18)](./2026-03-18_omar-status-interview.md), and [Yannik (2026-05-12)](./2026-05-12_yannik-amp-comparison.md):

### 1. Onboarding is a graveyard. This is the fifth consecutive conversation where install-time friction was the gating event.

Omar, Pedro, Yannik, Charles, and now Mike — every direct AgenC onboarding has consumed most of the available conversation time on getting the infrastructure to work, with little or no time left to drive actual work. **This is no longer an Omar-specific bug or a Charles-specific cognitive-saturation issue — it is the load-bearing failure mode of AgenC adoption.** `agenc-1p7z` (Design progressive/layered onboarding flow) is the existing bead for this; it should be elevated to P0 and this conversation added as the fifth signal.

### 2. tmux remains universal friction across all ICP segments.

Every onboarding log in the corpus flags tmux as the steepest learning curve. Mike (a strong engineer running Claude Code daily) had not used tmux. The `strategy/icp.md` profile already names this: *"Not familiar with tmux — this is a new tool for them, not a comfortable home."* This conversation adds another datapoint to a hypothesis that no longer needs more datapoints.

### 3. The "cold start" problem is a more specific version of Charles's "cognitive saturation."

Charles asked Kevin to slow down twice. Mike asked "what's the right project to apply this to?" The shared root cause: **AgenC has no opinionated first-mission.** The user has to invent the first task themselves, which collides with the unfamiliarity of the system. A starter mission ("here's a small but valuable workflow you can have AgenC run today") would address both Charles's and Mike's friction.

### 4. The Yannik-style billing-model framing is also Mike's framing.

Yannik (2026-05-12) said the #1 value of AgenC vs Amp is "use my Claude Code sub." Mike independently asked: *"does it go through the API or does it go through Claude Code?"* The Claude Code subscription compatibility is structurally load-bearing to the ICP. Currently this is implicit in the product; should be explicit in positioning.

### 5. Memory-management framing is a new positioning challenge.

Not seen in any prior conversation. Worth tracking whether other investor-adjacent conversations also map AgenC onto memory-management.

---

Action Items
------------

Promised in-conversation by Kevin:

- **Intro Mike to Gianni Mishra** (Kevin's friend in the agent-infra space — based in London). Note: Mike attended a Kurtosis dinner where Gianni was at the other end of the table; Mike did not get a chance to talk to him then. Kevin: *"I'll put together a list of people who I think [you should talk to]"*, *"I'll try to give you some intros to folks. You met a lot of them at the Kurtosis dinner, thankfully."*
- **Intro Mike to the founder of tabtabtab.ai** (Kevin's friend working on an agent IDE with cloud-offload). Kevin said he would.
- **Curate a longer list of agent-infrastructure folks for Mike** — Kevin: *"let me ask my cloud afterwards to try and come through and see my friends from \[Kurtosis\] in particular, because those are the guys who are in like the coast."*
- **Follow up with Mike on AgenC usage** — implied but not committed verbally. He installed; the install was incomplete (auto-mode-default change was still in-flight when call ended); he is the highest-signal new user the corpus has had.

Mike-side commitments:
- **Mike will try driving Claude work through AgenC.** Verbatim: *"I do want to start using this. I'm like, I would have just downloaded just now."*

---

Strategic Implications (Recommendations for `strategy/creative-direction.md`)
-----------------------------------------------------------------------------

These are flagged for the founder's review; this log does not edit the strategy docs.

1. **Elevate the S-work / unique-work language to a top-level positioning frame.** It is the move that flipped Mike from skeptical-investor mode to engaged-listener mode. The current creative-direction names "unique work" but does not pair it with "S-work that the labs will build." The pairing is the load-bearing distinction.

2. **Treat "AgenC vs. memory-management" as an open positioning question.** The Obsidian-framing answer is hypothesis-grade. If a second investor-adjacent conversation also maps AgenC onto memory-management, that hypothesis should be elevated to a strategic priority.

3. **The Claude-Code-subscription-compatibility should be explicit, not implicit.** Mike and Yannik independently surfaced this as load-bearing. Currently it appears in `icp.md` only as background; it deserves a dedicated framing as a billing-model differentiator.

4. **The solopreneur-not-venture path now has a venture-investor endorsement.** Mike's "I would do exactly what you're doing" remark is third-party validation of the `2026-03-31-product-roadmap.md` direction. Worth referencing the next time the question "should I raise" surfaces.

5. **The cold-start problem should be elevated to a first-class onboarding requirement.** A starter mission — concrete, valuable, demoable in <10 min, requiring no user-side workflow-already-exists — is now a recurring gap across five onboardings. This is bigger than progressive disclosure; it's about having an opinionated first-use case.

---

Selected Direct Quotes
----------------------

On observability as a cautionary tale:
> Mike: "Observability companies — I don't think the value accrues to them long-term. It accrues to them in the beginning when things are barely working, but as the underlying models, tool use, agents whatever, they get better and better, you just don't have a need for it."

On the build-vs-buy threat:
> Mike: "I basically built my own observability version just through Claude Code that's custom to what I need. […] If I can do it in 10 minutes, why pay someone money to do it continuously?"

On AgenC's positioning lane:
> Kevin: "There is work that is unique work that only you can do. […] The ability to encode the contents of your mind inside of Claude in a structured and effective way. And at least I haven't seen the labs doing a good job of this yet."

On the memory-management collision:
> Mike: "What makes what you're building unique compared to those memory-management companies?"

On the cold-start problem:
> Mike: "My thing is like the cold-start problem. […] I almost don't know what is the right project to apply this to."

On the strict-upgrade promise:
> Kevin: "Whatever you want to do, just do it through AgenC and it will be a strict upgrade."

On the solopreneur validation:
> Mike: "If I had any energy left for building, I would do the exact same thing as you."

On AgenC working:
> Mike: "Let's fucking go." [after Ctrl-H/Ctrl-L tab switching worked]
