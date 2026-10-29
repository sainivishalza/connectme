package uploadcheck

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func sample(w, h int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			im.Set(x, y, color.RGBA{uint8(x), uint8(y), 99, 255})
		}
	}
	return im
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, sample(w, h), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAJpegWithLocationDataComesOutClean(t *testing.T) {
	b := jpegBytes(t, 64, 48)
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude=1.0 secret-trailer")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	dirty := append(append(append([]byte{}, b[:2]...), append(seg, payload...)...), b[2:]...)
	dirty = append(dirty, []byte("TRAILING-HIDDEN-PAYLOAD")...)

	out, err := SanitizeImage(dirty, Photo)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"GPSLatitude", "Exif", "TRAILING-HIDDEN-PAYLOAD"} {
		if bytes.Contains(out, []byte(leak)) {
			t.Errorf("output still contains %q", leak)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("output is not a valid JPEG: %v", err)
	}
}

func TestTypeIsDecidedByTheBytes(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, sample(10, 10))
	if _, err := SanitizeImage(buf.Bytes(), Photo); err != nil {
		t.Errorf("a real PNG was refused: %v", err)
	}
	for name, raw := range map[string][]byte{
		"html named .jpg": []byte("<html><script>alert(1)</script></html>"),
		"gif":             []byte("GIF89a\x01\x00\x01\x00"),
		"plain text":      []byte("hello"),
		"svg":             []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
	} {
		if _, err := SanitizeImage(raw, Photo); !errors.Is(err, ErrNotAnImage) {
			t.Errorf("%s: got %v, want ErrNotAnImage", name, err)
		}
	}
}

func TestSizeLimits(t *testing.T) {
	if _, err := SanitizeImage(nil, Photo); !errors.Is(err, ErrTooLarge) {
		t.Errorf("empty: %v", err)
	}
	if _, err := SanitizeImage(make([]byte, Photo.MaxBytes+1), Photo); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many bytes: %v", err)
	}
	// A tiny file that declares a 30000 x 30000 canvas must be refused before decoding.
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], 30000)
	binary.BigEndian.PutUint32(b[20:], 30000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	if _, err := SanitizeImage(b, Photo); !errors.Is(err, ErrDimensions) {
		t.Errorf("pixel bomb: %v", err)
	}
	if _, err := SanitizeImage(jpegBytes(t, 40, 40)[:100], Photo); !errors.Is(err, ErrUnreadable) {
		t.Errorf("truncated file: %v", err)
	}
}

func TestOutputIsFittedAndIDDocumentsKeepMoreDetail(t *testing.T) {
	big := jpegBytes(t, 3000, 2000)
	for name, want := range map[string]int{"photo": Photo.OutSide, "id": IDDocument.OutSide} {
		l := Photo
		if name == "id" {
			l = IDDocument
		}
		out, err := SanitizeImage(big, l)
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
		if cfg.Width != want {
			t.Errorf("%s: width %d, want %d", name, cfg.Width, want)
		}
	}
}
