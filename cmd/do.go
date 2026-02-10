package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// confirmResult represents the user's response at the confirmation prompt.
type confirmResult int

const (
	confirmAccepted confirmResult = iota
	confirmEdit
)

var doYesFlag bool

var doCmd = &cobra.Command{
	Use:   doCmdStr + " [prompt]",
	Short: "Describe what you want in plain English and AgenC will do it",
	Long: `Describe what you want to do in natural language. AgenC interprets your
request, selects the right repo, and creates a new mission.

If no prompt is given inline, your $EDITOR opens for multi-line input.

Examples:
  agenc do "In dotfiles, add a test agent"
  agenc do "Fix the auth bug in my web app"
  agenc do`,
	Args: cobra.ArbitraryArgs,
	RunE: runDo,
}

func init() {
	doCmd.Flags().BoolVarP(&doYesFlag, yesFlagName, "y", false, "Skip confirmation and execute immediately")
	rootCmd.AddCommand(doCmd)
}

// llmInterpretation is the structured response from the LLM interpreter.
// It determines only which repo to use and whether the user has a task.
// The original user prompt is passed through verbatim — the LLM never rewrites it.
type llmInterpretation struct {
	Repo    string `json:"repo"`
	HasTask bool   `json:"has_task"`
}

// doAction is the resolved action to execute.
type doAction struct {
	Repo   string
	Prompt string
}

func runDo(cmd *cobra.Command, args []string) error {
	agencDirpath, err := getAgencContext()
	if err != nil {
		return err
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read agenc config")
	}

	autoConfirm := doYesFlag || cfg.DoAutoConfirm

	// Get initial user prompt: inline args or editor
	var userPrompt string
	if len(args) > 0 {
		userPrompt = strings.Join(args, " ")
	} else {
		userPrompt, err = openEditorForPrompt("")
		if err != nil {
			return err
		}
		if userPrompt == "" {
			fmt.Println("Empty prompt, aborting.")
			return nil
		}
	}

	// Gather state for the LLM (once — repos and missions don't change mid-loop)
	repoNames, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	missionsSummary, err := buildMissionsSummary()
	if err != nil {
		return stacktrace.Propagate(err, "failed to build missions summary")
	}

	// Interpret → confirm loop. ESC/Ctrl-C at confirmation re-opens the editor.
	for {
		interp, err := interpretWithLLM(userPrompt, repoNames, missionsSummary)
		if err != nil {
			return err
		}

		// Build the action using the original user prompt verbatim
		action := &doAction{Repo: interp.Repo}
		if interp.HasTask {
			action.Prompt = userPrompt
		}

		missionNewArgs := buildMissionNewArgs(action)

		if autoConfirm {
			printCommand(missionNewArgs)
			return executeMissionNew(missionNewArgs)
		}

		result := confirmExecution(missionNewArgs)
		if result == confirmAccepted {
			return executeMissionNew(missionNewArgs)
		}

		// User wants to edit — re-open editor with previous prompt
		userPrompt, err = openEditorForPrompt(userPrompt)
		if err != nil {
			return err
		}
		if userPrompt == "" {
			fmt.Println("Empty prompt, aborting.")
			return nil
		}
	}
}

