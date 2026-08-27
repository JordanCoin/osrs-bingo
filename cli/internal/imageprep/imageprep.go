// Package imageprep turns a local image file into the data URI the board host
// accepts in a tile's image field.
//
// Why a data URI at all: praynr.com stores an https:// image URL verbatim, so a
// Discord CDN link (signed, expiring in about two weeks) would render a broken
// tile mid-event. Handed a data: URI instead, the host re-hosts the bytes and
// returns a permanent https://praynr.com/static/uploads/board-images/<hash>.webp
// URL. The board host becomes the stable store, and no new hosting is needed.
//
// Why a file and not a flag value: a data URI for a screenshot-sized PNG is
// hundreds of kilobytes, and a single argv entry is capped at 128KB on Linux
// (MAX_ARG_STRLEN) and 1MB total on macOS (ARG_MAX). The bytes have to travel
// by path, so the CLI is what builds the URI.
package imageprep

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// MaxFileBytes is the largest file the CLI will read. Discord itself caps free
// uploads well below this; anything larger is a mistake or an attack, and
// base64 inflates whatever we accept by a third before it goes over the wire.
const MaxFileBytes = 8 << 20 // 8MB

// MaxDimension is the longest side of the image sent to the board host. Tiles
// render at a couple of hundred pixels, so anything larger is bytes nobody
// sees, paid for on every board load.
const MaxDimension = 512

// DataURIFromFile reads an image file and returns a PNG data URI, downscaled so
// that neither side exceeds MaxDimension.
//
// It never trusts the file extension. The type comes from the magic bytes, so a
// renamed text file is refused here rather than by the board host over the
// network, or worse, accepted as a broken tile.
func DataURIFromFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not an image file", path)
	}
	if info.Size() > MaxFileBytes {
		return "", fmt.Errorf("%s is %.1fMB; the largest image accepted is %dMB",
			path, float64(info.Size())/(1<<20), MaxFileBytes>>20)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	// Stat can lie about a file that grew between the check and the read, and
	// on anything that is not a regular file. The byte count is the real cap.
	if len(raw) > MaxFileBytes {
		return "", fmt.Errorf("%s is %.1fMB; the largest image accepted is %dMB",
			path, float64(len(raw))/(1<<20), MaxFileBytes>>20)
	}

	format := SniffFormat(raw)
	if format == "" {
		return "", fmt.Errorf("%s is not a PNG, JPEG, GIF or WebP image", path)
	}

	src, err := decode(raw, format)
	if err != nil {
		return "", fmt.Errorf("%s looks like %s but does not decode: %w", path, format, err)
	}

	out, err := encodePNG(Downscale(src, MaxDimension))
	if err != nil {
		return "", fmt.Errorf("cannot re-encode %s as PNG: %w", path, err)
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out), nil
}

// SniffFormat identifies an image by its leading bytes, returning "png",
// "jpeg", "gif", "webp", or "" for anything else.
func SniffFormat(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	case len(b) >= 3 && bytes.Equal(b[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return "gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp"
	default:
		return ""
	}
}

func decode(raw []byte, format string) (image.Image, error) {
	r := bytes.NewReader(raw)
	switch format {
	case "png":
		return png.Decode(r)
	case "jpeg":
		return jpeg.Decode(r)
	case "gif":
		// An animated GIF decodes to its first frame, which is what a static
		// tile wants anyway.
		return gif.Decode(r)
	case "webp":
		return webp.Decode(r)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

// Downscale returns src fitted inside a max-by-max box, preserving the aspect
// ratio. An image already inside the box is returned untouched: upscaling would
// only invent detail and grow the payload.
func Downscale(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}

	nw, nh := w, h
	if w >= h {
		nw = max
		nh = h * max / w
	} else {
		nh = max
		nw = w * max / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
