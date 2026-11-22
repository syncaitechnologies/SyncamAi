// Package boxsetup relays "Find cameras" work between the dashboard and a
// site box: the dashboard queues a task (scan the network, check a device
// with its password, add channels as cameras), the box picks it up with its
// regular poll, runs it on the site's network and posts the result back.
//
// Everything is held in memory for at most an hour and never written to the
// database. Camera passwords only ever pass through here encrypted for one
// box (see setup_crypto.py on the box), and a task's payload is dropped as
// soon as the box has picked it up. The hub is per process, which fits the
// single Render instance.
package boxsetup

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// TaskTTL is how long tasks and their results are kept.
	TaskTTL = time.Hour
	// MaxOpenTasks bounds the queue of one site.
	MaxOpenTasks = 20
)

var (
	ErrUnknownType  = errors.New("unknown setup task type")
	ErrTooManyTasks = errors.New("too many open setup tasks for this site")
	ErrTaskNotFound = errors.New("setup task not found")
	ErrBadStatus    = errors.New("setup task status must be done or failed")
)

// Types the site box understands. "restart" asks the box to stop its cameras
// and AI and start them again (box version 0.1.3 and later; older boxes
// answer that they do not know the task).
var Types = map[string]bool{"scan": true, "probe": true, "adopt": true, "restart": true}

// TypeRestart is the remote restart of a site box.
const TypeRestart = "restart"

// Task is one unit of setup work and, once finished, its result.
type Task struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"` // pending, running, done, failed
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Delivery is what the box receives: the task with its payload.
type Delivery struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type entry struct {
	task             Task
	tenantID, siteID string
	payload          json.RawMessage
}

type Hub struct {
	mu    sync.Mutex
	now   func() time.Time
	tasks map[string]*entry
}

func NewHub() *Hub { return NewHubWithClock(time.Now) }

func NewHubWithClock(now func() time.Time) *Hub {
	return &Hub{now: now, tasks: map[string]*entry{}}
}

func (h *Hub) prune(now time.Time) {
	for id, item := range h.tasks {
		if now.Sub(item.task.CreatedAt) > TaskTTL {
			delete(h.tasks, id)
		}
	}
}

// Create queues a task for one site's box.
func (h *Hub) Create(tenantID, siteID, kind string, payload json.RawMessage) (Task, error) {
	if !Types[kind] {
		return Task{}, ErrUnknownType
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.prune(now)
	open := 0
	for _, item := range h.tasks {
		if item.tenantID == tenantID && item.siteID == siteID && (item.task.Status == "pending" || item.task.Status == "running") {
			// A restart already on its way is reused, so repeated clicks
			// restart the box once.
			if kind == TypeRestart && item.task.Type == TypeRestart {
				return item.task, nil
			}
			open++
		}
	}
	if open >= MaxOpenTasks {
		return Task{}, ErrTooManyTasks
	}
	task := Task{ID: uuid.NewString(), Type: kind, Status: "pending", CreatedAt: now, UpdatedAt: now}
	h.tasks[task.ID] = &entry{task: task, tenantID: tenantID, siteID: siteID, payload: append(json.RawMessage(nil), payload...)}
	return task, nil
}

// Deliver hands the site's pending tasks to its box, oldest first, marks
// them running and forgets their payloads (which may hold encrypted
// credentials) right away.
func (h *Hub) Deliver(tenantID, siteID string) []Delivery {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.prune(now)
	pending := make([]*entry, 0)
	for _, item := range h.tasks {
		if item.tenantID == tenantID && item.siteID == siteID && item.task.Status == "pending" {
			pending = append(pending, item)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].task.CreatedAt.Before(pending[j].task.CreatedAt) })
	deliveries := make([]Delivery, 0, len(pending))
	for _, item := range pending {
		deliveries = append(deliveries, Delivery{ID: item.task.ID, Type: item.task.Type, Payload: item.payload})
		item.payload = nil
		item.task.Status = "running"
		item.task.UpdatedAt = now
	}
	return deliveries
}

// Complete records the box's result for a task of its own site.
func (h *Hub) Complete(tenantID, siteID, taskID, status string, result json.RawMessage, message string) error {
	if status != "done" && status != "failed" {
		return ErrBadStatus
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	item, ok := h.tasks[taskID]
	if !ok || item.tenantID != tenantID || item.siteID != siteID {
		return ErrTaskNotFound
	}
	item.task.Status = status
	item.task.Result = append(json.RawMessage(nil), result...)
	item.task.Error = message
	item.task.UpdatedAt = h.now()
	item.payload = nil
	return nil
}

// Get returns a task of the given site.
func (h *Hub) Get(tenantID, siteID, taskID string) (Task, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prune(h.now())
	item, ok := h.tasks[taskID]
	if !ok || item.tenantID != tenantID || item.siteID != siteID {
		return Task{}, false
	}
	return item.task, true
}
