package cmd

import "testing"

// board builds the nested boardData shape the API returns: a list of columns,
// each a list of row tiles.
func board(titles [][]string) map[string]interface{} {
	cols := make([]interface{}, 0, len(titles))
	for _, col := range titles {
		rows := make([]interface{}, 0, len(col))
		for _, title := range col {
			rows = append(rows, map[string]interface{}{
				"title":  title,
				"points": float64(5),
			})
		}
		cols = append(cols, rows)
	}
	return map[string]interface{}{"boardData": cols}
}

func TestFindTileByNameIsExactNotFuzzy(t *testing.T) {
	// A near-miss must not resolve to a neighbour. Marking is destructive and
	// (before `tile unmark`) was irreversible, so a wrong match is worse than
	// no match.
	b := board([][]string{{"Twisted Bow", "Twisted Bow (CoX)"}})
	if _, _, _, err := findTileByName(b, "Twisted"); err == nil {
		t.Fatal("prefix should not match a tile")
	}
	col, row, pts, err := findTileByName(b, "twisted bow")
	if err != nil || col != 0 || row != 0 || pts != 5 {
		t.Fatalf("case-insensitive exact match failed: %d,%d,%d,%v", col, row, pts, err)
	}
}

func TestDuplicateTitlesAreAnErrorNotASilentFirstMatch(t *testing.T) {
	// Two tiles can legitimately share a title on a big board. Silently taking
	// the first means the second can never be marked, and nobody is told why.
	b := board([][]string{{"Twisted Bow"}, {"Twisted Bow"}})
	_, _, _, err := findTileByName(b, "Twisted Bow")
	if err == nil {
		t.Fatal("duplicate titles must be an error, not a silent first match")
	}
	for _, want := range []string{"0,0", "1,0", "--col"} {
		if !contains(err.Error(), want) {
			t.Fatalf("error should name both positions and the way out, got: %s", err)
		}
	}
}

func TestFindTileAtAddressesByPosition(t *testing.T) {
	b := board([][]string{{"A", "B"}, {"C", "D"}})
	pts, err := findTileAt(b, 1, 1)
	if err != nil || pts != 5 {
		t.Fatalf("expected the tile at 1,1: %d %v", pts, err)
	}
	if _, err := findTileAt(b, 9, 9); err == nil {
		t.Fatal("out-of-range position must error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestUnknownSubcommandIsAnError(t *testing.T) {
	// Regression: a parent command with subcommands used to accept any
	// argument, print its own help and exit 0. `bingo tile unmark` therefore
	// looked like it had worked back when unmark did not exist — which is how
	// a capability probe concluded seven non-existent commands were present.
	if err := tileCmd.RunE(tileCmd, []string{"unmarkk"}); err == nil {
		t.Fatal("an unknown subcommand must return an error")
	}
	if err := boardCmd.RunE(boardCmd, []string{"nope"}); err == nil {
		t.Fatal("board: an unknown subcommand must return an error")
	}
	if err := teamsCmd.RunE(teamsCmd, []string{"nope"}); err == nil {
		t.Fatal("teams: an unknown subcommand must return an error")
	}
}

func TestResolveTileRequiresColAndRowTogether(t *testing.T) {
	// Half a coordinate silently defaulting the other half to 0 would address
	// a real but wrong tile — and marking is the destructive direction.
	cmd := *tileMarkCmd
	c := &cmd
	c.Flags().Set("col", "2")
	if _, _, _, err := resolveTile(c, board([][]string{{"A"}})); err == nil {
		t.Fatal("--col without --row must error")
	}
}

func TestUnmarkZeroesTheScore(t *testing.T) {
	// An unmark that clears the tick but leaves the points is the worst kind
	// of half-undo: the board looks corrected while the standings stay wrong.
	if got := awardedPoints(5, false); got != 0 {
		t.Fatalf("clearing a tile must zero its points, got %d", got)
	}
	if got := awardedPoints(5, true); got != 5 {
		t.Fatalf("marking must award the tile's points, got %d", got)
	}
}
