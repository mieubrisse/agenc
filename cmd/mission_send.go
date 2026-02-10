package cmd

import (
	"github.com/spf13/cobra"
)

var missionSendCmd = &cobra.Command{
	Use:   sendCmdStr,
	Short: "Send messages to a running mission wrapper",
}

func init() {
	missionCmd.AddCommand(missionSendCmd)
}
