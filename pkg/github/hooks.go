package github

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The App's webhook deliveries (RFC-0054, webhooks before polling): what
// GitHub sent and how shpyrd answered, kept three days, and a redelivery
// of one that failed. Read with the App's JWT.

// HookDelivery is one attempt of a delivery: its GUID is the delivery's
// (the X-GitHub-Delivery header), the same on every attempt.
type HookDelivery struct {
	ID           int64
	GUID         string
	DeliveredAt  time.Time
	Redelivery   bool
	StatusCode   int
	Event        string
	Action       string
	Installation int64
}

// Delivered says the attempt reached shpyrd and was taken: a 2xx or 3xx
// answer. 0 is no answer at all.
func (d HookDelivery) Delivered() bool { return d.StatusCode >= 200 && d.StatusCode < 400 }

// HookDeliveries are the App's delivery attempts since a time, newest
// first.
func (c *Client) HookDeliveries(ctx context.Context, since time.Time) ([]HookDelivery, error) {
	bearer, err := c.AppJWT()
	if err != nil {
		return nil, err
	}
	out := []HookDelivery{}
	next := fmt.Sprintf("%s/app/hook/deliveries?per_page=%d", c.api, perPage)
	for page := 0; next != ""; page++ {
		if page == maxPages {
			// Cut at the most pages shpyrd reads: what was read is kept.
			return out, ErrTruncated
		}
		var res []struct {
			ID             int64     `json:"id"`
			GUID           string    `json:"guid"`
			DeliveredAt    time.Time `json:"delivered_at"`
			Redelivery     bool      `json:"redelivery"`
			StatusCode     int       `json:"status_code"`
			Event          string    `json:"event"`
			Action         string    `json:"action"`
			InstallationID int64     `json:"installation_id"`
		}
		h, err := c.callHeaders(ctx, "GET", next, bearer, nil, &res)
		if err != nil {
			return nil, err
		}
		older := false
		for _, d := range res {
			if d.DeliveredAt.Before(since) {
				older = true
				continue
			}
			out = append(out, HookDelivery{ID: d.ID, GUID: d.GUID, DeliveredAt: d.DeliveredAt, Redelivery: d.Redelivery,
				StatusCode: d.StatusCode, Event: d.Event, Action: d.Action, Installation: d.InstallationID})
		}
		if older || len(res) == 0 {
			break
		}
		next = c.nextPage(h)
	}
	return out, nil
}

// Redeliver asks GitHub to deliver a delivery again.
func (c *Client) Redeliver(ctx context.Context, id int64) error {
	bearer, err := c.AppJWT()
	if err != nil {
		return err
	}
	return c.call(ctx, "POST", fmt.Sprintf("%s/app/hook/deliveries/%d/attempts", c.api, id), bearer, nil, nil)
}

var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextPage is the next page GitHub's Link header names, when it is on
// GitHub's API: a link elsewhere is not followed.
func (c *Client) nextPage(h http.Header) string {
	m := linkNext.FindStringSubmatch(h.Get("Link"))
	if m == nil || !strings.HasPrefix(m[1], c.api+"/") {
		return ""
	}
	return m[1]
}
