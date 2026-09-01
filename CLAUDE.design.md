AgenC CLAUDE.md — Design Notes
==============================

This document records why `CLAUDE.md` at the repo root says what it says, so a future agent can verify, extend, or argue with a convention instead of inheriting it as an unexplained rule.

Where This Document Starts
--------------------------

It starts on 2026-09-01, with the "Command Output Carries No ANSI Escapes" section, and records only decisions made from that point forward.

The sections that predate it were **not** reconstructed. Their reasoning lives in the conversations, pull requests, and beads that produced them, and writing a plausible-sounding rationale here from the outside would be worse than leaving the gap: a reader trusts a design document precisely where they have no other source, so an invented "why" teaches the wrong generalisation exactly where it does the most damage.

If you genuinely know the reasoning behind an earlier section — you made the call, or you can source it from a bead, a PR, or a session transcript — add it, and say where it came from. If you are inferring, say that instead.

Provenance
----------

### Created

2026-09-01, during AgenC mission `2c644c10-f76c-441c-8e1f-092a90b0566b`, under bead `agenc-afp8`.

The document exists because of the gap that bead names. While shipping `agenc-hxr4` (PR [#37](https://github.com/mieubrisse/agenc/pull/37)), the ANSI convention needed a home in `CLAUDE.md`, and Kevin's configuration requires a sibling design document when editing a `CLAUDE.md` that lacks one. Bootstrapping one meant either reconstructing rationale for the whole existing file — inventing it, in practice — or leaving the convention undocumented. Kevin resolved it directly: start the document, record only what is actually known, and say on the page where it begins.

### Edits

- **2026-09-01** — Created, alongside the "Command Output Carries No ANSI Escapes" section. Mission `2c644c10-f76c-441c-8e1f-092a90b0566b`; beads `agenc-hxr4` (the change) and `agenc-afp8` (this document). Field evidence originates from mission `591187e7-dab6-4b2d-a063-ff12220d4c3a`.

- **2026-09-01** — Two rounds of clean-context review, in the same mission. The first found the `ForPicker` convention incomplete and a false coverage claim in this document; findings and response in the [PR #38 comment](https://github.com/mieubrisse/agenc/pull/38#issuecomment-5496178214), fixed in PR [#39](https://github.com/mieubrisse/agenc/pull/39). The second found the E2E checks vacuous under mutation and the palette banner untested; both fixed in the same PR, and the guardrail added there was verified by re-running the reviewer's own mutation. Remaining gaps tracked in `agenc-pman`.

  Worth stating plainly, since this document's contract is about what is known rather than what sounds right: **every one of those defects was found by review, not by the author, and the last of them was a false "complete set" enumeration in this very document — the exact failure mode it was being edited to warn about.** A convention that holds for most of its members reads as a guarantee and behaves as a trap, and the pull toward writing the tidy universal is strong enough that it caught the person writing the warning against it. Verify enumerations against the tree mechanically before asserting them here.

Design Decisions
----------------

### Command output carries no ANSI escapes (2026-09-01)

**The failure being prevented.** A parent mission armed three background watchers, each branching on `if [ "$ST" = "STOPPED" ]`. The branch never fired in any of them, because `agenc mission ls` returned the status wrapped in colour codes. Nothing errored. Two of the three watchers appeared to work, because their success path was driven by a GitHub API call and only their stall path depended on parsing — so the broken half stayed masked. A dead child mission and a healthy one produced identical output: silence. Recorded in bead `agenc-5jxx`.

**Removal, not conditional colour.** The conventional fix is to emit colour only when stdout is a terminal, and it was cheap here — `github.com/mattn/go-isatty` is already a dependency, already used for stdin checks across the CLI (`cmd/first_run.go`, `cmd/fzf_picker.go`, `cmd/cron_new.go`, and six others). Kevin considered that alternative and chose removal, on the grounds that the human-reader case for command output barely exists any more, so conditional-colour machinery would be complexity serving almost nobody. Recorded in bead `agenc-hxr4`.

**The palette is carved out, deliberately.** Mid-implementation Kevin narrowed the scope: it is `agenc <command>` output that is agent-consumed. He uses the tmux command palette heavily, and it is a genuinely interactive human surface, so it keeps its colour. This is why the inline escape at `cmd/tmux_palette.go:333` — the one `agenc-hxr4` flagged as the sequence a constants-only change would miss — is kept on purpose rather than overlooked.

**The line is drawn by destination, not by file.** The shared formatters (`formatRepoDisplay`, `displayGitRepo`, `colorizeStatus`) each served both a stdout table and an fzf picker, so a file-scoped rule would have been wrong in both directions at once: strip globally and the palette silently loses its colour, keep per-file and escapes stay in parsed output.

**Named variants, not a `colored bool` parameter.** A boolean parameter puts the decision at every call site and depends on each caller passing the right value — the same "expected not to" that produced the original bug. Two functions named for where their output goes (`formatRepoDisplay` vs. `formatRepoDisplayForPicker`) mean the plain form is what a caller reaches for by default, and colour has to be asked for explicitly. This mattered in practice: while auditing call sites during the change, one fzf picker (the repo multi-select in `cmd/repo_helpers.go`) was initially misfiled as stdout, and re-checking each site by reading where its output actually goes is what caught it.

**The `ForPicker` suffix was made exhaustive after review.** The convention shipped in PR #37 applied only to the two repo formatters. `colorizeStatus`, `attachedDot`, and `formatMissionCell` returned colour under unsuffixed names, which inverted the convention's own signal: under a rule where an unsuffixed name means plain, `attachedDot` read as safe and was not. A clean-context reviewer traced the concrete path — an agent adding an "attached" column to `agenc mission ls`, following the convention literally, finds the one unsuffixed function and reintroduces escapes into the exact command whose broken status parse started all of this. Nothing would have caught it: `attachedDot` had no unit test, and the E2E suite creates only headless missions, which are never attached, so the dot is the empty string throughout and the regression ships green. All three were renamed and `attachedDotForPicker` gained a test. The lesson worth keeping: a naming convention that holds for most of its members is worse than none, because it converts a name into a false assurance.

**Verification is byte-level.** A terminal draws escape sequences invisibly, which is exactly how the original bug hid, so "it looks clean" is not evidence. Go unit tests assert that the plain formatters carry no escape and that every colour-returning function still does; they run under `make check`, so the pre-commit hook catches a regression either way.

**A check that inspects an empty table is worse than a missing one.** The E2E section began as ten commands captured to a file and grepped for `0x1b`, which looked like coverage and was not. Review proved it: colour injected into `agenc mission peers` and `agenc repo ls` passed the entire suite green, because by the time the section runs the repo library has been emptied and no mission has a peer, so both commands print an empty-state string and the tables that would carry an escape never render. `mission peers` is the table whose own source comment says it exists to be parsed by agents.

The fix is structural rather than a longer list. Each check now passes a pattern that the captured output must match, so a command that renders nothing fails as vacuous instead of passing — and the section seeds a repo and a cron first, because otherwise those checks would trip their own guard. Commands whose rows cannot be populated in the test environment at all (`mission peers` needs a live Claude session, `mission search` needs indexed session content) are listed as explicit skips, so the gap is visible in the output rather than disguised as a pass. Remaining coverage gaps are tracked in `agenc-pman`.

The guard was worth having immediately: it failed the first `config repoConfig ls` pattern written for it, because that table lists configured repos rather than the repo library and the seeded repo never appeared. That is the same defect it exists to catch, caught within minutes of existing.
