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

Design Decisions
----------------

### Command output carries no ANSI escapes (2026-09-01)

**The failure being prevented.** A parent mission armed three background watchers, each branching on `if [ "$ST" = "STOPPED" ]`. The branch never fired in any of them, because `agenc mission ls` returned the status wrapped in colour codes. Nothing errored. Two of the three watchers appeared to work, because their success path was driven by a GitHub API call and only their stall path depended on parsing — so the broken half stayed masked. A dead child mission and a healthy one produced identical output: silence. Recorded in bead `agenc-5jxx`.

**Removal, not conditional colour.** The conventional fix is to emit colour only when stdout is a terminal, and it was cheap here — `github.com/mattn/go-isatty` is already a dependency, already used for stdin checks across the CLI (`cmd/first_run.go`, `cmd/fzf_picker.go`, `cmd/cron_new.go`, and six others). Kevin considered that alternative and chose removal, on the grounds that the human-reader case for command output barely exists any more, so conditional-colour machinery would be complexity serving almost nobody. Recorded in bead `agenc-hxr4`.

**The palette is carved out, deliberately.** Mid-implementation Kevin narrowed the scope: it is `agenc <command>` output that is agent-consumed. He uses the tmux command palette heavily, and it is a genuinely interactive human surface, so it keeps its colour. This is why the inline escape at `cmd/tmux_palette.go:333` — the one `agenc-hxr4` flagged as the sequence a constants-only change would miss — is kept on purpose rather than overlooked.

**The line is drawn by destination, not by file.** The shared formatters (`formatRepoDisplay`, `displayGitRepo`, `colorizeStatus`) each served both a stdout table and an fzf picker, so a file-scoped rule would have been wrong in both directions at once: strip globally and the palette silently loses its colour, keep per-file and escapes stay in parsed output.

**Named variants, not a `colored bool` parameter.** A boolean parameter puts the decision at every call site and depends on each caller passing the right value — the same "expected not to" that produced the original bug. Two functions named for where their output goes (`formatRepoDisplay` vs. `formatRepoDisplayForPicker`) mean the plain form is what a caller reaches for by default, and colour has to be asked for explicitly. This mattered in practice: while auditing call sites during the change, one fzf picker (the repo multi-select in `cmd/repo_helpers.go`) was initially misfiled as stdout, and re-checking each site by reading where its output actually goes is what caught it.

**Verification is byte-level in both directions.** A terminal draws escape sequences invisibly, which is exactly how the original bug hid, so "it looks clean" is not evidence. Go unit tests assert that the plain formatters carry no escape and that the picker and palette formatters still do; they run under `make check`, so the pre-commit hook catches a regression either way. An E2E section captures each agent-facing command's output to a file and fails on any `0x1b` byte.
