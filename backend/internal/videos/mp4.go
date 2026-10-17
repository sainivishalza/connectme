package videos

import "encoding/binary"

// sniff identifies an MP4/QuickTime file from its leading bytes. The client's
// claimed type is never trusted; webm and everything else is refused.
func sniff(b []byte) (string, bool) {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return "", false
	}
	if string(b[8:12]) == "qt  " {
		return "video/quicktime", true
	}
	return "video/mp4", true
}

// duration reads the movie length (in seconds) from the moov/mvhd box. It
// returns false when the file has no readable header, so a client cannot claim
// a short length for a long upload.
func duration(b []byte) (float64, bool) {
	moov, ok := findBox(b, "moov")
	if !ok {
		return 0, false
	}
	h, ok := findBox(moov, "mvhd")
	if !ok || len(h) < 1 {
		return 0, false
	}
	var timescale uint32
	var dur uint64
	switch h[0] {
	case 0:
		if len(h) < 20 {
			return 0, false
		}
		timescale = binary.BigEndian.Uint32(h[12:16])
		dur = uint64(binary.BigEndian.Uint32(h[16:20]))
	case 1:
		if len(h) < 32 {
			return 0, false
		}
		timescale = binary.BigEndian.Uint32(h[20:24])
		dur = binary.BigEndian.Uint64(h[24:32])
	default:
		return 0, false
	}
	if timescale == 0 {
		return 0, false
	}
	return float64(dur) / float64(timescale), true
}

// findBox returns the payload of the first top-level box of the given type.
func findBox(b []byte, typ string) ([]byte, bool) {
	for len(b) >= 8 {
		size := uint64(binary.BigEndian.Uint32(b[:4]))
		head := uint64(8)
		switch size {
		case 0:
			size = uint64(len(b))
		case 1:
			if len(b) < 16 {
				return nil, false
			}
			size = binary.BigEndian.Uint64(b[8:16])
			head = 16
		}
		if size < head || size > uint64(len(b)) {
			return nil, false
		}
		if string(b[4:8]) == typ {
			return b[head:size], true
		}
		b = b[size:]
	}
	return nil, false
}
