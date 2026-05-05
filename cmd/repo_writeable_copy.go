package cmd

import "github.com/spf13/cobra"

var repoWriteableCopyCmd = &cobra.Command{
	Use:   writeableCopyCmdStr,
	Short: "Manage writeable copies of repos",
	Long: `A writeable copy is an additional clone of a repo at a user-chosen path
(e.g. ~/app/dotfiles) that AgenC keeps continuously synced with the repo's
git remote: local edits are auto-committed and pushed, remote changes are
pulled and rebased. Setting a writeable copy implies that the repo is
always-synced; the implication is enforced by AgenC.`,
}

func init() {
	repoCmd.AddCommand(repoWriteableCopyCmd)
}
