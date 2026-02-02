mission ls table alignment
==========================

Problem
-------

The `mission ls` table output has column misalignment. Columns don't line up
cleanly across rows.

Current implementation
----------------------

The table is rendered with Go's `text/tabwriter` in `cmd/mission_ls.go`:

```go
w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.StripEscape)
fmt.Fprintln(w, "ID\tSTATUS\tAGENT\tREPO\tCREATED")
```

Rows are written with tab-separated `fmt.Fprintf` calls, then `w.Flush()`.

The STATUS column uses ANSI color codes: green for RUNNING, yellow for ARCHIVED,
plain text for STOPPED.

What's been tried
-----------------

### Attempt 1: raw ANSI codes

```go
return "\033[32m" + status + "\033[0m"
```

Broke alignment because `tabwriter` counts the invisible ANSI escape bytes as
visible character width, making colored cells appear wider than they are. Rows
with different status values (colored vs plain STOPPED) get different effective
widths for the STATUS column, causing downstream columns to shift.

### Attempt 2: tabwriter StripEscape with \xff markers

Enabled `tabwriter.StripEscape` and wrapped the ANSI sequences in `\xff` escape
markers:

```go
tabEscape("\033[32m") + status + tabEscape("\033[0m")
// where tabEscape(s) = "\xff" + s + "\xff"
```

The intent was for `tabwriter` to pass the ANSI codes through to the terminal
but exclude them from column width calculation. This still does not render
correctly.

Where the problem likely lies
-----------------------------

Possible issues with the current `\xff` approach:

1. **`\xff` may conflict with ANSI escape bytes.** The tabwriter StripEscape
   docs say content between `\xff` markers is passed through unchanged and not
   counted for width. But the ANSI sequences contain bytes like `\033` and `[`
   that tabwriter may still interpret in unexpected ways. The `\xff` escape
   mechanism was designed for HTML filtering, not ANSI terminals.

2. **Tab characters inside escaped segments.** If any tab or newline appears
   inside a `\xff`-bracketed segment, tabwriter's behavior is undefined. This
   shouldn't apply here since the ANSI codes don't contain tabs, but worth
   verifying.

3. **Mixed escaped and non-escaped cells.** The STOPPED status has no ANSI
   codes and no `\xff` markers. If tabwriter treats escaped and non-escaped
   cells differently when computing column widths, this could cause per-row
   misalignment.

4. **The problem may not be ANSI-related at all.** The misalignment could come
   from variable-width content in other columns (ID, AGENT, REPO) where some
   values are much longer than others, combined with the tabwriter parameters
   (minwidth=0, tabwidth=0, padding=2). Worth checking if the issue persists
   with ANSI coloring removed entirely.

Possible approaches to try
---------------------------

- **Remove ANSI codes and test.** Check if the table aligns correctly with no
  coloring at all. This isolates whether ANSI is the cause or a red herring.

- **Apply color after tabwriter.** Write plain text through tabwriter, capture
  the formatted output in a `bytes.Buffer`, then do string replacement to
  inject ANSI codes. This completely avoids tabwriter ever seeing escape
  sequences.

- **Use a table library that handles ANSI natively.** Libraries like
  `github.com/olekukonko/tablewriter` or `github.com/rodaine/table` are
  ANSI-aware and compute column widths based on visible rune width, not byte
  count.

- **Manual column formatting with fmt.Sprintf.** Compute max widths per column
  manually (using visible string width) and use `%-Ns` format verbs. Full
  control, no library quirks.
