package videos

import (
	"encoding/binary"
	"testing"
)

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:], typ)
	return append(b, payload...)
}

func mp4(version byte, timescale uint32, dur uint64) []byte {
	var mvhd []byte
	if version == 0 {
		mvhd = make([]byte, 100)
		binary.BigEndian.PutUint32(mvhd[12:], timescale)
		binary.BigEndian.PutUint32(mvhd[16:], uint32(dur))
	} else {
		mvhd = make([]byte, 112)
		mvhd[0] = 1
		binary.BigEndian.PutUint32(mvhd[20:], timescale)
		binary.BigEndian.PutUint64(mvhd[24:], dur)
	}
	out := box("ftyp", []byte("isom\x00\x00\x02\x00"))
	return append(out, box("moov", box("mvhd", mvhd))...)
}

func TestDurationReadsBothHeaderVersions(t *testing.T) {
	for _, v := range []byte{0, 1} {
		d, ok := duration(mp4(v, 600, 6000))
		if !ok || d != 10 {
			t.Errorf("version %d: %v %v", v, d, ok)
		}
	}
}

func TestDurationRefusesUnreadableFiles(t *testing.T) {
	cases := map[string][]byte{
		"empty":        nil,
		"no moov":      box("ftyp", []byte("isom\x00\x00\x02\x00")),
		"zero scale":   mp4(0, 0, 100),
		"truncated":    mp4(0, 1000, 100)[:30],
		"size overrun": append([]byte{0xff, 0xff, 0xff, 0xff, 'm', 'o', 'o', 'v'}, make([]byte, 20)...),
	}
	for name, b := range cases {
		if d, ok := duration(b); ok {
			t.Errorf("%s accepted with duration %v", name, d)
		}
	}
}

func TestSniffOnlyAcceptsMP4Family(t *testing.T) {
	if m, ok := sniff(mp4(0, 1000, 1000)); !ok || m != "video/mp4" {
		t.Errorf("mp4: %q %v", m, ok)
	}
	qt := mp4(0, 1000, 1000)
	copy(qt[8:12], "qt  ")
	if m, ok := sniff(qt); !ok || m != "video/quicktime" {
		t.Errorf("mov: %q %v", m, ok)
	}
	for _, b := range [][]byte{nil, []byte("\x1a\x45\xdf\xa3 webm....."), []byte("\x89PNG\r\n\x1a\n........")} {
		if _, ok := sniff(b); ok {
			t.Errorf("accepted %q", b[:min(len(b), 8)])
		}
	}
}
