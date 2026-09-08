package session

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Everything in a transcript that is not a user or assistant message is an
// event record, and Claude Code writes a lot of them: hook results, turn
// timings, compaction boundaries, queue operations, attachments, slash-command
// invocations, API errors. Rendering them all by default would bury the
// conversation; rendering none of them — which is what the formatter used to do
// — silently drops the records that explain why the conversation looks the way
// it does.
//
// The split below is by that question. An event renders by default when a
// reader who cannot see it would misread the conversation: a compaction that
// deleted the preceding context, a slash command that produced the next turn, a
// queued human instruction, an error that truncated a response. Everything else
// is bookkeeping and renders only under Verbose.

// defaultVisibleSystemSubtypes are the system-record subtypes that change how
// the surrounding conversation should be read.
var defaultVisibleSystemSubtypes = map[string]bool{
	"compact_boundary":       true,
	"local_command":          true,
	"api_error":              true,
	"agents_killed":          true,
	"model_refusal_fallback": true,
	"informational":          true,
	"scheduled_task_fire":    true,
}

// formatEventEntry renders a non-conversation record, or "" when the record is
// not visible under these options.
func (f *conversationFormatter) formatEventEntry(record jsonlRecord) string {
	switch record.Type {
	case "system":
		return f.formatSystemEntry(record)
	case "queue-operation":
		return f.formatQueueEntry(record)
	case "attachment":
		return f.formatAttachmentEntry(record)
	case "summary":
		if record.Summary == "" {
			return ""
		}
		return formatRoleHeader("SUMMARY", record.Timestamp) + "\n" + record.Summary + "\n"
	default:
		if !f.opts.Verbose || record.Type == "" {
			return ""
		}
		return formatEventLine(record.Timestamp, strings.ToUpper(record.Type), minorRecordDetail(record))
	}
}

// minorRecordDetail returns the payload of the small single-purpose record
// types, each of which carries its whole meaning in one field. Rendering the
// bare type name instead reports that something happened while discarding what
// it was — a mode switch, a rename, the prompt that started a turn.
func minorRecordDetail(record jsonlRecord) string {
	return firstNonEmpty(
		record.CustomTitle,
		record.AgencCustomTitle,
		record.AITitle,
		record.AgentName,
		record.Mode,
		record.PermissionMode,
		record.LastPrompt,
	)
}

// formatSystemEntry renders a system record. Compaction boundaries are special
// cased because they are the single most consequential event in a transcript:
// everything before one was discarded from the model's context, and a reader
// who does not see the boundary will read the summary that follows as if it
// were a human message.
func (f *conversationFormatter) formatSystemEntry(record jsonlRecord) string {
	switch record.Subtype {
	case "compact_boundary":
		return formatCompactBoundary(record)
	case "stop_hook_summary":
		return f.formatStopHookSummary(record)
	case "turn_duration":
		if !f.opts.Verbose {
			return ""
		}
		return formatEventLine(record.Timestamp, "TURN",
			fmt.Sprintf("%s over %d messages", formatDurationMs(record.DurationMs), record.MessageCount))
	case "api_error":
		detail := "API error"
		if record.MaxRetries > 0 {
			detail = fmt.Sprintf("API error, retry %d/%d", record.RetryAttempt, record.MaxRetries)
		}
		return formatEventLine(record.Timestamp, "ERROR", detail)
	case "local_command":
		return formatEventLine(record.Timestamp, "COMMAND", f.eventText(record.Content))
	case "agents_killed":
		return formatEventLine(record.Timestamp, "AGENTS KILLED", f.eventText(record.Content))
	}

	if !defaultVisibleSystemSubtypes[record.Subtype] && !f.opts.Verbose {
		return ""
	}
	label := strings.ToUpper(strings.ReplaceAll(orUnknown(record.Subtype), "_", " "))
	if record.Level == "warning" || record.Level == "error" {
		label = strings.ToUpper(record.Level) + " " + label
	}
	return formatEventLine(record.Timestamp, label, f.eventText(record.Content))
}

// formatCompactBoundary renders a compaction with the numbers that say how much
// context it destroyed, so the reader can judge what the summary below it is
// standing in for.
func formatCompactBoundary(record jsonlRecord) string {
	detail := ""
	if m := record.CompactMetadata; m != nil {
		var parts []string
		if m.Trigger != "" {
			parts = append(parts, "trigger="+m.Trigger)
		}
		if m.PreTokens > 0 {
			parts = append(parts, fmt.Sprintf("%d tok before", int(m.PreTokens)))
		}
		if m.PostTokens > 0 {
			parts = append(parts, fmt.Sprintf("%d tok after", int(m.PostTokens)))
		}
		if m.CumulativeDroppedTokens > 0 {
			parts = append(parts, fmt.Sprintf("%d tok dropped cumulatively", int(m.CumulativeDroppedTokens)))
		}
		detail = strings.Join(parts, ", ")
	}
	return formatEventLine(record.Timestamp, "COMPACTED", detail)
}

