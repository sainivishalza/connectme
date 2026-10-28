// Package sounds is a small library of background loops for short videos.
//
// Every loop is synthesised here from a few notes and a drum pattern, so there
// is no recording, no sample and no third party behind any of them: nothing to
// license, nothing that can be taken down. They are short loops meant to sit
// under a clip, not songs.
package sounds

import (
	"bytes"
	"encoding/binary"
	"math"
	"net/http"
	"sync"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const sampleRate = 22050

// Sound is one loop in the library.
type Sound struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Mood    string  `json:"mood"`
	Seconds float64 `json:"seconds"`

	bpm   float64
	root  float64   // Hz of the key note
	scale []float64 // semitone offsets
	notes []int     // scale degrees for the melody, one per eighth note (-1 = rest)
	drums string    // 16 steps: k kick, h hat, s snare, . rest
	wave  int       // 0 soft sine, 1 bright, 2 mellow pad
}

// Library lists every sound. Ids are stable.
var Library = []*Sound{
	{ID: "sunrise", Name: "Sunrise", Mood: "warm", bpm: 96, root: 261.63, scale: major, notes: []int{0, 2, 4, 2, 5, 4, 2, 0, 0, 2, 4, 7, 5, 4, 2, 4}, drums: "k...h...k..kh...", wave: 0},
	{ID: "pulse", Name: "Pulse", Mood: "upbeat", bpm: 118, root: 220, scale: minorPent, notes: []int{0, -1, 2, 0, 3, -1, 2, 0, 4, 3, 2, -1, 3, 2, 0, -1}, drums: "k.h.s.hkk.h.s.h.", wave: 1},
	{ID: "afterglow", Name: "Afterglow", Mood: "calm", bpm: 72, root: 196, scale: major, notes: []int{0, -1, 4, -1, 2, -1, 5, -1, 4, -1, 2, -1, 0, -1, -1, -1}, drums: "k.......h.......", wave: 2},
	{ID: "citylights", Name: "City lights", Mood: "night", bpm: 104, root: 246.94, scale: minor, notes: []int{0, 3, 5, 3, 7, 5, 3, 0, 2, 3, 5, 7, 8, 7, 5, 3}, drums: "k..hk..hk..hk.hh", wave: 1},
	{ID: "daydream", Name: "Daydream", Mood: "dreamy", bpm: 84, root: 293.66, scale: lydian, notes: []int{0, 2, 4, 6, 4, 2, 0, -1, 2, 4, 6, 4, 2, 0, -1, -1}, drums: "k.......k...h...", wave: 2},
	{ID: "playful", Name: "Playful", Mood: "fun", bpm: 126, root: 329.63, scale: major, notes: []int{0, 2, 4, 2, 0, 2, 4, 2, 1, 3, 5, 3, 1, 3, 5, 3}, drums: "k.hhk.hhk.hhk.hh", wave: 1},
	{ID: "slowdance", Name: "Slow dance", Mood: "romantic", bpm: 66, root: 174.61, scale: major, notes: []int{0, -1, 2, -1, 4, -1, 2, -1, 5, -1, 4, -1, 2, -1, 0, -1}, drums: "k.......s.......", wave: 2},
	{ID: "roadtrip", Name: "Road trip", Mood: "energetic", bpm: 132, root: 196, scale: majorPent, notes: []int{0, 1, 2, 4, 2, 1, 0, 1, 2, 4, 3, 2, 1, 2, 0, -1}, drums: "k.s.k.s.k.s.k.sh", wave: 0},
}

var (
	major     = []float64{0, 2, 4, 5, 7, 9, 11, 12, 14}
	minor     = []float64{0, 2, 3, 5, 7, 8, 10, 12, 14}
	minorPent = []float64{0, 3, 5, 7, 10, 12}
	majorPent = []float64{0, 2, 4, 7, 9, 12}
	lydian    = []float64{0, 2, 4, 6, 7, 9, 11, 12}
)

var (
	mu    sync.Mutex
	cache = map[string][]byte{}
)

// Exists reports whether id is a sound in the library ("" means none).
func Exists(id string) bool {
	for _, s := range Library {
		if s.ID == id {
			return true
		}
	}
	return false
}

// Name returns a sound's display name.
func Name(id string) string {
	for _, s := range Library {
		if s.ID == id {
			return s.Name
		}
	}
	return ""
}

func (s *Sound) degree(d int) float64 {
	if d < 0 {
		return 0
	}
	return s.root * math.Pow(2, s.scale[d%len(s.scale)]/12)
}