// openEditorForPrompt opens $EDITOR with a template and returns the user's
// input with comment lines and surrounding whitespace stripped. If
// previousPrompt is non-empty, it is pre-filled below the comment header.
func openEditorForPrompt(previousPrompt string) (string, error) {
	editorEnv := os.Getenv("EDITOR")
	if editorEnv == "" {
		return "", stacktrace.NewError(
			"$EDITOR is not set; either set it or pass your prompt inline: %s %s \"your prompt here\"",
			agencCmdStr, doCmdStr,
		)
	}

	// Create temp file with template
	tmpFile, err := os.CreateTemp("", "agenc-do-*.md")
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to create temp file")
	}
	tmpFilepath := tmpFile.Name()
	defer os.Remove(tmpFilepath)

	header := "# Tell AgenC what to do.\n# Lines starting with # are ignored.\n# Save and exit when done. Leave empty to abort.\n\n"
	content := header + previousPrompt + "\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return "", stacktrace.Propagate(err, "failed to write editor template")
	}
	tmpFile.Close()

	// Parse editor command (supports "code --wait" style)
	editorParts := strings.Fields(editorEnv)
	editorBinary := editorParts[0]
	editorArgs := editorParts[1:]

	resolvedBinary, err := exec.LookPath(editorBinary)
	if err != nil {
		return "", stacktrace.Propagate(err, "editor binary '%s' not found in PATH", editorBinary)
	}

	// If vim/nvim, add args to position cursor at end in insert mode
	baseName := filepath.Base(resolvedBinary)
	if baseName == "vim" || baseName == "nvim" {
		editorArgs = append(editorArgs, "+normal G", "+startinsert")
	}

	editorArgs = append(editorArgs, tmpFilepath)

	editorCmd := exec.Command(resolvedBinary, editorArgs...)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return "", stacktrace.Propagate(err, "editor exited with error")
	}

	// Read back and strip comments
	edited, err := os.ReadFile(tmpFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read editor output")
	}

	var lines []string
	for line := range strings.SplitSeq(string(edited), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// buildMissionsSummary returns a plaintext summary of current missions
// suitable for the LLM context.
func buildMissionsSummary() (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return "No active missions.", nil
	}

	var sb strings.Builder
	for _, m := range missions {
		status := getMissionStatus(m.ID, m.Status)
		sessionName := truncatePrompt(m.SessionName, defaultPromptMaxLen)
		repoDisplay := m.GitRepo
		if repoDisplay == "" {
			repoDisplay = "--"
		}
		fmt.Fprintf(&sb, "%s  %s  %s  %s  %s\n",
			formatLastActive(m.LastHeartbeat),
			m.ShortID,
			status,
			sessionName,
			repoDisplay,
		)
	}

	return strings.TrimSpace(sb.String()), nil
}

// interpretWithLLM sends the user prompt and system state to Claude for
// interpretation. It determines which repo to target and whether the user
// has an actual task. The original user prompt is never rewritten by the LLM.
func interpretWithLLM(userPrompt string, repoNames []string, missionsSummary string) (*llmInterpretation, error) {
	// Check that claude is available
	if _, err := exec.LookPath("claude"); err != nil {
		return nil, stacktrace.NewError(
			"'claude' CLI not found in PATH; install it from https://docs.anthropic.com/en/docs/claude-code/overview",
		)
	}

	reposSection := "None"
	if len(repoNames) > 0 {
		reposSection = strings.Join(repoNames, "\n")
	}

	systemPrompt := fmt.Sprintf(`You are AgenC's command interpreter. Given a user request and the current system state, determine which repo to use and whether the user has a task for the agent.

AVAILABLE REPOS:
%s

CURRENT MISSIONS:
%s

RULES:
- The only action is creating a new mission (starting a Claude agent in a repo).
- Match the user's repo reference generously: "dotfiles" → "github.com/mieubrisse/dotfiles".
- If no repo matches, set repo to "" for a blank mission.
- Distinguish between requests that contain a TASK for the agent vs requests that just want to OPEN a repo.
  - "In dotfiles, add a test agent" → has_task = true
  - "open my todoist project" → has_task = false
  - "fix the auth bug in web app" → has_task = true
  - "launch dotfiles" → has_task = false
- Do NOT extract or rewrite the user's prompt. Only determine the repo and whether a task exists.

Respond with ONLY a JSON object (no markdown fences, no explanation):
{"repo": "github.com/owner/repo", "has_task": true}`, reposSection, missionsSummary)

	claudeCmd := exec.Command("claude", "-p", "--model", "haiku")
	claudeCmd.Stdin = strings.NewReader(systemPrompt + "\n\nUser request: " + userPrompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	claudeCmd.Stdout = &stdout
	claudeCmd.Stderr = &stderr

	if err := claudeCmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, stacktrace.NewError("claude failed: %s", stderrStr)
		}
		return nil, stacktrace.Propagate(err, "claude command failed")
	}

	return parseLLMResponse(stdout.String())
}

