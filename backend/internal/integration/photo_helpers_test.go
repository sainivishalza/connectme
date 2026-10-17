package integration

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	"connectme/pkg/middleware"
)

// jpegBytesFor returns a small valid JPEG.
func jpegBytesFor(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y += 4 {
		for x := 0; x < 64; x += 4 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// uploadPhoto posts a multipart photo and returns the HTTP status.
func uploadPhoto(e *env, uid string, data []byte) int {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("photo", "p.jpg")
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", "/users/me/photos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	tok, _ := middleware.SignAccessToken([]byte(secret), uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code
}
