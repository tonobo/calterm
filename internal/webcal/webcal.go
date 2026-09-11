// Package webcal synchronizes read-only iCalendar subscriptions into the
// same raw-object cache used by CalDAV calendars.
package webcal

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/store"
)

const (
	// AccountName groups subscriptions in metadata and the calendar selector.
	// Config validation reserves it against CalDAV accounts when feeds exist.
	AccountName = "webcal"
	feedUID     = "_feed"
	maxFeedSize = 16 << 20
)

type Result struct {
	Name      string
	Events    int
	Unchanged bool
}

// Sync downloads one feed, validates it with the project's iCalendar parser,
// and atomically replaces its cached source. A bad response never destroys the
// previous good copy.
func Sync(ctx context.Context, client *http.Client, s *store.Store, feed config.WebCal) (Result, error) {
	if client == nil {
		client = HTTPClient(feed.InsecureSkipVerify)
	}
	sourceURL, err := NormalizeURL(feed.URL)
	if err != nil {
		return Result{}, err
	}

	idx, err := s.LoadIndex(AccountName, feed.Name)
	if err != nil {
		return Result{}, fmt.Errorf("loading feed index: %w", err)
	}
	previous, hasPrevious := idx.Objects[sourceURL]
	var previousBody []byte
	if hasPrevious {
		previousBody, err = s.ReadObject(AccountName, feed.Name, previous.UID)
		if err != nil {
			hasPrevious = false
			previousBody = nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating feed request: %w", err)
	}
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.9, */*;q=0.1")
	if feed.Username != "" {
		password, err := feed.Password()
		if err != nil {
			return Result{}, err
		}
		req.SetBasicAuth(feed.Username, password)
		password = ""
	}
	if hasPrevious && previous.ETag != "" {
		req.Header.Set("If-None-Match", previous.ETag)
	}
	if hasPrevious && previous.LastModified != "" {
		req.Header.Set("If-Modified-Since", previous.LastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetching feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && hasPrevious {
		return Result{Unchanged: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("fetching feed: HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedSize+1))
	if err != nil {
		return Result{}, fmt.Errorf("reading feed: %w", err)
	}
	if len(body) > maxFeedSize {
		return Result{}, fmt.Errorf("reading feed: response exceeds %d MiB", maxFeedSize>>20)
	}
	cal, err := ical.NewDecoder(bytes.NewReader(body)).Decode()
	if err != nil {
		return Result{}, fmt.Errorf("parsing feed: %w", err)
	}
	name, _ := cal.Props.Text("X-WR-CALNAME")
	result := Result{Name: strings.TrimSpace(name), Events: len(cal.Events())}
	next := feedIndex(sourceURL, resp.Header)

	if hasPrevious && bytes.Equal(previousBody, body) {
		result.Unchanged = true
		// A server may rotate validators without changing the representation.
		// Persist the fresh headers so the next request stays conditional.
		if err := s.SaveIndex(AccountName, feed.Name, next); err != nil {
			return Result{}, fmt.Errorf("saving feed index: %w", err)
		}
		return result, nil
	}
	if err := s.WriteObject(AccountName, feed.Name, feedUID, body); err != nil {
		return Result{}, fmt.Errorf("caching feed: %w", err)
	}
	if err := s.SaveIndex(AccountName, feed.Name, next); err != nil {
		return Result{}, fmt.Errorf("saving feed index: %w", err)
	}
	return result, nil
}

func feedIndex(sourceURL string, header http.Header) *store.Index {
	return &store.Index{Objects: map[string]store.ObjectRef{
		sourceURL: {
			Href:         sourceURL,
			ETag:         header.Get("ETag"),
			LastModified: header.Get("Last-Modified"),
			UID:          feedUID,
		},
	}}
}

// NormalizeURL maps the subscription-only webcal scheme onto HTTPS and
// rejects schemes the HTTP client must never execute.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid webcal URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "webcal":
		u.Scheme = "https"
	case "https", "http":
	default:
		return "", fmt.Errorf("webcal URL must use https, http, or webcal")
	}
	return u.String(), nil
}

// HTTPClient is intentionally separate so tests and callers can supply their
// own client while production still gets a finite timeout and optional TLS
// compatibility for a self-hosted feed.
func HTTPClient(insecureSkipVerify bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipVerify} //nolint:gosec -- explicit config option
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return fmt.Errorf("redirected to unsupported scheme %q", req.URL.Scheme)
			}
			if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("refusing HTTPS downgrade")
			}
			return nil
		},
	}
}