// parseLLMResponse extracts an llmInterpretation from the LLM's raw output,
// handling optional markdown fences.
func parseLLMResponse(raw string) (*llmInterpretation, error) {
	cleaned := strings.TrimSpace(raw)

	// Strip markdown fences if present
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		// Remove first and last lines (fences)
		if len(lines) >= 3 {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var interp llmInterpretation
	if err := json.Unmarshal([]byte(cleaned), &interp); err != nil {
		return nil, stacktrace.NewError("failed to parse LLM response as JSON:\n%s", cleaned)
	}

	return &interp, nil
}

// buildMissionNewArgs constructs the argument list for `agenc mission new`.
func buildMissionNewArgs(action *doAction) []string {
	var args []string
	if action.Repo != "" {
		args = append(args, action.Repo)
	} else {
		args = append(args, "--"+blankFlagName)
	}
	if action.Prompt != "" {
		args = append(args, "--"+promptFlagName, action.Prompt)
	}
	return args
}

// formatCommandDisplay returns a formatted string showing the command that will be executed.
func formatCommandDisplay(missionNewArgs []string) string {
	binaryStr := agencCmdStr
	quotedArgs := shellQuoteArgs(missionNewArgs)

	if isInsideAgencTmux() {
		return fmt.Sprintf(
			"%s%s tmux window new -- %s %s%s %s %s",
			ansiDarkGray, binaryStr, binaryStr, ansiReset,
			ansiBold, missionCmdStr+" "+newCmdStr+" "+strings.Join(quotedArgs, " "), ansiReset,
		)
	}

	return fmt.Sprintf(
		"%s%s %s %s%s",
		ansiBold, binaryStr, missionCmdStr+" "+newCmdStr, strings.Join(quotedArgs, " "), ansiReset,
	)
}

// printCommand shows the user what will be executed, without waiting for confirmation.
func printCommand(missionNewArgs []string) {
	fmt.Printf("\n%s\n\n", formatCommandDisplay(missionNewArgs))
}

// confirmExecution shows the user what will be executed and waits for a
// single keypress. ENTER confirms; ESC or Ctrl-C returns to the editor.
func confirmExecution(missionNewArgs []string) confirmResult {
	fmt.Printf("\n%s\n\n", formatCommandDisplay(missionNewArgs))
	fmt.Printf("Press %sENTER%s to run, %sESC%s to edit\n", ansiBold, ansiReset, ansiBold, ansiReset)

	result := readConfirmKey()
	// Print a newline after the keypress so subsequent output starts clean
	fmt.Println()
	return result
}

// readConfirmKey reads a single keypress in raw terminal mode.
// Returns confirmAccepted for ENTER, confirmEdit for ESC or Ctrl-C.
func readConfirmKey() confirmResult {
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// If we can't enter raw mode (e.g. piped stdin), fall back to accepting
		return confirmAccepted
	}
	defer term.Restore(fd, oldState)

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return confirmEdit
		}

		switch buf[0] {
		case '\r', '\n': // ENTER
			return confirmAccepted
		case 0x1b: // ESC
			return confirmEdit
		case 0x03: // Ctrl-C
			return confirmEdit
		}
		// Ignore other keys — wait for a recognized one
	}
}

// shellQuoteArgs quotes arguments that contain special shell characters.
func shellQuoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\$`|&;(){}[]<>?*~!#") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return quoted
}

// executeMissionNew runs `agenc mission new` with the given args, either
// in a new tmux window (if inside agenc tmux) or directly.
func executeMissionNew(missionNewArgs []string) error {
	binaryFilepath, err := resolveAgencBinaryPath()
	if err != nil {
		return err
	}

	if isInsideAgencTmux() {
		// agenc tmux window new -- agenc mission new <args...>
		execArgs := []string{
			tmuxCmdStr, windowCmdStr, newCmdStr,
			"--",
			binaryFilepath,
			missionCmdStr, newCmdStr,
		}
		execArgs = append(execArgs, missionNewArgs...)

		execCmd := exec.Command(binaryFilepath, execArgs...)
		execCmd.Stdin = os.Stdin
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr

		if err := execCmd.Run(); err != nil {
			return stacktrace.Propagate(err, "failed to create tmux window for mission")
		}
		return nil
	}

	// Direct execution: agenc mission new <args...>
	execArgs := []string{missionCmdStr, newCmdStr}
	execArgs = append(execArgs, missionNewArgs...)

	execCmd := exec.Command(binaryFilepath, execArgs...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to execute mission new")
	}
	return nil
}