// formatStopHookSummary renders a Stop-hook result. A clean run is bookkeeping;
// a hook that errored or blocked continuation changed what the agent did next
// and is shown by default.
func (f *conversationFormatter) formatStopHookSummary(record jsonlRecord) string {
	errs := rawArrayLen(record.HookErrors)
	if errs == 0 && !record.PreventedContinuation && !f.opts.Verbose {
		return ""
	}

	var parts []string
	if errs > 0 {
		parts = append(parts, fmt.Sprintf("%d hook error(s)", errs))
	}
	if record.PreventedContinuation {
		parts = append(parts, "blocked continuation")
	}
	if record.StopReason != "" {
		parts = append(parts, "reason: "+record.StopReason)
	}
	if f.opts.Verbose {
		if n := rawArrayLen(record.HookInfos); n > 0 {
			parts = append(parts, fmt.Sprintf("%d hook(s) ran", n))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "ran clean")
	}
	return formatEventLine(record.Timestamp, "STOP HOOK", strings.Join(parts, ", "))
}

// formatQueueEntry renders a queue operation. An enqueue or popAll carries the
// text of an instruction that arrived mid-turn — the case where a message the
// user actually sent never appears as a USER record at all, because the turn it
// landed in was still running. The dequeue and remove churn around it is
// bookkeeping.
func (f *conversationFormatter) formatQueueEntry(record jsonlRecord) string {
	text := f.eventText(record.Content)
	switch record.Operation {
	case "enqueue", "popAll":
		if text == "" {
			return ""
		}
		return formatEventLine(record.Timestamp, "QUEUED "+record.Operation, text)
	case "dequeue", "remove":
		if !f.opts.Verbose {
			return ""
		}
		return formatEventLine(record.Timestamp, "QUEUE "+record.Operation, text)
	default:
		if !f.opts.Verbose || record.Operation == "" {
			return ""
		}
		return formatEventLine(record.Timestamp, "QUEUE "+record.Operation, text)
	}
}

// attachmentSummary is the subset of attachment payload fields used to build a
// one-line label. Attachments carry a dozen different shapes; these are the
// fields that identify which one it is and what it referred to.
type attachmentSummary struct {
	Type        string          `json:"type"`
	Filename    string          `json:"filename"`
	Path        string          `json:"path"`
	DisplayPath string          `json:"displayPath"`
	Prompt      string          `json:"prompt"`
	Content     json.RawMessage `json:"content"`
	HookName    string          `json:"hookName"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Banner      string          `json:"banner"`
	Style       string          `json:"style"`
	URL         string          `json:"url"`
}

// formatAttachmentEntry renders an attachment record. Only queued_command is
// visible by default: it is the mid-turn instruction the user typed, and it is
// otherwise invisible in the transcript.
func (f *conversationFormatter) formatAttachmentEntry(record jsonlRecord) string {
	var att attachmentSummary
	if err := json.Unmarshal(record.Attachment, &att); err != nil {
		return ""
	}

	if att.Type == "queued_command" {
		if att.Prompt == "" {
			return ""
		}
		return formatEventLine(record.Timestamp, "QUEUED COMMAND", att.Prompt)
	}

	if !f.opts.Verbose {
		return ""
	}

	detail := firstNonEmpty(
		att.DisplayPath,
		att.Filename,
		att.Path,
		att.Banner,
		att.HookName,
		att.Description,
		att.Style,
		att.URL,
		f.eventText(att.Content),
	)
	return formatEventLine(record.Timestamp, "ATTACHMENT "+orUnknown(att.Type), detail)
}

// formatEventLine renders one event as a bracketed header plus an optional
// detail, truncated to a single readable line's worth of text.
func formatEventLine(timestamp string, label string, detail string) string {
	header := formatRoleHeader(label, timestamp)
	detail = collapseWhitespace(detail)
	if detail == "" {
		return header + "\n"
	}
	return header + " " + truncate(detail, maxEventContentLen) + "\n"
}

// eventText coerces a record's content field into text. Content is written as a
// bare string on system and queue records, and as an array of strings or of
// {text}/{content} objects on hook attachments.
func (f *conversationFormatter) eventText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}

	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return strings.Join(asStrings, " ")
	}

	var asBlocks []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &asBlocks); err == nil {
		var parts []string
		for _, b := range asBlocks {
			if t := firstNonEmpty(b.Text, b.Content); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}

	var asObject struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &asObject); err == nil {
		return firstNonEmpty(asObject.Text, asObject.Content)
	}
	return ""
}

// rawArrayLen returns the element count of a raw JSON array, or 0 when the
// value is absent, null, or not an array.
func rawArrayLen(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}

// collapseWhitespace folds newlines and runs of spaces into single spaces so an
// event detail occupies exactly one line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
