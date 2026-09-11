package caldav

import (
	"context"
	"fmt"
)

// Discover walks the standard CalDAV discovery chain — current-user-principal,
// then calendar-home-set, then the calendar collections beneath it — and
// returns one Resource per calendar.
//
// If any step fails to yield an href, discovery falls back to treating the
// configured base URL as the calendar home, which is what a user pointing
// directly at their calendar home expects.
func (c *Client) Discover(ctx context.Context) ([]Resource, error) {
	// EscapedPath, not Path: resolve now preserves whatever encoding it is
	// handed, so handing it the decoded base path would strip the encoding
	// off a home whose name contains a percent sign or a space.
	home := c.base.EscapedPath()

	if principal, err := c.findHref(ctx, home, propfindCurrentUserPrincipal, func(p davProp) *hrefProp {
		return p.CurrentUserPrincipal
	}); err == nil && principal != "" {
		if hs, err := c.findHref(ctx, principal, propfindCalendarHomeSet, func(p davProp) *hrefProp {
			return p.CalendarHomeSet
		}); err == nil && hs != "" {
			home = hs
		}
	}

	ms, err := c.request(ctx, "PROPFIND", home, "1", propfindCalendarProps)
	if err != nil {
		return nil, fmt.Errorf("listing calendars under %s: %w", home, err)
	}
	var out []Resource
	for _, r := range flatten(ms) {
		if r.IsCalendar {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no calendar collections found under %s", home)
	}
	return out, nil
}

func (c *Client) findHref(ctx context.Context, path, body string, pick func(davProp) *hrefProp) (string, error) {
	ms, err := c.request(ctx, "PROPFIND", path, "0", body)
	if err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if h := pick(ps.Prop); h != nil && h.Href != "" {
				return h.Href, nil
			}
		}
	}
	return "", fmt.Errorf("no href found at %s", path)
}
