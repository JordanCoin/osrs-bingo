package cmd

import "testing"

// rowMajor builds boardData the way the board host stores it and the site
// draws it: boardData[row][col], row 0 at the top, col 0 at the left.
func rowMajor(rows [][]string) map[string]interface{} {
	grid := make([]interface{}, 0, len(rows))
	for _, r := range rows {
		cells := make([]interface{}, 0, len(r))
		for _, title := range r {
			cells = append(cells, map[string]interface{}{"title": title, "points": float64(1)})
		}
		grid = append(grid, cells)
	}
	return map[string]interface{}{"boardData": grid}
}

// A cell name reads like a chess square on the drawn board: the letter is
// the column, the number is the row. The site renders boardData[i] as display
// row i, so A1 is boardData[0][0], B1 is boardData[0][1] and A2 is
// boardData[1][0]. The CLI used to call boardData[i] column i, which put
// --at B1 on the second row and filled "reading order" down the left edge.
func TestCellNamesMatchTheBoardAsDrawn(t *testing.T) {
	b := rowMajor([][]string{
		{"A1", "B1", "C1", "D1", "E1"},
		{"A2", "B2", "C2", "D2", "E2"},
		{"A3", "B3", "C3", "D3", "E3"},
		{"A4", "B4", "C4", "D4", "E4"},
		{"A5", "B5", "C5", "D5", "E5"},
	})
	for _, tc := range []struct {
		name     string
		col, row int
	}{
		{"A1", 0, 0}, {"B1", 1, 0}, {"A2", 0, 1}, {"E5", 4, 4}, {"C4", 2, 3},
	} {
		if got := cellName(tc.col, tc.row); got != tc.name {
			t.Errorf("cellName(%d,%d) = %s, want %s", tc.col, tc.row, got, tc.name)
		}
		col, row, err := parseAt(tc.name, b)
		if err != nil || col != tc.col || row != tc.row {
			t.Errorf("parseAt(%s) = %d,%d,%v, want %d,%d", tc.name, col, row, err, tc.col, tc.row)
		}
		tile, err := findCellAt(b, tc.col, tc.row)
		if err != nil || tile["title"] != tc.name {
			t.Errorf("findCellAt(%d,%d) found %v, want the tile titled %s", tc.col, tc.row, tile["title"], tc.name)
		}
		c, r, _, err := findCellByName(b, tc.name)
		if err != nil || c != tc.col || r != tc.row {
			t.Errorf("findCellByName(%s) = %d,%d,%v, want %d,%d", tc.name, c, r, err, tc.col, tc.row)
		}
	}
	if cols, rows := boardDims(rowMajor([][]string{{"", "", ""}, {"", "", ""}})); cols != 3 || rows != 2 {
		t.Errorf("a 2-row, 3-column board reads as %d cols x %d rows", cols, rows)
	}
}

func TestReadingOrderFillsAcrossTheTopRowFirst(t *testing.T) {
	b := rowMajor([][]string{
		{"x", "x", "x", "x", "x"},
		{"x", "x", "", "", ""},
		{"", "", "", "", ""},
		{"", "", "", "", ""},
		{"", "", "", "", ""},
	})
	col, row, err := findEmptySlot(b)
	if err != nil || cellName(col, row) != "C2" {
		t.Errorf("next empty cell = %s (%v), want C2", cellName(col, row), err)
	}
}
