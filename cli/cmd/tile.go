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

var tileCmd = &cobra.Command{
	Use:   "tile",
	Short: "Manage board tiles (add, list, mark)",
}

var tileAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a tile to the next empty slot",
	Example: `  bingo tile add --board mesoscape-pvm --title "Twisted Bow" --points 10
  bingo tile add --board mesoscape-pvm --title "Fire Cape" --points 3 --image "https://oldschool.runescape.wiki/images/thumb/Fire_cape_detail.png/150px-Fire_cape_detail.png"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		boardName, _ := cmd.Flags().GetString("board")
		title, _ := cmd.Flags().GetString("title")
		points, _ := cmd.Flags().GetInt("points")
		description, _ := cmd.Flags().GetString("description")
		imageURL, _ := cmd.Flags().GetString("image")

		if boardName == "" || title == "" {
			return fmt.Errorf("--board and --title are required")
		}

		store := state.NewStore()
		board, err := store.Load(boardName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: board '%s' not found locally. Run 'bingo board create' first.\n", boardName)
			os.Exit(3)
		}

		client := api.NewClient()
		data, err := client.GetBoard(boardName, board.AdminPassword, "admin")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching board: %s\n", err)
			os.Exit(1)
		}

		col, row, err := findEmptySlot(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		var image interface{} = ""
		if imageURL != "" {
			image = map[string]interface{}{"url": imageURL, "opacity": 100}
		}

		info := map[string]interface{}{
			"title":       title,
			"description": description,
			"points":      points,
			"image":       image,
		}

		err = client.UpdateBoard(boardName, board.AdminPassword, "admin", col, row, info)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			out, _ := json.Marshal(map[string]interface{}{
				"action": "tile_added", "title": title, "col": col, "row": row, "points": points,
			})
			fmt.Println(string(out))
		} else {
			fmt.Printf("Added '%s' at [%d,%d] (%d pts)\n", title, col, row, points)
		}
		return nil
	},
}

var tileMarkCmd = &cobra.Command{
	Use:   "mark",
	Short: "Mark a tile as complete for a team",
	Long: `Mark a tile as complete for a specific team. The tile is looked up by name
(not row/col), so you never need to guess positions. The team is looked up
by name too.

