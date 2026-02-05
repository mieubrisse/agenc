package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var templateAddNicknameFlag string
var templateAddDefaultFlag string

var templateAddCmd = &cobra.Command{
	Use:   addCmdStr + " <repo>",
	Short: "Add an agent template from a GitHub repository",
	Long: fmt.Sprintf(`Add an agent template from a GitHub repository.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

You can also use search terms to find an existing repo in your library:
  %s %s %s my repo           - searches for repos matching "my repo"

The clone protocol is auto-detected: explicit URLs preserve their protocol,
while shorthand references (owner/repo) use the protocol inferred from
existing repos in the library. If no repos exist, you'll be prompted to choose.`,
		agencCmdStr, templateCmdStr, addCmdStr),
	Args: cobra.MinimumNArgs(1),
	RunE: runTemplateAdd,
}

func init() {
	templateAddCmd.Flags().StringVar(&templateAddNicknameFlag, nicknameFlagName, "", nicknameFlagDesc)
	templateAddCmd.Flags().StringVar(&templateAddDefaultFlag, defaultFlagName, "", defaultFlagDesc())
	templateCmd.AddCommand(templateAddCmd)
}

func runTemplateAdd(cmd *cobra.Command, args []string) error {
	// Join args - could be a single repo ref or multiple search terms
	input := args[0]
	if len(args) > 1 {
		// Multiple args: either multiple repo refs or search terms
		// If the first arg doesn't look like a repo ref, treat all as search terms
		if !looksLikeRepoReference(args[0]) {
			input = strings.Join(args, " ")
		}
	}

	result, err := ResolveRepoInput(agencDirpath, input, false, "Select repo to add as template: ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve repo")
	}

	added, err := addTemplateToLibrary(agencDirpath, result.RepoName, templateAddNicknameFlag, templateAddDefaultFlag)
	if err != nil {
		return err
	}

	if added {
		printTemplateAdded(result.RepoName)
	} else {
		printTemplateAlreadyExists(result.RepoName)
	}
	return nil
}
