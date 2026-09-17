package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// gridBoard builds a cols x rows board with every cell virgin except the named
// tiles, keyed [column,row], and the Raiders team holding the checked cells.
func gridBoard(t *testing.T, cols, rows int, tiles map[[2]int]map[string]interface{}, checked map[[2]int]bool) []byte {
	t.Helper()
	grid := make([]interface{}, cols)
	for c := 0; c < cols; c++ {
		column := make([]interface{}, rows)
		for r := 0; r < rows; r++ {
			if tile, ok := tiles[[2]int{c, r}]; ok {
				column[r] = tile
			} else {
				column[r] = fixtureCell("", 0, "", nil)
			}
		}
		grid[c] = column
	}
	out, err := json.Marshal(map[string]interface{}{
		"boardData": grid,
		"teamData": []interface{}{
			fixtureTeam("Raiders", checked, cols, rows),
			fixtureTeam("Slayers", nil, cols, rows),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func art(url string) map[string]interface{} {
	return map[string]interface{}{"url": url, "opacity": 100}
}

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = prev
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// A planner lists tiles left to right. Filling down columns put "Logs, Copper
// ore, Tin ore" down the left edge of a 3x3 instead of across the top row.
func TestAddFillsInReadingOrder(t *testing.T) {
	h := newEditHost(t, gridBoard(t, 3, 3, nil, nil))
	useHost(t, h)

	for _, title := range []string{"Logs", "Copper ore", "Tin ore", "Iron ore"} {
		if err, _ := runTile(t, "tile", "add", "--board", "testboard", "--title", title); err != nil {
			t.Fatalf("tile add %s failed: %v", title, err)
		}
	}
	if len(h.updates) != 4 {
		t.Fatalf("expected 4 updates, got %d", len(h.updates))
	}
	// API "row" carries our column, API "col" our row.
	if u := h.updates[2]; u.Row != 2 || u.Col != 0 {
		t.Errorf("third add should land at C1 (column 2, row 0), got column %d row %d", u.Row, u.Col)
	}
	if u := h.updates[3]; u.Row != 0 || u.Col != 1 {
		t.Errorf("fourth add should land at A2 (column 0, row 1), got column %d row %d", u.Row, u.Col)
	}
}

func TestAddTreatsExampleTileAsEmpty(t *testing.T) {
	h := newEditHost(t, gridBoard(t, 2, 2, map[[2]int]map[string]interface{}{
		{0, 0}: fixtureCell("Example Tile", 0, "", nil),
	}, nil))
	useHost(t, h)

	if err, _ := runTile(t, "tile", "add", "--board", "testboard", "--title", "Logs"); err != nil {
		t.Fatal(err)
	}
	if u := h.only(t); u.Row != 0 || u.Col != 0 {
		t.Errorf("Example Tile should be filled first, add went to column %d row %d", u.Row, u.Col)
	}
}

func TestAddAtAPositionPlacesTheTile(t *testing.T) {
	h := newEditHost(t, gridBoard(t, 3, 3, nil, nil))
	useHost(t, h)

	out := captureStdout(t, func() {
		if err, _ := runTile(t, "tile", "add", "--board", "testboard", "--title", "Logs", "--at", "b2", "--json"); err != nil {
			t.Fatal(err)
		}
	})
	if u := h.only(t); u.Row != 1 || u.Col != 1 {
		t.Errorf("--at B2 wrote column %d row %d", u.Row, u.Col)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("add --json printed %q: %v", out, err)
	}
	if got["at"] != "B2" {
		t.Errorf(`add JSON should carry "at":"B2", got %v`, got["at"])
	}
}

func TestAddAtAnOccupiedCellIsRefused(t *testing.T) {
	for _, addr := range [][]string{{"--at", "B1"}, {"--col", "1", "--row", "0"}} {
		t.Run(strings.Join(addr, " "), func(t *testing.T) {
			h := newEditHost(t, gridBoard(t, 3, 3, map[[2]int]map[string]interface{}{
				{1, 0}: fixtureCell("Zenyte shard", 10, "", nil),
			}, nil))
			useHost(t, h)

			args := append([]string{"tile", "add", "--board", "testboard", "--title", "Logs"}, addr...)
			err, code := runTile(t, args...)
			if err == nil {
				t.Fatal("adding onto an occupied cell was accepted")
			}
			if !strings.Contains(err.Error(), "Zenyte shard") || !strings.Contains(err.Error(), "B1") {
				t.Errorf("the refusal should name the cell and what is there: %v", err)
			}
			if code != 3 {
				t.Errorf("expected exit 3, got %d", code)
			}
			if len(h.updates) != 0 {
				t.Errorf("a refused add still wrote %d updates", len(h.updates))
			}
		})
	}
}

func TestMoveIntoAnEmptyCell(t *testing.T) {
	logs := fixtureCell("Logs", 2, "chop", art("https://wiki/logs.png"))
	h := newEditHost(t, gridBoard(t, 3, 3, map[[2]int]map[string]interface{}{{0, 0}: logs}, nil))
	useHost(t, h)

	out := captureStdout(t, func() {
		if err, _ := runTile(t, "tile", "move", "--board", "testboard", "--tile", "Logs", "--to", "C3", "--json"); err != nil {
			t.Fatalf("move failed: %v", err)
		}
	})

	if len(h.updates) != 2 {
		t.Fatalf("expected 2 writes, got %d", len(h.updates))
	}
	var b map[string]interface{}
	_ = json.Unmarshal(h.boardJS, &b)
	grid := b["boardData"].([]interface{})
	dest := grid[2].([]interface{})[2].(map[string]interface{})
	src := grid[0].([]interface{})[0].(map[string]interface{})
	if dest["title"] != "Logs" || dest["points"] != float64(2) || dest["description"] != "chop" {
		t.Errorf("destination did not receive the tile: %#v", dest)
	}
	if img, _ := dest["image"].(map[string]interface{}); img["url"] != "https://wiki/logs.png" {
		t.Errorf("destination lost the art: %#v", dest["image"])
	}
	if src["title"] != "" || src["points"] != float64(0) || src["description"] != "" || src["image"] != nil {
		t.Errorf("source was not cleared: %#v", src)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("move --json printed %q: %v", out, err)
	}
	if got["action"] != "tile_moved" || got["from"] != "A1" || got["to"] != "C3" {
		t.Errorf("unexpected JSON: %s", out)
	}
	if v, present := got["swapped_with"]; !present || v != nil {
		t.Errorf(`"swapped_with" should be present and null: %s`, out)
	}
}

func TestMoveOntoAnOccupiedCellSwaps(t *testing.T) {
	h := newEditHost(t, gridBoard(t, 3, 3, map[[2]int]map[string]interface{}{
		{0, 0}: fixtureCell("Logs", 2, "chop", art("https://wiki/logs.png")),
		{2, 2}: fixtureCell("Zenyte shard", 10, "drop", art("https://wiki/zenyte.png")),
	}, nil))
	useHost(t, h)

	out := captureStdout(t, func() {
		if err, _ := runTile(t, "tile", "move", "--board", "testboard", "--at", "A1", "--to-col", "2", "--to-row", "2", "--json"); err != nil {
			t.Fatalf("swap failed: %v", err)
		}
	})

	var b map[string]interface{}
	_ = json.Unmarshal(h.boardJS, &b)
	grid := b["boardData"].([]interface{})
	check := func(cell map[string]interface{}, title string, points float64, desc, url string) {
		t.Helper()
		img, _ := cell["image"].(map[string]interface{})
		if cell["title"] != title || cell["points"] != points || cell["description"] != desc || img["url"] != url {
			t.Errorf("want %s/%v/%s/%s, got %#v", title, points, desc, url, cell)
		}
	}
	check(grid[2].([]interface{})[2].(map[string]interface{}), "Logs", 2, "chop", "https://wiki/logs.png")
	check(grid[0].([]interface{})[0].(map[string]interface{}), "Zenyte shard", 10, "drop", "https://wiki/zenyte.png")

	var got map[string]interface{}
	_ = json.Unmarshal([]byte(out), &got)
	if got["swapped_with"] != "Zenyte shard" {
		t.Errorf(`"swapped_with" should name the displaced tile: %s`, out)
	}
}

// Completion lives in a per-team grid keyed by cell, so moving a checked cell's
// tile would leave the mark behind on whatever lands there.
func TestMoveIsRefusedWhenATeamCheckedEitherCell(t *testing.T) {
	tiles := map[[2]int]map[string]interface{}{
		{0, 0}: fixtureCell("Logs", 2, "", nil),
		{2, 2}: fixtureCell("Zenyte shard", 10, "", nil),
	}
	for name, checked := range map[string][2]int{"source": {0, 0}, "destination": {2, 2}} {
		t.Run(name, func(t *testing.T) {
			h := newEditHost(t, gridBoard(t, 3, 3, tiles, map[[2]int]bool{checked: true}))
			useHost(t, h)

			err, code := runTile(t, "tile", "move", "--board", "testboard", "--tile", "Logs", "--to", "C3")
			if err == nil {
				t.Fatal("a move touching a checked cell was accepted")
			}
			cell := cellName(checked[0], checked[1])
			if !strings.Contains(err.Error(), "Raiders") || !strings.Contains(err.Error(), cell) {
				t.Errorf("the refusal should name the team and %s: %v", cell, err)
			}
			if code != 3 {
				t.Errorf("expected exit 3, got %d", code)
			}
			if len(h.updates) != 0 {
				t.Errorf("a refused move still wrote %d updates", len(h.updates))
			}
		})
	}
}

// Destination is written first. If the source write then fails, the
// destination holds a duplicate until it is put back.
func TestMoveRestoresTheDestinationWhenTheSecondWriteFails(t *testing.T) {
	h := newEditHost(t, gridBoard(t, 3, 3, map[[2]int]map[string]interface{}{
		{0, 0}: fixtureCell("Logs", 2, "", nil),
		{2, 2}: fixtureCell("Zenyte shard", 10, "drop", art("https://wiki/zenyte.png")),
	}, nil))
	h.failOn = 2
	useHost(t, h)

	err, code := runTile(t, "tile", "move", "--board", "testboard", "--tile", "Logs", "--to", "C3")
	if err == nil {
		t.Fatal("a failed write was reported as success")
	}
	if code != 1 || !strings.Contains(err.Error(), "restored") {
		t.Errorf("expected exit 1 and a restore notice, got %d: %v", code, err)
	}
	if len(h.updates) != 3 {
		t.Fatalf("expected write, failed write, restore; got %d updates", len(h.updates))
	}
	restore := h.updates[2]
	if restore.Row != 2 || restore.Col != 2 || restore.Info["title"] != "Zenyte shard" || restore.Info["points"] != float64(10) {
		t.Errorf("restore did not put C3 back: %+v", restore)
	}
}

func TestParseAt(t *testing.T) {
	var data map[string]interface{}
	_ = json.Unmarshal(gridBoard(t, 3, 3, nil, nil), &data)

	for in, want := range map[string][2]int{"A1": {0, 0}, "c3": {2, 2}, " B2 ": {1, 1}} {
		col, row, err := parseAt(in, data)
		if err != nil || col != want[0] || row != want[1] {
			t.Errorf("parseAt(%q) = %d,%d,%v, want %v", in, col, row, err, want)
		}
	}
	for _, in := range []string{"D1", "A4", "A0", "1A", "", "AA1", "A-1"} {
		if _, _, err := parseAt(in, data); err == nil {
			t.Errorf("parseAt(%q) should be refused", in)
		}
	}
	if _, _, err := parseAt("D1", data); err == nil || !strings.Contains(err.Error(), "C3") {
		t.Errorf("off-board refusal should name the board's extent: %v", err)
	}
}

func TestEditAtOffTheBoardIsRefused(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, code := runTile(t, "tile", "edit", "--board", "testboard", "--at", "C1", "--points", "4")
	if err == nil || code != 3 {
		t.Fatalf("an off-board --at should exit 3, got %d: %v", code, err)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused edit still wrote %d updates", len(h.updates))
	}
}

func TestBoardShowJSONNamesEachCell(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	out := captureStdout(t, func() {
		if err, _ := runTile(t, "board", "show", "--name", "testboard", "--json"); err != nil {
			t.Fatal(err)
		}
	})
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("board show --json printed %q: %v", out, err)
	}
	fire := got["boardData"].([]interface{})[1].([]interface{})[0].(map[string]interface{})
	if fire["title"] != "Fire Cape" || fire["at"] != "B1" {
		t.Errorf(`Fire Cape (column 1, row 0) should carry "at":"B1": %#v`, fire)
	}
}
