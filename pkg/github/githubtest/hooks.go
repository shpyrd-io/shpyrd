package githubtest

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The App's delivery log (RFC-0054, the sweep): attempts added with
// AddDelivery, listed newest first with a Link to the next page, and
// redelivered, which adds an attempt that answers SetRedeliveryStatus's
// code (202 unless set).

// Delivery is one attempt of a webhook delivery.
type Delivery struct {
	ID             int64
	GUID           string
	DeliveredAt    time.Time
	Redelivery     bool
	StatusCode     int
	Event, Action  string
	InstallationID int64
}

// AddDelivery records an attempt; its ID is given when it has none.
func (f *Server) AddDelivery(d Delivery) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d.ID == 0 {
		f.nextID++
		d.ID = f.nextID
	}
	f.deliveries = append(f.deliveries, d)
	return d.ID
}

// Redelivered are the ids asked to be delivered again, in order.
func (f *Server) Redelivered() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.redelivered...)
}

// SetRedeliveryStatus is how shpyrd answers a redelivery: its status code.
func (f *Server) SetRedeliveryStatus(code int) { f.mu.Lock(); f.redeliveryStatus = code; f.mu.Unlock() }

// appJWT says the request carries the App's JWT.
func (f *Server) appJWT(r *http.Request) bool {
	_, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &jwt.RegisteredClaims{},
		func(*jwt.Token) (any, error) { return &Key().PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithTimeFunc(f.clock), jwt.WithIssuer(strconv.Itoa(AppID)))
	return err == nil
}

// hookDeliveries is GET /app/hook/deliveries: newest first, cursor pages.
func (f *Server) hookDeliveries(w http.ResponseWriter, r *http.Request) {
	if !f.appJWT(r) {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	list := append([]Delivery(nil), f.deliveries...)
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].DeliveredAt.Equal(list[j].DeliveredAt) {
			return list[i].DeliveredAt.After(list[j].DeliveredAt)
		}
		return list[i].ID > list[j].ID
	})
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	from, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	from = min(max(from, 0), len(list))
	to := min(from+per, len(list))
	if to < len(list) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/app/hook/deliveries?per_page=%d&cursor=%d>; rel="next"`, f.URL, per, to))
	}
	out := []map[string]any{}
	for _, d := range list[from:to] {
		out = append(out, map[string]any{"id": d.ID, "guid": d.GUID, "delivered_at": d.DeliveredAt.UTC().Format(time.RFC3339),
			"redelivery": d.Redelivery, "status_code": d.StatusCode, "status": http.StatusText(d.StatusCode), "event": d.Event,
			"action": d.Action, "installation_id": d.InstallationID})
	}
	reply(w, http.StatusOK, out)
}

// redeliver is POST /app/hook/deliveries/{id}/attempts.
func (f *Server) redeliver(w http.ResponseWriter, r *http.Request, idText string) {
	if !f.appJWT(r) {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	for _, d := range f.deliveries {
		if d.ID != id {
			continue
		}
		f.redelivered = append(f.redelivered, id)
		f.nextID++
		again := d
		again.ID, again.Redelivery, again.DeliveredAt = f.nextID, true, f.clock()
		again.StatusCode = f.redeliveryStatus
		if again.StatusCode == 0 {
			again.StatusCode = http.StatusAccepted
		}
		f.deliveries = append(f.deliveries, again)
		reply(w, http.StatusAccepted, map[string]any{})
		return
	}
	reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}
