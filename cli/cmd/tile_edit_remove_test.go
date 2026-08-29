package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// editHost stands in for praynr.com. It serves a board of our choosing and
// records every update it is sent, top-level row/col included, because the
// row/col swap is the part of this CLI most likely to be broken silently.
type editHost struct {
	server  *httptest.Server
	boardJS []byte
	updates []recordedUpdate
}

type recordedUpdate struct {
	path string
	Row  int                    `json:"row"`
	Col  int                    `json:"col"`
	Info map[string]interface{} `json:"info"`
}

func newEditHost(t *testing.T, boardJS []byte) *editHost {
	t.Helper()
	h := &editHost{boardJS: boardJS}
	mux := http.NewServeMux()

	mux.HandleFunc("/getBoard/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(h.boardJS)
	})

	mux.HandleFunc("/updateBoard/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var u recordedUpdate
		if err := json.Unmarshal(body, &u); err != nil {
			t.Errorf("board host got an un-parseable update: %v", err)
		}
		u.path = r.URL.Path
		h.updates = append(h.updates, u)
		w.WriteHeader(http.StatusOK)
	})

	h.server = httptest.NewServer(mux)
	t.Cleanup(h.server.Close)
	return h
}

func (h *editHost) only(t *testing.T) recordedUpdate {
	t.Helper()
	if len(h.updates) != 1 {
		t.Fatalf("expected exactly one tile update, got %d", len(h.updates))
	}
	return h.updates[0]
}

// fixtureCell is the six-key shape a real board carries for every cell,
// confirmed against a live praynr.com board.
func fixtureCell(title string, points int, description string, image interface{}) map[string]interface{} {
	return map[string]interface{}{
		"title":       title,
		"description": description,
		"points":      points,
		"image":       image,
		"rowBingo":    0,
		"colBingo":    0,
	}
}

func fixtureTeam(name string, checked map[[2]int]bool, cols, rows int) map[string]interface{} {
	grid := make([]interface{}, 0, cols)
	for c := 0; c < cols; c++ {
		col := make([]interface{}, 0, rows)
		for r := 0; r < rows; r++ {
			col = append(col, map[string]interface{}{
				"checked":    checked[[2]int{c, r}],
				"proof":      "",
				"currPoints": 0,
			})
		}
		grid = append(grid, col)
	}
	return map[string]interface{}{
		"data": map[string]interface{}{"name": name, "teamData": grid},
	}
}

// twoByTwo is the working board for these tests: one fully dressed tile, one
// bare-bones tile, and two cells nobody has ever touched.
func twoByTwo(t *testing.T, checked map[[2]int]bool) []byte {
	t.Helper()
	body := map[string]interface{}{
		"boardData": []interface{}{
			[]interface{}{
				fixtureCell("Twisted Bow", 5, "Drop from CoX",
					map[string]interface{}{"url": "https://wiki/tbow.png", "opacity": 100}),
				fixtureCell("", 0, "", nil),
			},
			[]interface{}{
				fixtureCell("Fire Cape", 3, "", nil),
				fixtureCell("", 0, "", nil),
			},
		},
		"teamData": []interface{}{
			fixtureTeam("Raiders", checked, 2, 2),
			fixtureTeam("Slayers", nil, 2, 2),
		},
	}
	out, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// useHost points the commands at the fake board host and a throwaway
// credentials file, so a command runs end to end without touching praynr.com.
func useHost(t *testing.T, h *editHost) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".bingo"), 0o755); err != nil {
		t.Fatal(err)
	}
	creds := `{"testboard":{"admin_password":"a","general_password":"g","teams":["Raiders","Slayers"],"size":[2,2]}}`
	if err := os.WriteFile(filepath.Join(home, ".bingo", "boards.json"), []byte(creds), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("BINGO_API_URL", h.server.URL)
}

// runTile drives the real cobra command rather than a copy of its logic, and
// captures the exit code a refusal would have used.
//
// The cleanup puts every flag back to its default AND clears the "changed"
// bit. Cobra keeps both between Execute calls, and every merge decision in
// `tile edit` is made from Changed(), so a leak would show up as a phantom
// pass in the next test.
func runTile(t *testing.T, args ...string) (error, int) {
	t.Helper()
	code := 0
	prev := exit
	exit = func(c int) { code = c }
	rootCmd.SetArgs(args)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	t.Cleanup(func() {
		exit = prev
		for _, c := range []*cobra.Command{tileEditCmd, tileRemoveCmd} {
			for _, n := range []string{"board", "tile", "col", "row", "title", "points", "description", "image", "image-file", "force"} {
				if f := c.Flags().Lookup(n); f != nil {
					_ = f.Value.Set(f.DefValue)
					f.Changed = false
				}
			}
		}
	})
	err := rootCmd.Execute()
	return err, code
}

