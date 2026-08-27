package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JordanCoin/osrs-bingo/cli/internal/imageprep"
)

// boardHost stands in for praynr.com: it serves one board with an empty slot
// and records the tile update it is sent, so a test can assert on the exact
// bytes the real host would have received.
type boardHost struct {
	server   *httptest.Server
	requests atomic.Int32
	lastInfo map[string]interface{}
}

func newBoardHost(t *testing.T) *boardHost {
	t.Helper()
	h := &boardHost{}
	mux := http.NewServeMux()

	mux.HandleFunc("/getBoard/", func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"boardData":[[{"title":"","points":0}]]}`))
	})

	mux.HandleFunc("/updateBoard/", func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Info map[string]interface{} `json:"info"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("board host got un-parseable update: %v", err)
		}
		h.lastInfo = payload.Info
		w.WriteHeader(http.StatusOK)
	})

	h.server = httptest.NewServer(mux)
	t.Cleanup(h.server.Close)
	return h
}

// imageURLSent is the string the host was asked to store for the tile.
func (h *boardHost) imageURLSent(t *testing.T) string {
	t.Helper()
	if h.lastInfo == nil {
		t.Fatal("the board host was never sent a tile update")
	}
	img, ok := h.lastInfo["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("tile update carried no image object: %#v", h.lastInfo["image"])
	}
	url, _ := img["url"].(string)
	return url
}

// withBoard points the command at a fake host and a throwaway credentials file,
// so `tile add` runs end to end without touching praynr.com or the real
// ~/.bingo/boards.json.
func withBoard(t *testing.T, h *boardHost) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".bingo"), 0o755); err != nil {
		t.Fatal(err)
	}
	creds := `{"testboard":{"admin_password":"a","general_password":"g","teams":["A","B"],"size":[1,1]}}`
	if err := os.WriteFile(filepath.Join(home, ".bingo", "boards.json"), []byte(creds), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("BINGO_API_URL", h.server.URL)
}

// runTileAdd drives the real cobra command, not a copy of its logic.
func runTileAdd(t *testing.T, args ...string) error {
	t.Helper()
	full := append([]string{"tile", "add"}, args...)
	rootCmd.SetArgs(full)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	t.Cleanup(func() {
		// Cobra remembers flag values between Execute calls, so a leftover
		// --image-file would leak into the next test as a phantom pass.
		_ = tileAddCmd.Flags().Set("image", "")
		_ = tileAddCmd.Flags().Set("image-file", "")
	})
	return rootCmd.Execute()
}

func writePNG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 0x40, 0xFF})
		}
	}
	path := filepath.Join(dir, "tile.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// decodeSentDataURI pulls the image back out of what the host received, which
// is the only assertion that proves the wire format is what we think it is.
func decodeSentDataURI(t *testing.T, uri string) image.Image {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("expected a PNG data URI, got %.60q", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatalf("data URI payload is not base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("data URI payload is not a PNG: %v", err)
	}
	return img
}

// A big screenshot is the normal case: someone drops a 2000x1500 image in
// Discord. It must reach the host shrunk, because the tile renders at a couple
// of hundred pixels and base64 inflates every byte we do not drop.
func TestImageFileArrivesAsADownscaledDataURI(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)
	path := writePNG(t, t.TempDir(), 2000, 1500)

	if err := runTileAdd(t, "--board", "testboard", "--title", "Art", "--image-file", path); err != nil {
		t.Fatalf("tile add failed: %v", err)
	}

	img := decodeSentDataURI(t, h.imageURLSent(t))
	b := img.Bounds()
	if b.Dx() != 512 {
		t.Errorf("long side should be 512px, got %dx%d", b.Dx(), b.Dy())
	}
	if b.Dy() != 384 {
		t.Errorf("aspect ratio not preserved: got %dx%d, want 512x384", b.Dx(), b.Dy())
	}
}

// A JPEG is the other thing a phone actually uploads.
func TestJPEGIsAccepted(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 900, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 900; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), 0x20, uint8(y % 256), 0xFF})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := runTileAdd(t, "--board", "testboard", "--title", "Photo", "--image-file", path); err != nil {
		t.Fatalf("tile add refused a JPEG: %v", err)
	}
	got := decodeSentDataURI(t, h.imageURLSent(t)).Bounds()
	if got.Dx() != 512 {
		t.Errorf("JPEG should be downscaled to 512 on the long side, got %dx%d", got.Dx(), got.Dy())
	}
}

