---
paths:
  - "internal/claudeconfig/prime_preamble.md"
  - "internal/claudeconfig/prime_postamble.md"
  - "internal/claudeconfig/prime_content.md"
  - "internal/claudeconfig/adjutant_claude.md"
---

Prompt-Editing Discipline
=========================

The files this rule scopes to are **prompts**, not documentation. They are injected verbatim into every AgenC agent's context — either via a SessionStart hook (`agenc prime` reads `prime_content.md`, which is composed from `prime_preamble.md` + the Cobra-generated command tree + `prime_postamble.md`) or via the CLAUDE.md merge in Adjutant missions (`adjutant_claude.md`).

A careless wording change here ships to every mission. Apply prompt-engineering discipline before editing:

- **Before modifying any of these files, invoke `/prompt-writing`.** That skill is the canonical doctrine for evaluating prompts to Claude.
- **Never edit `prime_content.md` directly.** It is regenerated from `prime_preamble.md` + Cobra command tree + `prime_postamble.md` by `cmd/genprime`. Edit the source files and re-run `make build` (or `go run ./cmd/genprime`).
- **Treat every line as load-bearing.** Words drift slowly and compound across thousands of mission turns.
