package github_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
)

func TestTheAppsDeliveriesArePagedBackToATime(t *testing.T) {
	c, fake := newClient(t)
	ctx := context.Background()
	for i := 0; i < 150; i++ {
		fake.AddDelivery(githubtest.Delivery{GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 202, DeliveredAt: t0.Add(-time.Duration(i) * time.Minute)})
	}
	list, err := c.HookDeliveries(ctx, t0.Add(-120*time.Minute))
	if err != nil || len(list) != 121 || list[0].GUID != "g-0" || list[120].GUID != "g-120" || !list[0].Delivered() {
		t.Fatalf("%d deliveries, %v", len(list), err)
	}
	pages := 0
	for _, call := range fake.Calls() {
		if call == "GET /app/hook/deliveries" {
			pages++
		}
	}
	if pages != 2 {
		t.Errorf("%d pages read: the second page reaches the time, no third is read", pages)
	}
}

func TestADeliveryIsAskedForAgain(t *testing.T) {
	c, fake := newClient(t)
	id := fake.AddDelivery(githubtest.Delivery{GUID: "g-1", Event: "push", StatusCode: 502, DeliveredAt: t0.Add(-time.Minute)})
	if err := c.Redeliver(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := fake.Redelivered(); len(got) != 1 || got[0] != id {
		t.Errorf("redelivered %v", got)
	}
	list, _ := c.HookDeliveries(context.Background(), t0.Add(-time.Hour))
	if len(list) != 2 || list[0].GUID != "g-1" || !list[0].Redelivery || !list[0].Delivered() || list[1].Delivered() {
		t.Errorf("attempts %+v", list)
	}
	if err := c.Redeliver(context.Background(), 999999); err == nil {
		t.Error("an unknown delivery")
	}
}

func TestADeliveryLogLongerThanThePagesReadIsCutShort(t *testing.T) {
	c, fake := newClient(t)
	defer github.SetPageCaps(1, 10)()
	for i := 0; i < 150; i++ {
		fake.AddDelivery(githubtest.Delivery{GUID: fmt.Sprintf("g-%d", i), Event: "push", StatusCode: 202, DeliveredAt: t0.Add(-time.Duration(i) * time.Minute)})
	}
	list, err := c.HookDeliveries(context.Background(), t0.Add(-time.Hour*24))
	if !errors.Is(err, github.ErrTruncated) || len(list) != 100 {
		t.Fatalf("%d deliveries, %v", len(list), err)
	}
}
