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

var boardCmd = &cobra.Command{
	Use:   "board",
	Short: "Manage bingo boards (create, show)",
}

var boardCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new bingo board",
	Example: `  bingo board create --name mesoscape-pvm --teams "Raiders,Slayers" --size 5x5
  bingo board create --name clan-skilling --teams "Team A,Team B,Team C" --size 3x3 --password meso`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		teamsStr, _ := cmd.Flags().GetString("teams")
		sizeStr, _ := cmd.Flags().GetString("size")
		password, _ := cmd.Flags().GetString("password")

		if name == "" {
			return fmt.Errorf("--name is required")
		}

		teamNames := strings.Split(teamsStr, ",")
		for i := range teamNames {
			teamNames[i] = strings.TrimSpace(teamNames[i])
		}
		teamCount := len(teamNames)
		if teamCount < 1 || (teamCount == 1 && teamNames[0] == "") {
			teamNames = []string{"Team 1", "Team 2"}
			teamCount = 2
		}

		rows, cols := 5, 5
		if sizeStr != "" {
			fmt.Sscanf(sizeStr, "%dx%d", &cols, &rows)
		}

		if password == "" {
			password = "bingo"
		}

		adminPw := fmt.Sprintf("admin-%s-%d", name[:min(len(name), 6)], os.Getpid()%10000)

		client := api.NewClient()

		if err := client.CreateBoard(name, adminPw, password, rows, cols, teamCount); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating board: %s\n", err)
			os.Exit(1)
		}

		if err := client.RenameTeams(name, adminPw, teamNames, rows, cols); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to rename teams: %s\n", err)
		}

		store := state.NewStore()
		store.Save(name, state.BoardState{
			AdminPassword:   adminPw,
			GeneralPassword: password,
			Teams:           teamNames,
			Size:            [2]int{cols, rows},
		})

		boardURL := fmt.Sprintf("https://pattyrich.github.io/github-pages/#/bingo/%s?password=%s", name, password)

		if jsonOutput {
			out, _ := json.Marshal(map[string]interface{}{
				"board_name": name,
				"board_url":  boardURL,
				"teams":      teamNames,
				"size":       fmt.Sprintf("%dx%d", cols, rows),
			})
			fmt.Println(string(out))
		} else {
			fmt.Printf("Board '%s' created!\n", name)
			fmt.Printf("URL: %s\n", boardURL)
			fmt.Printf("Teams: %s\n", strings.Join(teamNames, ", "))
			fmt.Printf("Size: %dx%d\n", cols, rows)
		}
		return nil
	},
}

var boardShowCmd = &cobra.Command{
	Use:     "show",
	Short:   "Show board state and tiles",
	Example: `  bingo board show --name mesoscape-pvm`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			return fmt.Errorf("--name is required")
		}

		store := state.NewStore()
		board, err := store.Load(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: board '%s' not found in local state. Was it created with this CLI?\n", name)
			os.Exit(3)
		}

		client := api.NewClient()
		data, err := client.GetBoard(name, board.AdminPassword, "admin")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			out, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Printf("Board: %s\n", name)
			fmt.Printf("URL: https://pattyrich.github.io/github-pages/#/bingo/%s?password=%s\n", name, board.GeneralPassword)
			fmt.Printf("Teams: %s\n", strings.Join(board.Teams, ", "))
			if tiles, ok := data["boardData"].([]interface{}); ok {
				for col, colData := range tiles {
					if rows, ok := colData.([]interface{}); ok {
						for row, rowData := range rows {
							if tile, ok := rowData.(map[string]interface{}); ok {
								title, _ := tile["title"].(string)
								if title != "" {
									points, _ := tile["points"].(float64)
									fmt.Printf("  [%d,%d] %s (%0.0f pts)\n", col, row, title, points)
								}
							}
						}
					}
				}
			}
		}
		return nil
	},
}

func init() {
	boardCreateCmd.Flags().String("name", "", "Board name (required)")
	boardCreateCmd.Flags().String("teams", "Team 1,Team 2", "Comma-separated team names")
	boardCreateCmd.Flags().String("size", "5x5", "Board size (e.g., 3x3, 5x5)")
	boardCreateCmd.Flags().String("password", "", "General password for viewers (default: bingo)")

	boardShowCmd.Flags().String("name", "", "Board name (required)")

	boardCmd.AddCommand(boardCreateCmd)
	boardCmd.AddCommand(boardShowCmd)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
