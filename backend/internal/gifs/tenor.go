package gifs

import (
	"connectme/pkg/safefetch"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Tenor searches Tenor's v2 API. The member's words go to Tenor from this
// server, not from their phone, with a fixed client key and the strictest
// content filter. Image bytes are fetched only from Tenor's own media hosts.
type Tenor struct {
	Key     string
	APIBase string // default https://tenor.googleapis.com
	// AllowHost decides which media hosts may be fetched (tests widen it).
	AllowHost func(host string) bool
	// AllowInsecure permits http media addresses (tests only).
	AllowInsecure bool
	// allowPrivate lets tests talk to servers on this machine; the address
	// check stays on in production (see pkg/safefetch).
	allowPrivate bool
}

// fetcher is the only way this client reaches the network: addresses are
// checked after DNS, redirects are re-checked against AllowHost, size and time
// are capped. Tenor's replies name the media URLs, so they are not trusted.
func (t *Tenor) fetcher() *safefetch.Client {
	return safefetch.New(safefetch.Options{
		Timeout: 10 * time.Second, MaxBytes: maxGIFBytes + 1,
		AllowHTTP: t.AllowInsecure, AllowHost: t.AllowHost, AllowPrivateNetworks: t.allowPrivate,
	})
}

const maxGIFBytes = 3 << 20

func NewTenor(key string) *Tenor {
	return &Tenor{Key: key, APIBase: "https://tenor.googleapis.com",
		AllowHost: func(h string) bool { return h == "tenor.com" || strings.HasSuffix(h, ".tenor.com") }}
}

func (*Tenor) Name() string { return "tenor" }

type tenorResult struct {
	ID           string `json:"id"`
	Title        string `json:"content_description"`
	MediaFormats map[string]struct {
		URL string `json:"url"`
	} `json:"media_formats"`
}

func (t *Tenor) call(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	q.Set("key", t.Key)
	q.Set("client_key", "connectme")
	q.Set("contentfilter", "high")
	q.Set("media_filter", "tinygif")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.APIBase+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// The API address is ours (APIBase); only the media addresses it returns are
	// restricted to Tenor's hosts, so the API call gets the address check only.
	api := safefetch.New(safefetch.Options{Timeout: 10 * time.Second, MaxBytes: 1 << 20, AllowHTTP: t.AllowInsecure, AllowPrivateNetworks: t.allowPrivate})
	return api.Do(req)
}

func (t *Tenor) results(resp *http.Response) ([]tenorResult, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tenor answered %s", resp.Status)
	}
	var body struct {
		Results []tenorResult `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	return body.Results, nil
}

func (t *Tenor) Search(ctx context.Context, query string, limit int) ([]Item, error) {
	q := url.Values{"limit": {fmt.Sprint(limit)}}
	path := "/v2/featured"
	if c := CleanQuery(query); c != "" {
		path = "/v2/search"
		q.Set("q", c)
	}
	resp, err := t.call(ctx, path, q)
	if err != nil {
		return nil, err
	}
	rs, err := t.results(resp)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, r := range rs {
		if r.ID != "" && r.MediaFormats["tinygif"].URL != "" {
			out = append(out, Item{ID: r.ID, Title: r.Title})
		}
	}
	return out, nil
}

func (t *Tenor) Media(ctx context.Context, id string) ([]byte, error) {
	if id == "" || len(id) > 40 || strings.ContainsAny(id, "/?&# ") {
		return nil, ErrNotFound
	}
	resp, err := t.call(ctx, "/v2/posts", url.Values{"ids": {id}})
	if err != nil {
		return nil, err
	}
	rs, err := t.results(resp)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 || rs[0].MediaFormats["tinygif"].URL == "" {
		return nil, ErrNotFound
	}
	u, err := url.Parse(rs[0].MediaFormats["tinygif"].URL)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && t.AllowInsecure)) || !t.AllowHost(u.Hostname()) {
		return nil, errors.New("tenor returned a media address outside its own hosts")
	}
	data, err := t.fetcher().Get(ctx, u.String())
	if err != nil || len(data) > maxGIFBytes || len(data) < 6 || string(data[:3]) != "GIF" {
		return nil, errors.New("media is not a small GIF")
	}
	return data, nil
}
