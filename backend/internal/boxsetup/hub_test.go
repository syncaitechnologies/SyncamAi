package boxsetup

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const (
	tenant = "11111111-1111-4111-8111-111111111111"
	siteA  = "22222222-2222-4222-8222-222222222222"
	siteB  = "33333333-3333-4333-8333-333333333333"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newHub() (*Hub, *clock) {
	c := &clock{now: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	return NewHubWithClock(c.Now), c
}

func TestTaskLifecycleDropsThePayloadOnDelivery(t *testing.T) {
	hub, c := newHub()
	payload := json.RawMessage(`{"credentials":{"ct":"ENCRYPTED"}}`)
	task, err := hub.Create(tenant, siteA, "probe", payload)
	if err != nil || task.Status != "pending" {
		t.Fatalf("create: %v %+v", err, task)
	}
	c.now = c.now.Add(time.Second)
	second, _ := hub.Create(tenant, siteA, "scan", nil)
	if got := hub.Deliver(tenant, siteB); len(got) != 0 {
		t.Fatalf("another site's box gets nothing: %+v", got)
	}
	delivered := hub.Deliver(tenant, siteA)
	if len(delivered) != 2 || delivered[0].ID != task.ID || string(delivered[0].Payload) != string(payload) || delivered[1].ID != second.ID {
		t.Fatalf("oldest first with payload: %+v", delivered)
	}
	if again := hub.Deliver(tenant, siteA); len(again) != 0 {
		t.Fatal("a task is delivered once")
	}
	if got, _ := hub.Get(tenant, siteA, task.ID); got.Status != "running" {
		t.Fatalf("delivered task is running: %+v", got)
	}
	for _, item := range hub.tasks {
		if item.payload != nil {
			t.Fatal("payloads (encrypted credentials) are dropped once delivered")
		}
	}
	if err := hub.Complete(tenant, siteB, task.ID, "done", nil, ""); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("another site cannot complete it: %v", err)
	}
	if err := hub.Complete(tenant, siteA, task.ID, "weird", nil, ""); !errors.Is(err, ErrBadStatus) {
		t.Fatalf("bad status: %v", err)
	}
	if err := hub.Complete(tenant, siteA, task.ID, "done", json.RawMessage(`{"channels":[1]}`), ""); err != nil {
		t.Fatal(err)
	}
	got, ok := hub.Get(tenant, siteA, task.ID)
	if !ok || got.Status != "done" || string(got.Result) != `{"channels":[1]}` {
		t.Fatalf("result: %+v", got)
	}
	if _, ok := hub.Get(tenant, siteB, task.ID); ok {
		t.Fatal("another site cannot read it")
	}
	c.now = c.now.Add(TaskTTL + time.Minute)
	if _, ok := hub.Get(tenant, siteA, task.ID); ok {
		t.Fatal("tasks expire after an hour")
	}
}

func TestRepeatedRestartsReuseTheOpenOne(t *testing.T) {
	hub, c := newHub()
	first, err := hub.Create(tenant, siteA, TypeRestart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := hub.Create(tenant, siteA, TypeRestart, nil); again.ID != first.ID {
		t.Fatal("a second click while the restart is pending reuses it")
	}
	if other, _ := hub.Create(tenant, siteB, TypeRestart, nil); other.ID == first.ID {
		t.Fatal("restarts are per site")
	}
	if delivered := hub.Deliver(tenant, siteA); len(delivered) != 1 || delivered[0].Type != TypeRestart {
		t.Fatalf("the box gets one restart: %+v", delivered)
	}
	if again, _ := hub.Create(tenant, siteA, TypeRestart, nil); again.ID != first.ID {
		t.Fatal("still reused while the box is restarting")
	}
	c.now = c.now.Add(time.Second)
	if err := hub.Complete(tenant, siteA, first.ID, "done", nil, ""); err != nil {
		t.Fatal(err)
	}
	if next, _ := hub.Create(tenant, siteA, TypeRestart, nil); next.ID == first.ID || next.Status != "pending" {
		t.Fatalf("after it finished a new restart can be asked for: %+v", next)
	}
}

func TestLimitsAndTypes(t *testing.T) {
	hub, _ := newHub()
	if _, err := hub.Create(tenant, siteA, "format-disk", nil); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("unknown type: %v", err)
	}
	for index := 0; index < MaxOpenTasks; index++ {
		if _, err := hub.Create(tenant, siteA, "scan", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := hub.Create(tenant, siteA, "scan", nil); !errors.Is(err, ErrTooManyTasks) {
		t.Fatalf("queue limit: %v", err)
	}
	if _, err := hub.Create(tenant, siteB, "scan", nil); err != nil {
		t.Fatalf("the limit is per site: %v", err)
	}
}
