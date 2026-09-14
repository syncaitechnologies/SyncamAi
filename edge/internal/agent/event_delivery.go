package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
)

const maxReviewEventPayloadBytes = 64 * 1024

var (
	ErrInvalidReviewEvent   = errors.New("review event is invalid")
	ErrReviewEventScope     = errors.New("review event scope does not match delivery scope")
	ErrReviewDeliveryConfig = errors.New("review event delivery configuration is invalid")
)

var reviewEventTypes = map[string]struct{}{
	"camera_health":           {},
	"intrusion":               {},
	"restricted_zone":         {},
	"loitering":               {},
	"vehicle_activity":        {},
	"weapon_review":           {},
	"fire_review":             {},
	"smoke_review":            {},
	"ppe_review":              {},
	"fall_review":             {},
	"fight_review":            {},
	"abandoned_object_review": {},
}

var vehicleReviewClasses = map[string]struct{}{
	"bicycle":    {},
	"bus":        {},
	"car":        {},
	"motorcycle": {},
	"truck":      {},
	"van":        {},
}

// ReviewEventSender owns the authenticated upstream transport. It must return
// nil only after the receiver has durably accepted this event ID. The delivery
// boundary deliberately does not provide a weaker HTTP fallback.
type ReviewEventSender interface {
	SendReviewEvent(context.Context, []byte) error
}

// ReviewEventDelivery validates and durably sequences metadata-only,
// pending-human-review events for an injected authenticated sender.
type ReviewEventDelivery struct {
	tenantID string
	siteID   string
	spool    *DurableSpool
	sender   ReviewEventSender
}

type reviewEventInput struct {
	EventID             *string    `json:"event_id"`
	TenantID            *string    `json:"tenant_id"`
	DedupeKey           *string    `json:"dedupe_key"`
	OccurredAt          *time.Time `json:"occurred_at"`
	SiteID              *string    `json:"site_id"`
	CameraID            *string    `json:"camera_id"`
	ZoneID              *string    `json:"zone_id"`
	EventType           *string    `json:"event_type"`
	ModelVersion        *string    `json:"model_version"`
	Confidence          *float64   `json:"confidence"`
	EvidenceRefs        *[]string  `json:"evidence_refs"`
	RequiresHumanReview *bool      `json:"requires_human_review"`
	ReviewState         *string    `json:"review_state"`
	ObservedBehavior    *string    `json:"observed_behavior"`
	SubjectClass        *string    `json:"subject_class"`
}

type reviewEvent struct {
	EventID             string    `json:"event_id"`
	TenantID            string    `json:"tenant_id"`
	DedupeKey           string    `json:"dedupe_key"`
	OccurredAt          time.Time `json:"occurred_at"`
	SiteID              string    `json:"site_id"`
	CameraID            string    `json:"camera_id"`
	ZoneID              string    `json:"zone_id"`
	EventType           string    `json:"event_type"`
	ModelVersion        string    `json:"model_version"`
	Confidence          float64   `json:"confidence"`
	EvidenceRefs        []string  `json:"evidence_refs"`
	RequiresHumanReview bool      `json:"requires_human_review"`
	ReviewState         string    `json:"review_state"`
	ObservedBehavior    string    `json:"observed_behavior,omitempty"`
	SubjectClass        string    `json:"subject_class,omitempty"`
}

func NewReviewEventDelivery(tenantID, siteID string, spool *DurableSpool, sender ReviewEventSender) (*ReviewEventDelivery, error) {
	tenantID, ok := canonicalReviewUUID(tenantID)
	if !ok {
		return nil, ErrReviewDeliveryConfig
	}
	siteID, ok = canonicalReviewUUID(siteID)
	if !ok || spool == nil || sender == nil {
		return nil, ErrReviewDeliveryConfig
	}
	return &ReviewEventDelivery{tenantID: tenantID, siteID: siteID, spool: spool, sender: sender}, nil
}

// Queue validates, canonicalizes, and durably stores one event. Identical
// replay is idempotent; a changed payload for the same event ID fails closed.
// Capacity exhaustion returns backpressure without evicting an earlier event.
func (d *ReviewEventDelivery) Queue(payload []byte) (SpoolItem, error) {
	if d == nil || d.spool == nil || d.sender == nil {
		return SpoolItem{}, ErrReviewDeliveryConfig
	}
	event, canonical, err := d.validate(payload)
	if err != nil {
		return SpoolItem{}, err
	}
	return d.spool.EnqueueRetained("review-"+event.EventID, SpoolMetadata, canonical)
}

// DeliverNext sends the oldest queued review event and acknowledges local
// storage only after the sender confirms durable upstream acceptance.
func (d *ReviewEventDelivery) DeliverNext(ctx context.Context) (SpoolItem, error) {
	if d == nil || d.spool == nil || d.sender == nil || ctx == nil {
		return SpoolItem{}, ErrReviewDeliveryConfig
	}
	message, err := d.spool.NextPriority(SpoolMetadata)
	if err != nil {
		return SpoolItem{}, err
	}
	_, canonical, err := d.validate(message.Payload)
	if err != nil {
		return SpoolItem{}, err
	}
	if err := d.sender.SendReviewEvent(ctx, append([]byte(nil), canonical...)); err != nil {
		return SpoolItem{}, fmt.Errorf("send review event: %w", err)
	}
	if err := d.spool.Ack(message.ID); err != nil {
		return SpoolItem{}, fmt.Errorf("acknowledge delivered review event: %w", err)
	}
	return message.SpoolItem, nil
}

