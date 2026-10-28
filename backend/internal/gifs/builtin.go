package gifs

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math"
	"sort"
	"strings"
	"sync"
)

// Builtin serves a small set of animated stickers drawn by the server itself.
// They are abstract (shapes and simple faces), searchable by a few words each,
// need no account or network, and cannot contain anything unexpected.
type Builtin struct {
	mu    sync.Mutex
	cache map[string][]byte
}

func NewBuiltin() *Builtin { return &Builtin{cache: map[string][]byte{}} }

func (*Builtin) Name() string { return "builtin" }

type sticker struct {
	id, title string
	tags      []string
	draw      func(f, n int, s float64) func(img *image.Paletted)
	bg        color.RGBA
}

const (
	size   = 160
	frames = 12
	delay  = 7 // 70 ms
)

var (
	cPink   = color.RGBA{0xF4, 0x5B, 0x8D, 0xFF}
	cRed    = color.RGBA{0xE5, 0x48, 0x4D, 0xFF}
	cYellow = color.RGBA{0xFF, 0xCB, 0x2E, 0xFF}
	cBlue   = color.RGBA{0x3E, 0x8B, 0xFF, 0xFF}
	cGreen  = color.RGBA{0x2E, 0xC2, 0x7E, 0xFF}
	cWhite  = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	cInk    = color.RGBA{0x1B, 0x1A, 0x2E, 0xFF}
	cOrange = color.RGBA{0xFF, 0x8A, 0x3D, 0xFF}
	cPurple = color.RGBA{0x7A, 0x6B, 0xE6, 0xFF}
	cSoft   = color.RGBA{0xFD, 0xE7, 0xEE, 0xFF}
)

func disc(img *image.Paletted, cx, cy, r float64, c color.Color) {
	x0, x1 := int(math.Floor(cx-r)), int(math.Ceil(cx+r))
	y0, y1 := int(math.Floor(cy-r)), int(math.Ceil(cy+r))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if (float64(x)-cx)*(float64(x)-cx)+(float64(y)-cy)*(float64(y)-cy) <= r*r && image.Pt(x, y).In(img.Rect) {
				img.Set(x, y, c)
			}
		}
	}
}

func poly(img *image.Paletted, pts [][2]float64, c color.Color) {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	for y := int(minY); y <= int(maxY); y++ {
		var xs []float64
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= float64(y) && b[1] > float64(y)) || (b[1] <= float64(y) && a[1] > float64(y)) {
				xs = append(xs, a[0]+(float64(y)-a[1])/(b[1]-a[1])*(b[0]-a[0]))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			for x := int(xs[i]); x <= int(xs[i+1]); x++ {
				if image.Pt(x, y).In(img.Rect) {
					img.Set(x, y, c)
				}
			}
		}
	}
}

func line(img *image.Paletted, x0, y0, x1, y1, w float64, c color.Color) {
	n := int(math.Hypot(x1-x0, y1-y0)) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		disc(img, x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, c)
	}
}

func heartPts(cx, cy, s float64) [][2]float64 {
	var pts [][2]float64
	for i := 0; i < 48; i++ {
		t := float64(i) / 48 * 2 * math.Pi
		x := 16 * math.Pow(math.Sin(t), 3)
		y := 13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t)
		pts = append(pts, [2]float64{cx + x*s, cy - y*s})
	}
	return pts
}

