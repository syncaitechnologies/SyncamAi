// Package features decides which AI features run for each workspace
// (ADR-019).
//
// Two layers decide it. The platform owner sets which features a workspace's
// plan includes, and later bills from that. The workspace's own admins
// switch included features on or off, for all cameras or chosen ones. A
// feature runs only when both allow it, and the site box can run only what
// it has loaded.
package features

// Key names one feature on the wire and in the database.
type Key string

const (
	ZoneAlerts   Key = "zone_alerts"
	Loitering    Key = "loitering"
	Attendance   Key = "attendance"
	Weapon       Key = "weapon"
	LiveView     Key = "live_view"
	CameraHealth Key = "camera_health"
	FireSmoke    Key = "fire_smoke"
	Fall         Key = "fall"
	Fight        Key = "fight"
	PPE          Key = "ppe"
	NumberPlates Key = "number_plates"
)

// Status says how far along a feature is.
type Status string

const (
	// Available features work today and every plan includes them by default.
	StatusAvailable Status = "available"
	// Preview features work but are not ready for every client, so no plan
	// includes them unless the owner adds them.
	StatusPreview Status = "preview"
	// AlwaysOn features are part of every plan and cannot be switched off.
	StatusAlwaysOn Status = "always_on"
	// ComingSoon features are shown so clients can say they are interested.
	StatusComingSoon Status = "coming_soon"
)

// Definition describes one feature.
type Definition struct {
	Key        Key      `json:"key"`
	Status     Status   `json:"status"`
	EventTypes []string `json:"event_types,omitempty"`
	// PerCamera features can be limited to chosen cameras.
	PerCamera bool `json:"per_camera"`
	// NeedsBox features only run where a site box has loaded them.
	NeedsBox bool `json:"needs_box"`
}

var catalog = []Definition{
	{Key: ZoneAlerts, Status: StatusAvailable, EventTypes: []string{"intrusion", "restricted_zone", "tripwire"}, PerCamera: true, NeedsBox: true},
	{Key: Loitering, Status: StatusAvailable, EventTypes: []string{"loitering"}, PerCamera: true, NeedsBox: true},
	{Key: Attendance, Status: StatusAvailable, PerCamera: true, NeedsBox: true},
	{Key: Weapon, Status: StatusPreview, EventTypes: []string{"weapon_review"}, PerCamera: true, NeedsBox: true},
	{Key: LiveView, Status: StatusAvailable, NeedsBox: true},
	{Key: CameraHealth, Status: StatusAlwaysOn, EventTypes: []string{"camera_health"}},
	{Key: FireSmoke, Status: StatusComingSoon},
	{Key: Fall, Status: StatusComingSoon},
	{Key: Fight, Status: StatusComingSoon},
	{Key: PPE, Status: StatusComingSoon},
	{Key: NumberPlates, Status: StatusComingSoon},
}

// Catalog returns every feature in display order.
func Catalog() []Definition {
	result := make([]Definition, len(catalog))
	copy(result, catalog)
	return result
}

// Lookup returns one feature's definition.
func Lookup(key Key) (Definition, bool) {
	for _, definition := range catalog {
		if definition.Key == key {
			return definition, true
		}
	}
	return Definition{}, false
}

// ForEventType returns the feature that produces an event type. Event types
// that belong to no feature are never filtered.
func ForEventType(eventType string) (Key, bool) {
	for _, definition := range catalog {
		for _, candidate := range definition.EventTypes {
			if candidate == eventType {
				return definition.Key, true
			}
		}
	}
	return "", false
}

// PlanControlled reports whether the owner decides per workspace if a plan
// includes the feature (identity.tenant_features). Live view is decided by
// its minute allowance instead; always-on and coming-soon features by status.
func PlanControlled(key Key) bool {
	switch key {
	case ZoneAlerts, Loitering, Attendance, Weapon:
		return true
	}
	return false
}

// Switchable reports whether a workspace can switch the feature on or off
// (config.feature_settings).
func Switchable(key Key) bool {
	return PlanControlled(key) || key == LiveView
}

// DefaultIncluded is whether a plan includes a feature when the owner has not
// said otherwise: available features are in every plan (trials too, so they
// show their full value), previews and coming-soon features in none.
func DefaultIncluded(key Key) bool {
	definition, ok := Lookup(key)
	return ok && (definition.Status == StatusAvailable || definition.Status == StatusAlwaysOn)
}
