package sounds

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestEverySoundRendersAValidLoopThatIsNotSilentOrClipped(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Library {
		if seen[s.ID] || s.ID == "" || s.Name == "" {
			t.Fatalf("bad or duplicate entry %q", s.ID)
		}
		seen[s.ID] = true
		if len(s.drums) != 16 {
			t.Errorf("%s: drum pattern has %d steps", s.ID, len(s.drums))
		}
		for _, d := range s.notes {
			if d >= len(s.scale) {
				t.Errorf("%s: note %d is outside the scale", s.ID, d)
			}
		}
		b := s.Render()
		if !bytes.HasPrefix(b, []byte("RIFF")) || !bytes.Equal(b[8:16], []byte("WAVEfmt ")) {
			t.Fatalf("%s: not a WAV file", s.ID)
		}
		if binary.LittleEndian.Uint32(b[24:28]) != sampleRate {
			t.Errorf("%s: wrong sample rate", s.ID)
		}
		data := b[44:]
		if len(data) != int(binary.LittleEndian.Uint32(b[40:44])) {
			t.Errorf("%s: data length does not match the header", s.ID)
		}
		var peak, energy float64
		for i := 0; i+1 < len(data); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(data[i:])))
			peak = math.Max(peak, math.Abs(v))
			energy += v * v
		}
		if peak < 8000 || peak > 32767 {
			t.Errorf("%s: peak %.0f is silent or clipped", s.ID, peak)
		}
		if secs := float64(len(data)/2) / sampleRate; secs < 6 || secs > 30 {
			t.Errorf("%s: loop is %.1f s", s.ID, secs)
		}
		if !bytes.Equal(b, s.Render()) {
			t.Errorf("%s: render is not stable", s.ID)
		}
	}
	if !Exists("sunrise") || Exists("") || Exists("nope") || Name("pulse") != "Pulse" {
		t.Error("lookup helpers")
	}
	if got := List(); len(got) != len(Library) || got[0].Seconds <= 0 {
		t.Errorf("list: %+v", got)
	}
}