func starPts(cx, cy, r, rot float64) [][2]float64 {
	var pts [][2]float64
	for i := 0; i < 10; i++ {
		rad := r
		if i%2 == 1 {
			rad = r * 0.45
		}
		a := rot + float64(i)*math.Pi/5 - math.Pi/2
		pts = append(pts, [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
	}
	return pts
}

func face(img *image.Paletted, cx, cy, r float64, body color.Color) {
	disc(img, cx, cy, r, body)
}

var stickers = []sticker{
	{id: "heart", title: "Pulsing heart", tags: []string{"love", "heart", "like", "crush", "aww"}, bg: cSoft,
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				k := 4.0 + 0.9*math.Sin(2*math.Pi*float64(f)/float64(n))
				poly(img, heartPts(80, 82, k), cPink)
				poly(img, heartPts(80, 82, k*0.55), color.RGBA{0xFF, 0x9F, 0xBE, 0xFF})
			}
		}},
	{id: "star", title: "Spinning star", tags: []string{"wow", "star", "great", "amazing", "win"}, bg: color.RGBA{0xFF, 0xF3, 0xC4, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				poly(img, starPts(80, 80, 58, 2*math.Pi*float64(f)/float64(n)/5), cYellow)
				disc(img, 80, 80, 12, cOrange)
			}
		}},
	{id: "party", title: "Confetti", tags: []string{"party", "celebrate", "yay", "congrats", "birthday"}, bg: color.RGBA{0xEE, 0xEA, 0xFF, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				cols := []color.RGBA{cPink, cYellow, cBlue, cGreen, cOrange, cPurple}
				for i := 0; i < 24; i++ {
					x := float64((i*47)%150 + 5)
					y := math.Mod(float64((i*29)%160)+float64(f)*14*(0.6+float64(i%4)*0.2), 170) - 5
					disc(img, x, y, 4+float64(i%3), cols[i%len(cols)])
				}
			}
		}},
	{id: "hello", title: "Hello bounce", tags: []string{"hello", "hi", "hey", "wave", "greeting"}, bg: color.RGBA{0xE4, 0xF1, 0xFF, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				for i := 0; i < 3; i++ {
					y := 90 - 34*math.Abs(math.Sin(2*math.Pi*(float64(f)/float64(n)+float64(i)*0.18)))
					disc(img, 40+float64(i)*40, y, 15, []color.RGBA{cBlue, cPurple, cPink}[i])
				}
			}
		}},
	{id: "yes", title: "Yes", tags: []string{"yes", "ok", "agree", "sure", "done", "deal"}, bg: color.RGBA{0xDF, 0xF7, 0xEB, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				r := 54 + 6*math.Sin(2*math.Pi*float64(f)/float64(n))
				disc(img, 80, 80, r, cGreen)
				line(img, 55, 82, 74, 102, 12, cWhite)
				line(img, 74, 102, 108, 60, 12, cWhite)
			}
		}},
	{id: "no", title: "No", tags: []string{"no", "nope", "disagree", "stop", "never"}, bg: color.RGBA{0xFF, 0xE6, 0xE6, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				dx := 7 * math.Sin(4*math.Pi*float64(f)/float64(n))
				disc(img, 80+dx, 80, 54, cRed)
				line(img, 58+dx, 58, 102+dx, 102, 12, cWhite)
				line(img, 102+dx, 58, 58+dx, 102, 12, cWhite)
			}
		}},
	{id: "laugh", title: "Laughing", tags: []string{"haha", "lol", "funny", "laugh", "joke"}, bg: color.RGBA{0xFF, 0xF6, 0xD6, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				face(img, 80, 82+3*math.Sin(4*math.Pi*float64(f)/float64(n)), 56, cYellow)
				disc(img, 60, 68, 5, cInk)
				disc(img, 100, 68, 5, cInk)
				open := 8 + 14*math.Abs(math.Sin(2*math.Pi*float64(f)/float64(n)))
				disc(img, 80, 98, open, cInk)
				disc(img, 80, 98+open*0.45, open*0.55, cPink)
			}
		}},
	{id: "sad", title: "Sad", tags: []string{"sad", "cry", "sorry", "miss", "upset"}, bg: color.RGBA{0xE6, 0xEE, 0xFA, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				face(img, 80, 78, 54, cYellow)
				disc(img, 60, 68, 5, cInk)
				disc(img, 100, 68, 5, cInk)
				line(img, 62, 106, 80, 98, 5, cInk)
				line(img, 80, 98, 98, 106, 5, cInk)
				ty := 76 + math.Mod(float64(f)*10, 70)
				disc(img, 100, ty, 6, cBlue)
			}
		}},
	{id: "wow", title: "Wow", tags: []string{"wow", "surprise", "omg", "shock", "whoa"}, bg: color.RGBA{0xF1, 0xEA, 0xFF, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				face(img, 80, 80, 54, cYellow)
				disc(img, 60, 64, 8, cWhite)
				disc(img, 100, 64, 8, cWhite)
				disc(img, 60, 64, 4, cInk)
				disc(img, 100, 64, 4, cInk)
				disc(img, 80, 104, 6+10*math.Abs(math.Sin(math.Pi*float64(f)/float64(n))), cInk)
			}
		}},
	{id: "kiss", title: "Flirty", tags: []string{"kiss", "flirt", "xoxo", "cute", "smitten"}, bg: cSoft,
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				face(img, 70, 90, 46, cYellow)
				disc(img, 54, 80, 4, cInk)
				line(img, 80, 78, 94, 78, 5, cInk)
				disc(img, 72, 106, 5, cRed)
				for i := 0; i < 3; i++ {
					p := math.Mod(float64(f)/float64(n)+float64(i)/3, 1)
					poly(img, heartPts(112+8*float64(i), 90-70*p, 1.0+0.5*(1-p)), cPink)
				}
			}
		}},
	{id: "sleepy", title: "Sleepy", tags: []string{"sleep", "tired", "night", "bed", "yawn"}, bg: color.RGBA{0xE9, 0xE7, 0xFF, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				disc(img, 70, 100, 40, cPurple)
				disc(img, 56, 92, 4, cWhite)
				line(img, 60, 102, 82, 102, 4, cWhite)
				for i := 0; i < 3; i++ {
					p := math.Mod(float64(f)/float64(n)+float64(i)/3, 1)
					disc(img, 104+14*p, 70-50*p, 6+float64(i)*2, color.RGBA{0xB9, 0xB3, 0xFF, 0xFF})
				}
			}
		}},
	{id: "fire", title: "Fire", tags: []string{"fire", "hot", "lit", "spicy", "burn"}, bg: color.RGBA{0xFF, 0xE9, 0xD6, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				w := 4 * math.Sin(4*math.Pi*float64(f)/float64(n))
				poly(img, [][2]float64{{80, 18 + w}, {112 + w, 70}, {104, 128}, {80, 140}, {56, 128}, {48 - w, 70}}, cOrange)
				poly(img, [][2]float64{{80, 56 - w}, {100, 96}, {94, 128}, {80, 134}, {66, 128}, {60, 96}}, cYellow)
			}
		}},
	{id: "coffee", title: "Coffee date", tags: []string{"coffee", "date", "drink", "tea", "morning"}, bg: color.RGBA{0xF6, 0xEC, 0xE0, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				poly(img, [][2]float64{{46, 80}, {114, 80}, {106, 134}, {54, 134}}, color.RGBA{0x8B, 0x5A, 0x3C, 0xFF})
				line(img, 114, 90, 130, 90, 7, color.RGBA{0x8B, 0x5A, 0x3C, 0xFF})
				line(img, 130, 90, 126, 116, 7, color.RGBA{0x8B, 0x5A, 0x3C, 0xFF})
				for i := 0; i < 3; i++ {
					p := math.Mod(float64(f)/float64(n)+float64(i)/3, 1)
					disc(img, 64+16*float64(i)+6*math.Sin(6*p), 72-50*p, 5, color.RGBA{0xC9, 0xC2, 0xBA, 0xFF})
				}
			}
		}},
	{id: "dance", title: "Dancing", tags: []string{"dance", "music", "party", "groove", "fun"}, bg: color.RGBA{0xE8, 0xFF, 0xF3, 0xFF},
		draw: func(f, n int, s float64) func(*image.Paletted) {
			return func(img *image.Paletted) {
				sw := 22 * math.Sin(2*math.Pi*float64(f)/float64(n))
				line(img, 80, 78, 80+sw*0.3, 124, 9, cGreen)
				disc(img, 80, 60, 20, cGreen)
				line(img, 80, 90, 52+sw, 70, 8, cGreen)
				line(img, 80, 90, 108+sw, 70, 8, cGreen)
				line(img, 80+sw*0.3, 124, 60-sw*0.4, 148, 8, cGreen)
				line(img, 80+sw*0.3, 124, 100+sw*0.4, 148, 8, cGreen)
			}
		}},
}