Uses the GENERAL password (not admin) to update completion state.`,
	Example: `  bingo tile mark --board mesoscape-pvm --tile "Twisted Bow" --team Raiders`,
	RunE: func(cmd *cobra.Command, args []string) error {
		boardName, _ := cmd.Flags().GetString("board")
		tileName, _ := cmd.Flags().GetString("tile")
		teamName, _ := cmd.Flags().GetString("team")

		if boardName == "" || tileName == "" || teamName == "" {
			return fmt.Errorf("--board, --tile, and --team are required")
		}

		store := state.NewStore()
		board, err := store.Load(boardName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: board '%s' not found locally\n", boardName)
			os.Exit(3)
		}

		client := api.NewClient()
		data, err := client.GetBoard(boardName, board.AdminPassword, "admin")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching board: %s\n", err)
			os.Exit(1)
		}

		col, row, points, err := findTileByName(data, tileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(3)
		}

		teamIndex := -1
		teamNameLower := strings.ToLower(teamName)
		for i, t := range board.Teams {
			if strings.ToLower(t) == teamNameLower {
				teamIndex = i
				break
			}
		}
		if teamIndex == -1 {
			fmt.Fprintf(os.Stderr, "Error: team '%s' not found. Available: %s\n", teamName, strings.Join(board.Teams, ", "))
			os.Exit(3)
		}

		// Mark complete using GENERAL password
		info := map[string]interface{}{
			"teamId":     teamIndex,
			"checked":    true,
			"currPoints": points,
		}
		err = client.UpdateBoard(boardName, board.GeneralPassword, "general", col, row, info)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			out, _ := json.Marshal(map[string]interface{}{
				"action": "tile_marked", "tile": tileName, "team": teamName, "points": points,
			})
			fmt.Println(string(out))
		} else {
			fmt.Printf("Marked '%s' complete for %s (%d pts)\n", tileName, teamName, points)
		}
		return nil
	},
}

var tileListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all tiles on the board",
	Example: `  bingo tile list --board mesoscape-pvm`,
	RunE: func(cmd *cobra.Command, args []string) error {
		boardName, _ := cmd.Flags().GetString("board")
		if boardName == "" {
			return fmt.Errorf("--board is required")
		}

		store := state.NewStore()
		board, err := store.Load(boardName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: board '%s' not found locally\n", boardName)
			os.Exit(3)
		}

		client := api.NewClient()
		data, err := client.GetBoard(boardName, board.AdminPassword, "admin")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		type TileInfo struct {
			Col    int    `json:"col"`
			Row    int    `json:"row"`
			Title  string `json:"title"`
			Points int    `json:"points"`
		}
		var tiles []TileInfo

		if boardData, ok := data["boardData"].([]interface{}); ok {
			for col, colData := range boardData {
				if rows, ok := colData.([]interface{}); ok {
					for row, rowData := range rows {
						if tile, ok := rowData.(map[string]interface{}); ok {
							title, _ := tile["title"].(string)
							if title != "" {
								pts, _ := tile["points"].(float64)
								tiles = append(tiles, TileInfo{Col: col, Row: row, Title: title, Points: int(pts)})
							}
						}
					}
				}
			}
		}

		if jsonOutput {
			out, _ := json.MarshalIndent(tiles, "", "  ")
			fmt.Println(string(out))
		} else {
			if len(tiles) == 0 {
				fmt.Println("No tiles on the board yet.")
			} else {
				for _, t := range tiles {
					fmt.Printf("  [%d,%d] %s (%d pts)\n", t.Col, t.Row, t.Title, t.Points)
				}
			}
		}
		return nil
	},
}

func init() {
	tileAddCmd.Flags().String("board", "", "Board name (required)")
	tileAddCmd.Flags().String("title", "", "Tile title (required)")
	tileAddCmd.Flags().Int("points", 1, "Point value")
	tileAddCmd.Flags().String("description", "", "Tile description")
	tileAddCmd.Flags().String("image", "", "Image URL")

	tileMarkCmd.Flags().String("board", "", "Board name (required)")
	tileMarkCmd.Flags().String("tile", "", "Tile title to mark (required)")
	tileMarkCmd.Flags().String("team", "", "Team name (required)")

	tileListCmd.Flags().String("board", "", "Board name (required)")

	tileCmd.AddCommand(tileAddCmd)
	tileCmd.AddCommand(tileMarkCmd)
	tileCmd.AddCommand(tileListCmd)
}

func findEmptySlot(data map[string]interface{}) (int, int, error) {
	boardData, ok := data["boardData"].([]interface{})
	if !ok {
		return 0, 0, fmt.Errorf("invalid board data")
	}
	for col, colData := range boardData {
		if rows, ok := colData.([]interface{}); ok {
			for row, rowData := range rows {
				if tile, ok := rowData.(map[string]interface{}); ok {
					title, _ := tile["title"].(string)
					// Treat "Example Tile" (PattyRich default) as empty
					if title == "" || title == "Example Tile" {
						return col, row, nil
					}
				}
			}
		}
	}
	return 0, 0, fmt.Errorf("no empty slots — board is full")
}

func findTileByName(data map[string]interface{}, name string) (int, int, int, error) {
	nameLower := strings.ToLower(name)
	boardData, ok := data["boardData"].([]interface{})
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid board data")
	}
	for col, colData := range boardData {
		if rows, ok := colData.([]interface{}); ok {
			for row, rowData := range rows {
				if tile, ok := rowData.(map[string]interface{}); ok {
					title, _ := tile["title"].(string)
					if strings.ToLower(title) == nameLower {
						pts, _ := tile["points"].(float64)
						return col, row, int(pts), nil
					}
				}
			}
		}
	}
	return 0, 0, 0, fmt.Errorf("tile '%s' not found on the board", name)
}
