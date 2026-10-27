package cover

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	return img
}

func encoded(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&b, img)
	case "jpeg":
		err = jpeg.Encode(&b, img, nil)
	case "gif":
		err = gif.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader is a PNG whose IHDR declares w by h and that has no pixel data.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
	// A tiny IDAT stands in for a highly compressed body.
	idat := append([]byte("IDAT"), 0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01)
	out = binary.BigEndian.AppendUint32(out, uint32(len(idat)-4))
	out = append(out, idat...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(idat))
}

func gifHeader(w, h uint16) []byte {
	out := []byte("GIF89a")
	out = binary.LittleEndian.AppendUint16(out, w)
	out = binary.LittleEndian.AppendUint16(out, h)
	return append(out, 0, 0, 0, ';')
}

func jpegHeader(w, h uint16) []byte {
	out := []byte{0xff, 0xd8, 0xff, 0xc0, 0, 11, 8}
	out = binary.BigEndian.AppendUint16(out, h)
	out = binary.BigEndian.AppendUint16(out, w)
	// One component, then a start of scan with no entropy-coded data.
	return append(out, 1, 1, 0x11, 0, 0xff, 0xda, 0, 8, 1, 1, 0, 0, 63, 0, 0xff, 0xd9)
}

func TestDecodeMakesABoundedThumbnail(t *testing.T) {
	for _, tc := range []struct {
		media, format string
		w, h          int
		thumb         image.Rectangle
	}{
		{"image/png", "png", 600, 900, image.Rect(0, 0, 266, 400)},
		{"image/jpeg", "jpeg", 1000, 500, image.Rect(0, 0, 280, 140)},
		{"image/gif", "gif", 40, 60, image.Rect(0, 0, 40, 60)},
	} {
		got, err := Decode(encoded(t, tc.format, picture(tc.w, tc.h)), tc.media)
		if err != nil || got.Width != tc.w || got.Height != tc.h || got.Bounds != tc.thumb || got.Format != Format(tc.media) {
			t.Fatalf("%s: %+v %v", tc.media, got.Bounds, err)
		}
		back, err := png.Decode(bytes.NewReader(got.PNG))
		if err != nil || back.Bounds() != tc.thumb {
			t.Fatal(tc.media, err)
		}
	}
}

// Decompression bombs are refused from their headers: a full decode of any
// of these would need gigabytes.
func TestDecodeRefusesBeforeFullDecoding(t *testing.T) {
	for name, tc := range map[string]struct {
		data  []byte
		media string
		want  error
	}{
		"png 100000 square": {pngHeader(100000, 100000), "image/png", ErrDimensions},
		"png too tall":      {pngHeader(10, MaxSide+1), "image/png", ErrDimensions},
		"png area":          {pngHeader(3000, 3000), "image/png", ErrDimensions},
		"gif 65535 square":  {gifHeader(65535, 65535), "image/gif", ErrDimensions},
		"jpeg 65500 square": {jpegHeader(65500, 65500), "image/jpeg", ErrDimensions},
		"declared jpeg":     {pngHeader(10, 10), "image/jpeg", ErrMismatch},
		"svg":               {[]byte("<svg/>"), "image/svg+xml", ErrUnsupported},
		"webp":              {[]byte("RIFF....WEBP"), "image/webp", ErrUnsupported},
		"empty":             {nil, "image/png", ErrSize},
		"oversized":         {append(pngHeader(1, 1), make([]byte, MaxBytes)...), "image/png", ErrSize},
	} {
		if _, err := Decode(tc.data, tc.media); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A valid header over a malformed body fails without a thumbnail.
	for _, data := range [][]byte{pngHeader(10, 10), jpegHeader(10, 10), gifHeader(10, 10)} {
		media := map[byte]string{0x89: "image/png", 0xff: "image/jpeg", 'G': "image/gif"}[data[0]]
		if got, err := Decode(data, media); err == nil || got.PNG != nil {
			t.Fatal(media, err)
		}
	}
	data := encoded(t, "png", picture(20, 20))
	if _, err := Decode(data[:len(data)/2], "image/png"); err == nil {
		t.Fatal("truncated image decoded")
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(pngHeader(10, 10), "image/png")
	f.Add(gifHeader(10, 10), "image/gif")
	f.Add(jpegHeader(10, 10), "image/jpeg")
	var b bytes.Buffer
	if err := png.Encode(&b, picture(8, 8)); err != nil {
		f.Fatal(err)
	}
	f.Add(b.Bytes(), "image/png")
	f.Fuzz(func(t *testing.T, data []byte, media string) {
		got, err := Decode(data, media)
		if err != nil {
			if got.PNG != nil {
				t.Fatal("refused cover kept a thumbnail")
			}
			return
		}
		if got.Width > MaxSide || got.Height > MaxSide || got.Bounds.Dx() > ThumbWidth || got.Bounds.Dy() > ThumbHeight || len(got.PNG) == 0 {
			t.Fatalf("%+v", got.Bounds)
		}
	})
}
