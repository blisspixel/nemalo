// Package cover decodes a declared EPUB cover image into a small in-memory
// thumbnail for the terminal (decision 0105). The bytes are untrusted: the
// declared media type and the bytes' signature must agree, and dimensions are
// checked from the header before any pixel is decoded. Only the standard
// library's JPEG, PNG and GIF decoders are used, and GIF decodes one frame.
package cover

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
)

const (
	// MaxBytes is the largest cover member read, as in content.
	MaxBytes = 8 << 20
	// MaxSide and MaxPixels bound the decoded image: 32 MiB as RGBA.
	MaxSide   = 4096
	MaxPixels = 1 << 23
	// ThumbWidth and ThumbHeight bound the thumbnail sent to the terminal.
	ThumbWidth  = 280
	ThumbHeight = 400
)

var (
	ErrUnsupported = errors.New("cover media type is not JPEG, PNG or GIF")
	ErrMismatch    = errors.New("cover bytes do not match the declared media type")
	ErrDimensions  = errors.New("cover dimensions exceed the decoding limit")
	ErrSize        = errors.New("cover member exceeds the size limit")
)

// Thumbnail is a decoded cover reduced to fit ThumbWidth by ThumbHeight and
// encoded as PNG. Width and Height are the source image's header dimensions.
type Thumbnail struct {
	Format        string
	Width, Height int
	PNG           []byte
	Bounds        image.Rectangle
}

type decoder struct {
	signatures [][]byte
	config     func(*bytes.Reader) (image.Config, error)
	decode     func(*bytes.Reader) (image.Image, error)
}

var formats = map[string]struct {
	name string
	decoder
}{
	"image/jpeg": {"JPEG", decoder{[][]byte{{0xff, 0xd8, 0xff}}, func(r *bytes.Reader) (image.Config, error) { return jpeg.DecodeConfig(r) }, func(r *bytes.Reader) (image.Image, error) { return jpeg.Decode(r) }}},
	"image/png":  {"PNG", decoder{[][]byte{[]byte("\x89PNG\r\n\x1a\n")}, func(r *bytes.Reader) (image.Config, error) { return png.DecodeConfig(r) }, func(r *bytes.Reader) (image.Image, error) { return png.Decode(r) }}},
	// gif.Decode returns the first frame and stops reading.
	"image/gif": {"GIF", decoder{[][]byte{[]byte("GIF87a"), []byte("GIF89a")}, func(r *bytes.Reader) (image.Config, error) { return gif.DecodeConfig(r) }, func(r *bytes.Reader) (image.Image, error) { return gif.Decode(r) }}},
}

// Format names a supported declared media type, or "" for any other.
func Format(media string) string {
	return formats[media].name
}

// Decode checks and decodes data declared as media and returns its
// thumbnail. Every refusal happens before full decoding.
func Decode(data []byte, media string) (Thumbnail, error) {
	f, ok := formats[media]
	if !ok {
		return Thumbnail{}, ErrUnsupported
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return Thumbnail{}, ErrSize
	}
	signed := false
	for _, s := range f.signatures {
		signed = signed || bytes.HasPrefix(data, s)
	}
	if !signed {
		return Thumbnail{}, ErrMismatch
	}
	cfg, err := f.config(bytes.NewReader(data))
	if err != nil {
		return Thumbnail{}, fmt.Errorf("cover header: %w", err)
	}
	t := Thumbnail{Format: f.name, Width: cfg.Width, Height: cfg.Height}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return t, ErrDimensions
	}
	img, err := f.decode(bytes.NewReader(data))
	if err != nil {
		return t, fmt.Errorf("cover image: %w", err)
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 || b.Dx() > cfg.Width || b.Dy() > cfg.Height {
		return t, ErrDimensions
	}
	thumb := shrink(img, ThumbWidth, ThumbHeight)
	var out bytes.Buffer
	if err := png.Encode(&out, thumb); err != nil {
		return t, err
	}
	t.PNG, t.Bounds = out.Bytes(), thumb.Bounds()
	return t, nil
}

// shrink box-filters img to fit within w by h, keeping its aspect ratio. A
// smaller image keeps its size.
func shrink(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if dw > w {
		dw, dh = w, max(1, sh*w/sw)
	}
	if dh > h {
		dw, dh = max(1, sw*h/sh), h
	}
	if dw == sw && dh == sh {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0, y1 := y*sh/dh, max(y*sh/dh+1, (y+1)*sh/dh)
		for x := range dw {
			x0, x1 := x*sw/dw, max(x*sw/dw+1, (x+1)*sw/dw)
			var sum [4]int
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride+x0*4 : sy*src.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					sum[0] += int(row[i])
					sum[1] += int(row[i+1])
					sum[2] += int(row[i+2])
					sum[3] += int(row[i+3])
				}
			}
			n := (y1 - y0) * (x1 - x0)
			p := dst.Pix[y*dst.Stride+x*4:]
			for i := range 4 {
				p[i] = uint8(sum[i] / n)
			}
		}
	}
	return dst
}