func (d *ReviewEventDelivery) validate(payload []byte) (reviewEvent, []byte, error) {
	input, err := decodeReviewEvent(payload)
	if err != nil {
		return reviewEvent{}, nil, err
	}
	eventID, valid := canonicalReviewUUID(*input.EventID)
	if !valid {
		return reviewEvent{}, nil, invalidReviewEvent("event_id must be a UUID")
	}
	tenantID, valid := canonicalReviewUUID(*input.TenantID)
	if !valid {
		return reviewEvent{}, nil, invalidReviewEvent("tenant_id must be a UUID")
	}
	siteID, valid := canonicalReviewUUID(*input.SiteID)
	if !valid {
		return reviewEvent{}, nil, invalidReviewEvent("site_id must be a UUID")
	}
	cameraID, valid := canonicalReviewUUID(*input.CameraID)
	if !valid {
		return reviewEvent{}, nil, invalidReviewEvent("camera_id must be a UUID")
	}
	zoneID, valid := canonicalReviewUUID(*input.ZoneID)
	if !valid {
		return reviewEvent{}, nil, invalidReviewEvent("zone_id must be a UUID")
	}
	if tenantID != d.tenantID || siteID != d.siteID {
		return reviewEvent{}, nil, ErrReviewEventScope
	}
	eventType := strings.ToLower(strings.TrimSpace(*input.EventType))
	if _, ok := reviewEventTypes[eventType]; !ok {
		return reviewEvent{}, nil, invalidReviewEvent("event_type is not an approved non-biometric review type")
	}
	dedupeKey, ok := boundedReviewText(*input.DedupeKey, 256)
	if !ok || dedupeKey != eventType+":"+eventID {
		return reviewEvent{}, nil, invalidReviewEvent("dedupe_key must be the opaque canonical event key")
	}
	modelVersion, ok := boundedReviewText(*input.ModelVersion, 128)
	if !ok {
		return reviewEvent{}, nil, invalidReviewEvent("model_version must be bounded metadata")
	}
	if input.OccurredAt.IsZero() || math.IsNaN(*input.Confidence) || math.IsInf(*input.Confidence, 0) || *input.Confidence < 0 || *input.Confidence > 1 {
		return reviewEvent{}, nil, invalidReviewEvent("occurrence time and confidence are invalid")
	}
	if !*input.RequiresHumanReview || strings.TrimSpace(*input.ReviewState) != "pending" {
		return reviewEvent{}, nil, invalidReviewEvent("event must remain pending human review")
	}
	behavior := optionalReviewText(input.ObservedBehavior)
	subjectClass := strings.ToLower(optionalReviewText(input.SubjectClass))
	if eventType == "vehicle_activity" {
		if behavior != "detected" {
			return reviewEvent{}, nil, invalidReviewEvent("vehicle activity behavior must be detected")
		}
		if _, ok := vehicleReviewClasses[subjectClass]; !ok {
			return reviewEvent{}, nil, invalidReviewEvent("vehicle activity class is invalid")
		}
	} else if behavior != "" || subjectClass != "" {
		return reviewEvent{}, nil, invalidReviewEvent("vehicle metadata is forbidden for this event type")
	}
	refs := make([]string, len(*input.EvidenceRefs))
	copy(refs, *input.EvidenceRefs)
	if len(refs) > 32 {
		return reviewEvent{}, nil, invalidReviewEvent("too many evidence references")
	}
	for index, ref := range refs {
		refs[index], ok = boundedReviewText(ref, 1024)
		if !ok {
			return reviewEvent{}, nil, invalidReviewEvent("evidence reference must be bounded metadata")
		}
	}
	event := reviewEvent{
		EventID:             eventID,
		TenantID:            tenantID,
		DedupeKey:           dedupeKey,
		OccurredAt:          input.OccurredAt.UTC(),
		SiteID:              siteID,
		CameraID:            cameraID,
		ZoneID:              zoneID,
		EventType:           eventType,
		ModelVersion:        modelVersion,
		Confidence:          *input.Confidence,
		EvidenceRefs:        refs,
		RequiresHumanReview: true,
		ReviewState:         "pending",
		ObservedBehavior:    behavior,
		SubjectClass:        subjectClass,
	}
	canonical, err := json.Marshal(event)
	if err != nil {
		return reviewEvent{}, nil, invalidReviewEvent("event cannot be encoded")
	}
	return event, canonical, nil
}

func decodeReviewEvent(payload []byte) (reviewEventInput, error) {
	if len(payload) == 0 || len(payload) > maxReviewEventPayloadBytes {
		return reviewEventInput{}, invalidReviewEvent("payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var input reviewEventInput
	if err := decoder.Decode(&input); err != nil {
		return reviewEventInput{}, invalidReviewEvent("payload is not canonical event JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return reviewEventInput{}, invalidReviewEvent("payload must contain exactly one event")
	}
	if input.EventID == nil || input.TenantID == nil || input.DedupeKey == nil || input.OccurredAt == nil ||
		input.SiteID == nil || input.CameraID == nil || input.ZoneID == nil || input.EventType == nil ||
		input.ModelVersion == nil || input.Confidence == nil || input.EvidenceRefs == nil ||
		input.RequiresHumanReview == nil || input.ReviewState == nil {
		return reviewEventInput{}, invalidReviewEvent("required event fields are missing")
	}
	return input, nil
}

func canonicalReviewUUID(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return "", false
	}
	return value, true
}

func boundedReviewText(value string, maximum int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false
	}
	return value, true
}

func optionalReviewText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func invalidReviewEvent(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidReviewEvent, detail)
}
