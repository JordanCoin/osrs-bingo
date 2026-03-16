package cmd

import (
	"github.com/spf13/cobra"
)

var jsonOutput bool
var appVersion string

func SetVersion(v string) { appVersion = v }

var rootCmd = &cobra.Command{
	Use:   "bingo",
	Short: "Manage OSRS bingo boards via the PattyRich API",
	Long: `Create, manage, and track bingo boards for Old School RuneScape clan events.
Boards are hosted at pattyrich.github.io and managed via the praynr.com API.

Agent-friendly: supports --json output, meaningful exit codes, no interactive prompts.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.AddCommand(boardCmd)
	rootCmd.AddCommand(tileCmd)
	rootCmd.AddCommand(teamsCmd)
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("bingo " + appVersion)
		},
	})
}
