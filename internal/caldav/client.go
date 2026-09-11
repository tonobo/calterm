// Package caldav implements the slice of CalDAV this project needs, directly
// over net/http: PROPFIND for getctag, getetag, and calendar-color, and a
// calendar-multiget REPORT returning the server's original iCalendar bytes.
package caldav

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Resource is one entry of a multistatus response, flattened to the fields
// this project cares about.
type Resource struct {
	Href         string
	ETag         string
	DisplayName  string
	CTag         string
	Color        string
	CalendarData string
	IsCalendar   bool
}

type Client struct {
	http     *http.Client
	base     *url.URL
	username string
	password string
}

// maxObjectsPerMultiGet bounds the size of a single REPORT body. Servers
// commonly reject very large requests, and a bounded batch keeps memory flat.
const maxObjectsPerMultiGet = 50

// maxResponseBytes caps a multistatus response. Large calendars produce
// large REPORT responses, so the cap is generous -- it exists to bound a
// misbehaving or hostile server, not to constrain normal use.
const maxResponseBytes = 64 << 20 // 64 MiB

func NewClient(baseURL, username, password string, insecureSkipVerify bool) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing url %q: %w", baseURL, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		http:     &http.Client{Transport: transport, Timeout: 60 * time.Second},
		base:     u,
		username: username,
		password: password,
	}, nil
}

func (c *Client) HTTPClient() *http.Client { return c.http }
func (c *Client) BaseURL() string          { return c.base.String() }

// Do performs an authenticated request, which Task 7's discovery also uses.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(c.username, c.password)
	return c.http.Do(req)
}

// resolve turns a server-supplied href into the absolute URL to request.
//
// Hrefs arrive percent-encoded, by definition. Assigning one to url.URL.Path
// and calling String() escapes it a SECOND time, turning
// /calendars/user%40example.com/ into /calendars/user%2540example.com/ -- a
// 404 on the first PROPFIND for every server whose paths contain an @, a
// space, or non-ASCII. ResolveReference does the RFC 3986 thing instead,
// preserving the original encoding and handling absolute, root-relative, and
// relative hrefs alike (the old code silently rebased a relative href onto /,
// discarding the base path).
//
// Only the request URL is resolved. The verbatim href STRING is what index
// keys and the calendar-multiget <d:href> elements must carry, so callers keep
// passing the server's own bytes around and never this.
func (c *Client) resolve(href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		// A malformed href is the server's problem; send it as-is and let the
		// server reject it, rather than silently mangling it here.
		return href
	}
	return c.base.ResolveReference(ref).String()
}

func (c *Client) request(ctx context.Context, method, path, depth, body string) (*multistatus, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.resolve(path), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s %s: server returned %d %s: %s",
			method, path, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(snippet)))
	}

	var ms multistatus
	limited := io.LimitReader(resp.Body, maxResponseBytes)
	if err := xml.NewDecoder(limited).Decode(&ms); err != nil {
		return nil, fmt.Errorf("%s %s: parsing multistatus: %w", method, path, err)
	}
	return &ms, nil
}

// flatten merges each response's 200-status propstats into a single Resource.
// Propstats with any other status carry properties the server could not
// supply, and are ignored.
func flatten(ms *multistatus) []Resource {
	out := make([]Resource, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		res := Resource{Href: r.Href}
		for _, ps := range r.Propstats {
			if statusCode(ps.Status) != http.StatusOK {
				continue
			}
			if ps.Prop.ETag != "" {
				res.ETag = normaliseETag(ps.Prop.ETag)
			}
			if ps.Prop.DisplayName != "" {
				res.DisplayName = ps.Prop.DisplayName
			}
			if ps.Prop.CTag != "" {
				res.CTag = ps.Prop.CTag
			}
			if ps.Prop.Color != "" {
				res.Color = ps.Prop.Color
			}
			if ps.Prop.CalendarData != "" {
				// encoding/xml already resolves character references, including
				// the &#13; that servers use for CRLF line endings. Do not add a
				// second unescaping pass: it would turn a legitimately escaped
				// "&amp;" in an event summary into a bare "&".
				res.CalendarData = ps.Prop.CalendarData
			}
			if ps.Prop.ResourceType.Calendar != nil {
				res.IsCalendar = true
			}
		}
		out = append(out, res)
	}
	return out
}