// An edit must be an edit. Sending the whole tile back with only the named
// field replaced is the difference between "worth 12 now" and a tile that
// quietly loses its title, art and description because the caller mentioned
// points.
func TestEditMergesOnlyTheFlagsGiven(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--tile", "Twisted Bow", "--points", "12")
	if err != nil {
		t.Fatalf("tile edit failed: %v", err)
	}

	info := h.only(t).Info
	if got := info["points"]; got != float64(12) {
		t.Errorf("points not applied: %v", got)
	}
	if got, _ := info["title"].(string); got != "Twisted Bow" {
		t.Errorf("an unnamed field was overwritten: title is %q", got)
	}
	if got, _ := info["description"].(string); got != "Drop from CoX" {
		t.Errorf("an unnamed field was overwritten: description is %q", got)
	}
	img, ok := info["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("an unnamed field was overwritten: image is %#v", info["image"])
	}
	if got, _ := img["url"].(string); got != "https://wiki/tbow.png" {
		t.Errorf("image url changed to %q", got)
	}
}

// The other half of the merge: a field the caller DID name must actually
// change, including one being set to empty on purpose.
func TestEditAppliesEveryFlagGiven(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--col", "0", "--row", "0",
		"--title", "Twisted Bow (CoX)", "--description", "", "--image", "")
	if err != nil {
		t.Fatalf("tile edit failed: %v", err)
	}

	info := h.only(t).Info
	if got, _ := info["title"].(string); got != "Twisted Bow (CoX)" {
		t.Errorf("title not applied: %q", got)
	}
	if got, _ := info["description"].(string); got != "" {
		t.Errorf("--description \"\" should clear the text, got %q", got)
	}
	if info["image"] != nil {
		t.Errorf("--image \"\" should clear the art to null, got %#v", info["image"])
	}
	if got := info["points"]; got != float64(5) {
		t.Errorf("points were not left alone: %v", got)
	}
}

// A board may legitimately carry the same title twice. Editing the first one by
// guess would leave the second uneditable and say nothing.
func TestEditByAmbiguousTitleIsRefused(t *testing.T) {
	body := map[string]interface{}{
		"boardData": []interface{}{
			[]interface{}{fixtureCell("Pet", 10, "", nil)},
			[]interface{}{fixtureCell("Pet", 10, "", nil)},
		},
	}
	js, _ := json.Marshal(body)
	h := newEditHost(t, js)
	useHost(t, h)

	err, code := runTile(t, "tile", "edit", "--board", "testboard", "--tile", "Pet", "--points", "20")
	if err == nil {
		t.Fatal("a duplicated title was edited instead of refused")
	}
	if !strings.Contains(err.Error(), "appears 2 times") || !strings.Contains(err.Error(), "--col") {
		t.Errorf("the refusal should name the collision and the way out: %v", err)
	}
	if code != 3 {
		t.Errorf("an unresolvable address should exit 3, got %d", code)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused edit still wrote %d updates", len(h.updates))
	}
}

// Editing a cell that holds nothing is a caller mistake with no sensible
// outcome: there is no metadata to merge onto.
func TestEditAnEmptyCellIsRefused(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, code := runTile(t, "tile", "edit", "--board", "testboard", "--col", "1", "--row", "1", "--points", "9")
	if err == nil {
		t.Fatal("editing an empty cell was accepted")
	}
	if !strings.Contains(err.Error(), "1,1") {
		t.Errorf("the refusal should name the cell: %v", err)
	}
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused edit still wrote %d updates", len(h.updates))
	}
}

// An edit naming no field is a no-op that would still cost a write. Say so
// rather than pretending something happened.
func TestEditWithNothingToChangeIsRefused(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--tile", "Twisted Bow")
	if err == nil {
		t.Fatal("an edit with no field flags was accepted")
	}
	if len(h.updates) != 0 {
		t.Errorf("a no-op edit still wrote %d updates", len(h.updates))
	}
}

// Replacing art from a file has to go through the same downscale-and-data-URI
// path as `tile add`, or an edited tile would carry an expiring Discord link.
func TestEditImageFileArrivesAsADataURI(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)
	path := writePNG(t, t.TempDir(), 1200, 900)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--tile", "Fire Cape", "--image-file", path)
	if err != nil {
		t.Fatalf("tile edit --image-file failed: %v", err)
	}

	img, ok := h.only(t).Info["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("tile update carried no image object: %#v", h.only(t).Info["image"])
	}
	url, _ := img["url"].(string)
	if b := decodeSentDataURI(t, url).Bounds(); b.Dx() != 512 {
		t.Errorf("edited art should be downscaled to 512 on the long side, got %dx%d", b.Dx(), b.Dy())
	}
}

// The board host reads boardData[row][col] but stores it [column][row], so the
// client sends our column as the API's "row". Dropping that swap in a new call
// path writes to the transposed cell, which on a non-square board is a silent
// write to a different tile.
func TestEditSendsTheColumnAsTheAPIsRow(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--tile", "Fire Cape", "--points", "7")
	if err != nil {
		t.Fatalf("tile edit failed: %v", err)
	}

	// Fire Cape sits at column 1, row 0.
	u := h.only(t)
	if u.Row != 1 {
		t.Errorf(`the API's "row" must carry our column index 1, got %d`, u.Row)
	}
	if u.Col != 0 {
		t.Errorf(`the API's "col" must carry our row index 0, got %d`, u.Col)
	}
	if !strings.Contains(u.path, "/admin") {
		t.Errorf("tile metadata needs the admin password, went to %s", u.path)
	}
}

