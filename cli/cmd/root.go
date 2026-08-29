package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var jsonOutput bool
var appVersion string

func SetVersion(v string) { appVersion = v }

// exit ends the process with a shell exit code.
//
// It is a variable so a test can watch a refusal instead of being killed by it.
// Production never reaches the line after a call.
var exit = os.Exit

// refuse reports an operational failure on stderr and ends the process.
//
// Callers must return its error immediately: in production the process is
// already gone by then, and under test the returned error is what the
// assertion reads.
//
// Exit codes follow the rest of the CLI: 3 means the caller addressed
// something that is not there or is not allowed, 1 means the board host said
// no.
func refuse(code int, format string, a ...interface{}) error {
	err := fmt.Errorf(format, a...)
	fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	exit(code)
	return err
}

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