// statusCode extracts the numeric code from a propstat status line such as
// "HTTP/1.1 200 OK". The reason phrase is optional -- "HTTP/1.1 200" is
// perfectly legal -- so the code must be parsed rather than matched as the
// substring " 200 ": that test needs a trailing space and silently discarded
// every property of an otherwise-fine response. In ListObjectETags that meant
// an empty ETag, a skipped entry, and sync.Calendar deleting every cached
// object as server-side-deleted. It returns 0 for anything it cannot parse,
// which callers treat as "not 200".
func statusCode(status string) int {
	fields := strings.Fields(status)
	if len(fields) < 2 {
		return 0
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return code
}

// normaliseETag strips the quoting and weak-validator prefix that servers vary
// on, so that a cached ETag compares equal across syncs.
func normaliseETag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "W/")
	return strings.Trim(s, `"`)
}

// CalendarProps fetches a calendar collection's ctag, display name, and colour.
func (c *Client) CalendarProps(ctx context.Context, path string) (Resource, error) {
	ms, err := c.request(ctx, "PROPFIND", path, "0", propfindCalendarProps)
	if err != nil {
		return Resource{}, err
	}
	res := flatten(ms)
	if len(res) == 0 {
		return Resource{}, fmt.Errorf("PROPFIND %s: server returned no responses", path)
	}
	return res[0], nil
}

// ListObjectETags returns one Resource per calendar object in the collection.
// The collection resource itself is excluded.
func (c *Client) ListObjectETags(ctx context.Context, path string) ([]Resource, error) {
	ms, err := c.request(ctx, "PROPFIND", path, "1", propfindObjectETags)
	if err != nil {
		return nil, err
	}
	var out []Resource
	for _, r := range flatten(ms) {
		if r.ETag == "" {
			continue // the collection itself, or an entry the server could not describe
		}
		out = append(out, r)
	}
	return out, nil
}

// MultiGet fetches the raw iCalendar bytes for the given hrefs, in batches.
func (c *Client) MultiGet(ctx context.Context, calendarPath string, hrefs []string) ([]Resource, error) {
	var out []Resource
	for start := 0; start < len(hrefs); start += maxObjectsPerMultiGet {
		end := start + maxObjectsPerMultiGet
		if end > len(hrefs) {
			end = len(hrefs)
		}
		batch, err := c.multiGetBatch(ctx, calendarPath, hrefs[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (c *Client) multiGetBatch(ctx context.Context, calendarPath string, hrefs []string) ([]Resource, error) {
	if len(hrefs) == 0 {
		return nil, nil
	}
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	body.WriteString(`<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` + "\n")
	body.WriteString("  <d:prop><d:getetag/><c:calendar-data/></d:prop>\n")
	for _, h := range hrefs {
		body.WriteString("  <d:href>")
		xml.EscapeText(&body, []byte(h))
		body.WriteString("</d:href>\n")
	}
	body.WriteString(`</c:calendar-multiget>`)

	ms, err := c.request(ctx, "REPORT", calendarPath, "1", body.String())
	if err != nil {
		return nil, err
	}
	var out []Resource
	for _, r := range flatten(ms) {
		if r.CalendarData == "" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// PutCalendarObject replaces one existing scheduling object. If-Match makes
// the update conflict-safe: an organizer update that landed after our last
// sync must be fetched and reviewed rather than silently overwritten.
func (c *Client) PutCalendarObject(ctx context.Context, href, etag string, data []byte) error {
	if etag == "" {
		return fmt.Errorf("PUT %s: cached object has no ETag", href)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.resolve(href), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	req.Header.Set("If-Match", `"`+etag+`"`)
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", href, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("PUT %s: server returned %d %s: %s",
			href, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(snippet)))
	}
	return nil
}
