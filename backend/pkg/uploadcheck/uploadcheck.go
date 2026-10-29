// Package uploadcheck is the one place an uploaded image is validated and
// cleaned. The bytes decide what a file is (never the extension or the
// client's Content-Type); the declared size is checked before the pixels are
// decoded, so a tiny file that claims a gigantic canvas is refused; and the
// result is always a fresh JPEG, which drops every metadata block (EXIF with
// GPS, XMP, ICC comments) and anything hidden after the image data.
package uploadcheck

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg" // register decoders for image.DecodeConfig
	_ "image/png"
	"net/http"

	"github.com/disintegration/imaging"
)

// Limits are the rules for one kind of image.
type Limits struct {
	MaxBytes        int // largest accepted upload
	MaxSourceSide   int // longest accepted side, as declared in the header
	MaxSourcePixels int // largest accepted canvas, as declared in the header
	OutSide         int // the output is fitted inside OutSide x OutSide
	Quality         int // JPEG quality of the output
}

var (
	// Photo is a profile photo.
	Photo = Limits{MaxBytes: 5 << 20, MaxSourceSide: 12000, MaxSourcePixels: 40_000_000, OutSide: 1080, Quality: 85}
	// IDDocument is an identity document or selfie: larger so small print stays legible.
	IDDocument = Limits{MaxBytes: 5 << 20, MaxSourceSide: 12000, MaxSourcePixels: 40_000_000, OutSide: 2000, Quality: 90}
)

var (
	ErrTooLarge   = errors.New("the file is empty or too large")
	ErrNotAnImage = errors.New("the file is not a JPEG or PNG image")
	ErrUnreadable = errors.New("the image could not be read")
	ErrDimensions = errors.New("the image dimensions are too large")
)

// SanitizeImage validates raw against l and returns a clean JPEG.
func SanitizeImage(raw []byte, l Limits) ([]byte, error) {
	if len(raw) == 0 || len(raw) > l.MaxBytes {
		return nil, ErrTooLarge
	}
	switch http.DetectContentType(raw) {
	case "image/jpeg", "image/png":
	default:
		return nil, ErrNotAnImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnreadable
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > l.MaxSourceSide || cfg.Height > l.MaxSourceSide ||
		cfg.Width*cfg.Height > l.MaxSourcePixels {
		return nil, ErrDimensions
	}
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, ErrUnreadable
	}
	if b := img.Bounds(); b.Dx() > l.OutSide || b.Dy() > l.OutSide {
		img = imaging.Fit(img, l.OutSide, l.OutSide, imaging.Lanczos)
	}
	var out bytes.Buffer
	if err := imaging.Encode(&out, img, imaging.JPEG, imaging.JPEGQuality(l.Quality)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