// A removed tile has to leave the cell indistinguishable from one that never
// held a tile. The host merges an update onto what it already has, so any field
// left unnamed survives the removal and the cell stays half-alive.
func TestRemoveWritesTheVirginCellShape(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "remove", "--board", "testboard", "--tile", "Twisted Bow")
	if err != nil {
		t.Fatalf("tile remove failed: %v", err)
	}

	info := h.only(t).Info
	want := map[string]interface{}{
		"title":       "",
		"description": "",
		"points":      float64(0),
		"image":       nil,
		"rowBingo":    float64(0),
		"colBingo":    float64(0),
	}
	if len(info) != len(want) {
		t.Fatalf("removal wrote %d fields, want %d: %#v", len(info), len(want), info)
	}
	for k, v := range want {
		got, present := info[k]
		if !present {
			t.Errorf("removal left %q unnamed, so the old value survives", k)
			continue
		}
		if got != v {
			t.Errorf("%q = %#v, want %#v", k, got, v)
		}
	}
	// image must be present and null, not absent and not "": that is the shape
	// a real board gives an untouched cell.
	raw, _ := json.Marshal(info)
	if !strings.Contains(string(raw), `"image":null`) {
		t.Errorf(`removal must write "image":null, sent %s`, raw)
	}
}

// Removing a tile a team has already scored keeps the points in the standings
// while the tile they came from disappears. That is the audit hole, so it needs
// an explicit --force.
func TestRemoveOfAScoredTileIsRefusedWithoutForce(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, map[[2]int]bool{{1, 0}: true}))
	useHost(t, h)

	err, code := runTile(t, "tile", "remove", "--board", "testboard", "--tile", "Fire Cape")
	if err == nil {
		t.Fatal("a scored tile was removed without --force")
	}
	if !strings.Contains(err.Error(), "Raiders") {
		t.Errorf("the refusal should name the team holding it: %v", err)
	}
	if !strings.Contains(err.Error(), "unmark") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal should name both ways forward: %v", err)
	}
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused removal still wrote %d updates", len(h.updates))
	}
}

func TestRemoveOfAScoredTileIsAllowedWithForce(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, map[[2]int]bool{{1, 0}: true}))
	useHost(t, h)

	err, _ := runTile(t, "tile", "remove", "--board", "testboard", "--tile", "Fire Cape", "--force")
	if err != nil {
		t.Fatalf("--force should remove a scored tile: %v", err)
	}
	if got, _ := h.only(t).Info["title"].(string); got != "" {
		t.Errorf("forced removal left the title as %q", got)
	}
}

// An unscored tile needs no ceremony: the guard must not fire on a board where
// some other tile is checked.
func TestRemoveOfAnUnscoredTileNeedsNoForce(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, map[[2]int]bool{{1, 0}: true}))
	useHost(t, h)

	err, _ := runTile(t, "tile", "remove", "--board", "testboard", "--tile", "Twisted Bow")
	if err != nil {
		t.Fatalf("an unscored tile should remove without --force: %v", err)
	}
	if len(h.updates) != 1 {
		t.Errorf("expected one update, got %d", len(h.updates))
	}
}

func TestRemoveOfAnEmptyCellIsRefused(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, code := runTile(t, "tile", "remove", "--board", "testboard", "--col", "0", "--row", "1")
	if err == nil {
		t.Fatal("removing an empty cell was accepted")
	}
	if !strings.Contains(err.Error(), "0,1") {
		t.Errorf("the refusal should name the cell: %v", err)
	}
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused removal still wrote %d updates", len(h.updates))
	}
}

// Addressing is one implementation shared with mark, so half a coordinate has
// to fail the same way here as it does there.
func TestEditAddressingMatchesMark(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "edit", "--board", "testboard", "--col", "1", "--points", "3")
	if err == nil {
		t.Fatal("--col without --row must be refused for edit, as it is for mark")
	}
	if !strings.Contains(err.Error(), "must be given together") {
		t.Errorf("edit and mark should give the same message: %v", err)
	}
	if len(h.updates) != 0 {
		t.Errorf("a refused edit still wrote %d updates", len(h.updates))
	}
}

func TestRemoveAddressingAcceptsPosition(t *testing.T) {
	h := newEditHost(t, twoByTwo(t, nil))
	useHost(t, h)

	err, _ := runTile(t, "tile", "remove", "--board", "testboard", "--col", "1", "--row", "0")
	if err != nil {
		t.Fatalf("positional removal failed: %v", err)
	}
	if u := h.only(t); u.Row != 1 || u.Col != 0 {
		t.Errorf("positional removal addressed the wrong cell: row=%d col=%d", u.Row, u.Col)
	}
}
