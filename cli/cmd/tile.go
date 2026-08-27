package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/JordanCoin/osrs-bingo/cli/internal/api"
	"github.com/JordanCoin/osrs-bingo/cli/internal/imageprep"
	"github.com/JordanCoin/osrs-bingo/cli/internal/state"
	"github.com/spf13/cobra"
)

var tileCmd = &cobra.Command{
	Use:   "tile",
	Short: "Manage board tiles (add, list, mark, unmark)",
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

var tileAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a tile to the next empty slot",
	Long: `Add a tile to the next empty slot on the board.

Tile art comes in one of two ways:

  --image URL         stored verbatim, so the URL must outlive the event
  --image-file PATH   read from disk, downscaled to 512px, and handed to the
                      board host as a data URI, which it re-hosts permanently

Prefer --image-file for anything uploaded by a person. A Discord attachment URL
is signed and expires in about two weeks, which on a long event means a board
full of broken images partway through, with nothing saying why.`,
	Example: `  bingo tile add --board mesoscape-pvm --title "Twisted Bow" --points 10
  bingo tile add --board mesoscape-pvm --title "Fire Cape" --points 3 --image "https://oldschool.runescape.wiki/images/thumb/Fire_cape_detail.png/150px-Fire_cape_detail.png"
  bingo tile add --board mesoscape-pvm --title "Clan art" --points 5 --image-file ./tile.png`,
	RunE: func(cmd *cobra.Command, args []string) error {
		boardName, _ := cmd.Flags().GetString("board")
		title, _ := cmd.Flags().GetString("title")
		points, _ := cmd.Flags().GetInt("points")
		description, _ := cmd.Flags().GetString("description")
		imageURL, _ := cmd.Flags().GetString("image")
		imageFile, _ := cmd.Flags().GetString("image-file")

		if boardName == "" || title == "" {
			return fmt.Errorf("--board and --title are required")
		}
		if imageURL != "" && imageFile != "" {
			return fmt.Errorf("give either --image or --image-file, not both")
		}

		// Prepare the image before anything touches the network. A bad file is
		// the caller's mistake, and it should cost them an error, not two API
		// round trips and a half-added tile.
		if imageFile != "" {
			dataURI, err := imageprep.DataURIFromFile(imageFile)
			if err != nil {
				return err
			}
			imageURL = dataURI
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

// resolveTile finds the tile a command is addressing, by position when
// --col/--row are given and by title otherwise. Position wins because it is
// the board's own key; a title is a guess that can collide.
func resolveTile(cmd *cobra.Command, data map[string]interface{}) (col, row, points int, err error) {
	tileName, _ := cmd.Flags().GetString("tile")
	hasCol := cmd.Flags().Changed("col")
	hasRow := cmd.Flags().Changed("row")

	if hasCol != hasRow {
		return 0, 0, 0, fmt.Errorf("--col and --row must be given together")
	}
	if hasCol {
		col, _ = cmd.Flags().GetInt("col")
		row, _ = cmd.Flags().GetInt("row")
		points, err = findTileAt(data, col, row)
		return col, row, points, err
	}
	if tileName == "" {
		return 0, 0, 0, fmt.Errorf("give either --tile, or --col and --row")
	}
	return findTileByName(data, tileName)
}

// setTileChecked is the whole of both mark and unmark: the API takes the same
// update either way, so the only difference is the boolean and the points.
// Keeping one path means unmark cannot drift from mark.
func setTileChecked(cmd *cobra.Command, checked bool) error {
	boardName, _ := cmd.Flags().GetString("board")
	teamName, _ := cmd.Flags().GetString("team")
	tileName, _ := cmd.Flags().GetString("tile")

	if boardName == "" || teamName == "" {
		return fmt.Errorf("--board and --team are required")
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

	col, row, points, err := resolveTile(cmd, data)
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

	awarded := awardedPoints(points, checked)

	info := map[string]interface{}{
		"teamId":     teamIndex,
		"checked":    checked,
		"currPoints": awarded,
	}
	if err := client.UpdateBoard(boardName, board.GeneralPassword, "general", col, row, info); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	action, verb := "tile_marked", "Marked"
	if !checked {
		action, verb = "tile_unmarked", "Cleared"
	}
	label := tileName
	if label == "" {
		label = fmt.Sprintf("%d,%d", col, row)
	}

	if jsonOutput {
		out, _ := json.Marshal(map[string]interface{}{
			"action": action, "tile": label, "team": teamName,
			"col": col, "row": row, "points": awarded,
		})
		fmt.Println(string(out))
	} else if checked {
		fmt.Printf("%s '%s' complete for %s (%d pts)\n", verb, label, teamName, awarded)
	} else {
		fmt.Printf("%s '%s' for %s\n", verb, label, teamName)
	}
	return nil
}

var tileMarkCmd = &cobra.Command{
	Use:   "mark",
	Short: "Mark a tile as complete for a team",
	Long: `Mark a tile as complete for a specific team.

The tile is looked up by title (exact, case-insensitive), or by position with
--col and --row. Prefer --col/--row from scripts: titles can repeat on a board,
and a repeated title is rejected rather than guessed at.

Uses the GENERAL password (not admin) to update completion state.

Reversible with 'bingo tile unmark'.`,
	Example: `  bingo tile mark --board mesoscape-pvm --tile "Twisted Bow" --team Raiders
  bingo tile mark --board mesoscape-pvm --col 2 --row 3 --team Raiders`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setTileChecked(cmd, true)
	},
}

var tileUnmarkCmd = &cobra.Command{
	Use:   "unmark",
	Short: "Clear a tile's completion for a team",
	Long: `Clear a tile that was marked complete, and zero the points it awarded.

The inverse of 'bingo tile mark', and the reason an agent or a bot can safely
propose marks at all: a mistake costs one command instead of a manual fix to
the board mid-event.

Addressing works exactly as it does for mark: --tile, or --col with --row.
Uses the GENERAL password.`,
	Example: `  bingo tile unmark --board mesoscape-pvm --tile "Twisted Bow" --team Raiders
  bingo tile unmark --board mesoscape-pvm --col 2 --row 3 --team Raiders`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setTileChecked(cmd, false)
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
	tileAddCmd.Flags().String("image", "", "Image URL, stored verbatim (must outlive the event)")
	tileAddCmd.Flags().String("image-file", "", "Local image file, re-hosted permanently by the board host (mutually exclusive with --image)")

	for _, c := range []*cobra.Command{tileMarkCmd, tileUnmarkCmd} {
		c.Flags().String("board", "", "Board name (required)")
		c.Flags().String("tile", "", "Tile title (required unless --col/--row)")
		c.Flags().String("team", "", "Team name (required)")
		c.Flags().Int("col", 0, "Tile column, unambiguous alternative to --tile (needs --row)")
		c.Flags().Int("row", 0, "Tile row, unambiguous alternative to --tile (needs --col)")
	}

	tileListCmd.Flags().String("board", "", "Board name (required)")

	tileCmd.AddCommand(tileAddCmd)
	tileCmd.AddCommand(tileMarkCmd)
	tileCmd.AddCommand(tileUnmarkCmd)
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

// findTileByName resolves a tile title to its position and points.
//
// The match is exact (case-insensitive), never a prefix or substring: marking
// is destructive, so a near-miss must fail rather than silently land on a
// neighbouring tile.
//
// Duplicate titles are an error, not a first-match win. A board may
// legitimately carry the same title twice; silently taking the first makes the
// second unmarkable and tells nobody why. Callers that hit this can address
// the tile unambiguously with --col/--row.
func findTileByName(data map[string]interface{}, name string) (int, int, int, error) {
	nameLower := strings.ToLower(name)
	boardData, ok := data["boardData"].([]interface{})
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid board data")
	}

	type hit struct{ col, row, points int }
	var hits []hit

	for col, colData := range boardData {
		if rows, ok := colData.([]interface{}); ok {
			for row, rowData := range rows {
				if tile, ok := rowData.(map[string]interface{}); ok {
					title, _ := tile["title"].(string)
					if strings.ToLower(title) == nameLower {
						pts, _ := tile["points"].(float64)
						hits = append(hits, hit{col, row, int(pts)})
					}
				}
			}
		}
	}

	switch len(hits) {
	case 0:
		return 0, 0, 0, fmt.Errorf("tile '%s' not found on the board", name)
	case 1:
		return hits[0].col, hits[0].row, hits[0].points, nil
	default:
		positions := make([]string, 0, len(hits))
		for _, h := range hits {
			positions = append(positions, fmt.Sprintf("%d,%d", h.col, h.row))
		}
		return 0, 0, 0, fmt.Errorf(
			"tile '%s' appears %d times (at col,row %s) — address it with --col and --row instead of --tile",
			name, len(hits), strings.Join(positions, " and "))
	}
}

// awardedPoints is what a tile contributes after this update.
//
// Clearing a tile must zero its points: leaving them behind keeps the team's
// total as though the tile still counted, so an unmark would undo the tick but
// not the score — the worst kind of half-undo, because the board looks correct.
func awardedPoints(points int, checked bool) int {
	if !checked {
		return 0
	}
	return points
}

// findTileAt resolves a tile by position, the board's own unambiguous key.
// Preferred by automated callers, for whom a title is a guess and a position
// is a fact.
func findTileAt(data map[string]interface{}, col, row int) (int, error) {
	boardData, ok := data["boardData"].([]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid board data")
	}
	if col < 0 || col >= len(boardData) {
		return 0, fmt.Errorf("column %d is off the board (0-%d)", col, len(boardData)-1)
	}
	rows, ok := boardData[col].([]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid column data at %d", col)
	}
	if row < 0 || row >= len(rows) {
		return 0, fmt.Errorf("row %d is off the board (0-%d)", row, len(rows)-1)
	}
	tile, ok := rows[row].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid tile at %d,%d", col, row)
	}
	pts, _ := tile["points"].(float64)
	return int(pts), nil
}