func (b *Builtin) Search(_ context.Context, query string, limit int) ([]Item, error) {
	q := strings.ToLower(CleanQuery(query))
	var out []Item
	for _, s := range stickers {
		if q != "" && !matches(s, q) {
			continue
		}
		out = append(out, Item{ID: s.id, Title: s.title})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func matches(s sticker, q string) bool {
	for _, w := range strings.Fields(q) {
		hit := strings.Contains(strings.ToLower(s.title), w)
		for _, t := range s.tags {
			hit = hit || strings.HasPrefix(t, w) || strings.Contains(t, w)
		}
		if !hit {
			return false
		}
	}
	return true
}

func (b *Builtin) Media(_ context.Context, id string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if data, ok := b.cache[id]; ok {
		return data, nil
	}
	for _, s := range stickers {
		if s.id == id {
			data, err := render(s)
			if err != nil {
				return nil, err
			}
			b.cache[id] = data
			return data, nil
		}
	}
	return nil, ErrNotFound
}

func render(s sticker) ([]byte, error) {
	pal := color.Palette{s.bg, cPink, cRed, cYellow, cBlue, cGreen, cWhite, cInk, cOrange, cPurple,
		color.RGBA{0xFF, 0x9F, 0xBE, 0xFF}, color.RGBA{0xB9, 0xB3, 0xFF, 0xFF}, color.RGBA{0x8B, 0x5A, 0x3C, 0xFF}, color.RGBA{0xC9, 0xC2, 0xBA, 0xFF}}
	g := &gif.GIF{LoopCount: 0}
	for f := 0; f < frames; f++ {
		img := image.NewPaletted(image.Rect(0, 0, size, size), pal)
		draw.Draw(img, img.Bounds(), &image.Uniform{s.bg}, image.Point{}, draw.Src)
		s.draw(f, frames, 1)(img)
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, delay)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