// WebP matters because it is what Discord itself re-serves images as, so an
// attachment downloaded from the CDN is often a WebP even when the person
// uploaded a PNG. Go's standard library cannot encode one, hence the fixture.
func TestWebPIsAccepted(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)

	if err := runTileAdd(t, "--board", "testboard", "--title", "WebP", "--image-file", "testdata/sample.webp"); err != nil {
		t.Fatalf("tile add refused a WebP: %v", err)
	}
	got := decodeSentDataURI(t, h.imageURLSent(t)).Bounds()
	if got.Dx() != 512 {
		t.Errorf("WebP should be downscaled to 512 on the long side, got %dx%d", got.Dx(), got.Dy())
	}
}

// A non-image must die locally. The board host rejects it with HTTP 400 anyway,
// but only after we have already fetched the board and burned a round trip on a
// mistake the file itself announces in its first eight bytes.
func TestNonImageIsRefusedBeforeAnyHTTPCall(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)

	path := filepath.Join(t.TempDir(), "notes.png") // extension lies on purpose
	if err := os.WriteFile(path, []byte("this is plain text, not an image at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runTileAdd(t, "--board", "testboard", "--title", "Nope", "--image-file", path)
	if err == nil {
		t.Fatal("a text file named .png was accepted as tile art")
	}
	if !strings.Contains(err.Error(), "not a PNG, JPEG, GIF or WebP image") {
		t.Errorf("unhelpful message for a non-image: %v", err)
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("board host saw %d requests; a bad file should cost zero", n)
	}
}

// Two sources for one image is a caller bug with two plausible answers, so it
// gets neither.
func TestImageAndImageFileTogetherAreRefused(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)
	path := writePNG(t, t.TempDir(), 64, 64)

	err := runTileAdd(t, "--board", "testboard", "--title", "Both",
		"--image", "https://example.com/x.png", "--image-file", path)
	if err == nil {
		t.Fatal("--image and --image-file were accepted together")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Errorf("unhelpful message for the conflict: %v", err)
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("board host saw %d requests; a flag conflict should cost zero", n)
	}
}

// Base64 inflates by a third, so an 8MB cap is really an 11MB request. Past
// that the caller has handed us the wrong file.
func TestOversizedFileIsRefused(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)

	path := filepath.Join(t.TempDir(), "huge.png")
	// Real PNG magic followed by nine megabytes of nothing: the size check has
	// to fire before the decode does, or the message blames the wrong thing.
	blob := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 9<<20)...)
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	err := runTileAdd(t, "--board", "testboard", "--title", "Huge", "--image-file", path)
	if err == nil {
		t.Fatal("a 9MB file was accepted")
	}
	if !strings.Contains(err.Error(), "largest image accepted is 8MB") {
		t.Errorf("unhelpful message for an oversized file: %v", err)
	}
	if n := h.requests.Load(); n != 0 {
		t.Errorf("board host saw %d requests; an oversized file should cost zero", n)
	}
}

// A plain --image URL must still travel verbatim. Wiki art is already permanent
// and re-encoding it would be pointless churn.
func TestPlainImageURLIsStillSentVerbatim(t *testing.T) {
	h := newBoardHost(t)
	withBoard(t, h)

	const url = "https://oldschool.runescape.wiki/images/Fire_cape_detail.png"
	if err := runTileAdd(t, "--board", "testboard", "--title", "Cape", "--image", url); err != nil {
		t.Fatalf("tile add failed: %v", err)
	}
	if got := h.imageURLSent(t); got != url {
		t.Errorf("URL was rewritten: got %q, want %q", got, url)
	}
}

// Sniffing is the whole defence against a renamed file, so it is worth a direct
// test as well as the end-to-end one.
func TestSniffFormatReadsMagicBytesNotExtensions(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0}, "png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "jpeg"},
		{"gif87", []byte("GIF87a....."), "gif"},
		{"gif89", []byte("GIF89a....."), "gif"},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp"},
		{"text", []byte("hello there, not an image"), ""},
		{"riff but not webp", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), ""},
		{"empty", nil, ""},
		{"truncated png magic", []byte{0x89, 'P', 'N'}, ""},
	}
	for _, tc := range cases {
		if got := imageprep.SniffFormat(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// An image already inside the box must not be blown up: upscaling invents
// detail and costs bytes on every board load.
func TestDownscaleLeavesSmallImagesAlone(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	out := imageprep.Downscale(src, 512)
	if b := out.Bounds(); b.Dx() != 200 || b.Dy() != 100 {
		t.Errorf("a 200x100 image was resized to %dx%d", b.Dx(), b.Dy())
	}
}

// A tall image fits by its height, not its width.
func TestDownscaleFitsTheLongerSide(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 600, 1800))
	b := imageprep.Downscale(src, 512).Bounds()
	if b.Dy() != 512 || b.Dx() != 170 {
		t.Errorf("a 600x1800 image became %dx%d, want 170x512", b.Dx(), b.Dy())
	}
}
