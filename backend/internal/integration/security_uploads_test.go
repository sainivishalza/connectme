package integration

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// jpegWithGPS is a small valid JPEG with an EXIF block that carries a GPS-like
// marker right after the SOI, where cameras put it.
func jpegWithGPS(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := 0; x < 64; x++ {
		for y := 0; y < 48; y++ {
			im.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, im, nil); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude=52.5200N GPSLongitude=013.4050E")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	return append(append(append([]byte{}, b[:2]...), seg...), b[2:]...)
}

// pixelBomb is a PNG that declares a huge canvas in a few bytes.
func pixelBomb(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// IHDR starts at byte 8: length(4) "IHDR"(4) width(4) height(4) ... crc(4)
	binary.BigEndian.PutUint32(b[16:], 30000)
	binary.BigEndian.PutUint32(b[20:], 30000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func (e *env) startIDCheck(member string) {
	e.t.Helper()
	if c, b := e.call("POST", "/idcheck/start", nil, member); c != 200 {
		e.t.Fatalf("start: %d %s", c, b)
	}
}

func TestIDCheckImagesLoseTheirLocationData(t *testing.T) {
	e := setup(t)
	member := e.user("Applicant With GPS")
	mod := e.newUser("moderator", true, "Reviewer")
	e.startIDCheck(member)
	img := jpegWithGPS(t)
	if !bytes.Contains(img, []byte("GPSLatitude")) {
		t.Fatal("test image does not carry the marker")
	}
	if c, b := e.idSubmit(member, img, img); c != 200 {
		t.Fatalf("submit: %d %s", c, b)
	}
	_, q := e.callAs("GET", "/idcheck/queue", nil, mod, "moderator")
	var queue struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"items"`
	}
	decodeInto(t, q, &queue)
	id := ""
	for _, it := range queue.Items {
		if it.DisplayName == "Applicant With GPS" {
			id = it.ID
		}
	}
	if id == "" {
		t.Fatalf("the check is not in the queue: %s", q)
	}
	for _, which := range []string{"id_photo", "selfie"} {
		c, stored := e.callAs("GET", "/idcheck/"+id+"/image/"+which, nil, mod, "moderator")
		if c != 200 {
			t.Fatalf("staff image %s: %d", which, c)
		}
		if bytes.Contains(stored, []byte("GPSLatitude")) || bytes.Contains(stored, []byte("Exif")) {
			t.Errorf("the stored %s still carries its metadata", which)
		}
	}
}

func TestIDCheckRefusesAnImageThatIsAPixelBomb(t *testing.T) {
	e := setup(t)
	member := e.user("Applicant With Bomb")
	e.startIDCheck(member)
	good := jpegBytesFor(t)
	c, b := e.idSubmit(member, pixelBomb(t), good)
	if c != 415 && c != 422 {
		t.Fatalf("a 30000x30000 PNG was accepted or crashed the server: %d %s", c, strings.TrimSpace(b))
	}
	if n := idFilesOnDisk(e); n != 0 {
		t.Errorf("a refused submission left %d files on disk", n)
	}
}