// Render makes the loop as a mono 16-bit WAV file.
func (s *Sound) Render() []byte {
	mu.Lock()
	defer mu.Unlock()
	if b, ok := cache[s.ID]; ok {
		return b
	}
	eighth := 60.0 / s.bpm / 2
	steps := len(s.notes)
	total := int(float64(steps) * 2 * eighth * sampleRate) // two passes through the melody
	out := make([]float64, total)
	for pass := 0; pass < 2; pass++ {
		for i, d := range s.notes {
			start := int((float64(pass*steps+i) * eighth) * sampleRate)
			if d >= 0 {
				addNote(out, start, s.degree(d), eighth*1.6, s.wave, 0.30)
				if i%4 == 0 { // a low root underneath, once a beat
					addNote(out, start, s.root/2, eighth*3.2, 2, 0.22)
				}
			}
		}
		for i := 0; i < 16; i++ {
			start := int((float64(pass*16+i) * eighth) * sampleRate)
			if start >= total {
				break
			}
			switch s.drums[i] {
			case 'k':
				addKick(out, start)
			case 'h':
				addNoise(out, start, 0.05, 0.10)
			case 's':
				addNoise(out, start, 0.14, 0.18)
			}
		}
	}
	// A short fade at both ends so the loop never clicks.
	fade := sampleRate / 100
	for i := 0; i < fade && i < total; i++ {
		g := float64(i) / float64(fade)
		out[i] *= g
		out[total-1-i] *= g
	}
	var peak float64
	for _, v := range out {
		peak = math.Max(peak, math.Abs(v))
	}
	if peak == 0 {
		peak = 1
	}
	pcm := make([]int16, total)
	for i, v := range out {
		pcm[i] = int16(v / peak * 0.8 * 32767)
	}
	var buf bytes.Buffer
	dataLen := uint32(len(pcm) * 2)
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, 36+dataLen)
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataLen)
	_ = binary.Write(&buf, binary.LittleEndian, pcm)
	cache[s.ID] = buf.Bytes()
	s.Seconds = float64(total) / sampleRate
	return cache[s.ID]
}

func addNote(out []float64, start int, freq, dur float64, wave int, vol float64) {
	n := int(dur * sampleRate)
	for i := 0; i < n && start+i < len(out); i++ {
		t := float64(i) / sampleRate
		env := math.Min(1, t/0.012) * math.Exp(-3.2*t/dur)
		if wave == 2 {
			env = math.Min(1, t/0.08) * math.Exp(-1.6*t/dur)
		}
		p := 2 * math.Pi * freq * t
		v := math.Sin(p)
		switch wave {
		case 1:
			v = 0.7*math.Sin(p) + 0.3*math.Sin(2*p) + 0.15*math.Sin(3*p)
		case 2:
			v = 0.8*math.Sin(p) + 0.2*math.Sin(p*1.005)
		}
		out[start+i] += v * env * vol
	}
}

func addKick(out []float64, start int) {
	n := int(0.18 * sampleRate)
	for i := 0; i < n && start+i < len(out); i++ {
		t := float64(i) / sampleRate
		f := 110 * math.Exp(-14*t)
		out[start+i] += math.Sin(2*math.Pi*f*t*3) * math.Exp(-16*t) * 0.55
	}
}

var noiseState uint32 = 1

func addNoise(out []float64, start int, dur, vol float64) {
	n := int(dur * sampleRate)
	for i := 0; i < n && start+i < len(out); i++ {
		noiseState = noiseState*1664525 + 1013904223 // fixed sequence: the same loop every time
		r := float64(int32(noiseState))/float64(math.MaxInt32) - 0
		t := float64(i) / sampleRate
		out[start+i] += r * math.Exp(-40*t/dur) * vol
	}
}

// List returns the library with each loop's length.
func List() []Sound {
	out := make([]Sound, 0, len(Library))
	for _, s := range Library {
		s.Render()
		out = append(out, Sound{ID: s.ID, Name: s.Name, Mood: s.Mood, Seconds: math.Round(s.Seconds*10) / 10})
	}
	return out
}

// Routes mounts /sounds (sign-in required: the library is for members).
func Routes(rdb *redis.Client, secret []byte) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "sounds:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/", func(w http.ResponseWriter, _ *http.Request) {
		middleware.WriteJSON(w, http.StatusOK, map[string]any{"sounds": List()})
	})
	r.With(lim("file", 600, 15*time.Minute)).Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		for _, s := range Library {
			if s.ID == chi.URLParam(r, "id") {
				w.Header().Set("Content-Type", "audio/wav")
				w.Header().Set("Cache-Control", "private, max-age=86400")
				_, _ = w.Write(s.Render())
				return
			}
		}
		middleware.WriteError(w, http.StatusNotFound, "not found")
	})
	return r
}
