package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/JordanCoin/osrs-bingo/cli/internal/api"
	"github.com/JordanCoin/osrs-bingo/cli/internal/state"
	"github.com/spf13/cobra"
)

var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "Manage board teams (rename)",
	// A parent with subcommands otherwise accepts anything, prints its own
	// help, and exits 0 — so `bingo tile unmark` looked like success back when
	// unmark did not exist. Silent success is the worst answer to give a script.
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unknown subcommand %q for %q\n\nRun '%s --help' for usage",
				args[0], cmd.CommandPath(), cmd.CommandPath())
		}
		return cmd.Help()
	},
}

var teamsRenameCmd = &cobra.Command{
	Use:     "rename",
	Short:   "Rename teams on a board",
	Example: `  bingo teams rename --board mesoscape-pvm --teams "Alpha,Beta"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		boardName, _ := cmd.Flags().GetString("board")
		teamsStr, _ := cmd.Flags().GetString("teams")

		if boardName == "" || teamsStr == "" {
			return fmt.Errorf("--board and --teams are required")
		}

		store := state.NewStore()
		board, err := store.Load(boardName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: board '%s' not found locally\n", boardName)
			os.Exit(3)
		}

		teamNames := strings.Split(teamsStr, ",")
		for i := range teamNames {
			teamNames[i] = strings.TrimSpace(teamNames[i])
		}

		client := api.NewClient()
		err = client.RenameTeams(boardName, board.AdminPassword, teamNames, board.Size[1], board.Size[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		board.Teams = teamNames
		store.Save(boardName, *board)

		if jsonOutput {
			out, _ := json.Marshal(map[string]interface{}{"action": "teams_renamed", "teams": teamNames})
			fmt.Println(string(out))
		} else {
			fmt.Printf("Teams renamed: %s\n", strings.Join(teamNames, ", "))
		}
		return nil
	},
}

func init() {
	teamsRenameCmd.Flags().String("board", "", "Board name (required)")
	teamsRenameCmd.Flags().String("teams", "", "Comma-separated team names (required)")
	teamsCmd.AddCommand(teamsRenameCmd)
}
