package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

var zoneKinds = map[string]bool{"intrusion": true, "restricted_zone": true, "loitering": true, "abandoned": true, "tripwire": true}

type createZoneRequest struct {
	SiteID         string          `json:"site_id"`
	CameraID       string          `json:"camera_id"`
	Floor          string          `json:"floor"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Geometry       json.RawMessage `json:"geometry"`
	Enabled        *bool           `json:"enabled"`
	LoiterSeconds  *int            `json:"loiter_seconds"`
	SubjectClasses []string        `json:"subject_classes"`
}
type updateZoneRequest struct {
	ConfigVersion  int64            `json:"config_version"`
	Name           *string          `json:"name"`
	Floor          *string          `json:"floor"`
	Geometry       *json.RawMessage `json:"geometry"`
	Enabled        *bool            `json:"enabled"`
	LoiterSeconds  *int             `json:"loiter_seconds"`
	SubjectClasses *[]string        `json:"subject_classes"`
}

func (s *Server) listZones(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.zoneRequestContext(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	siteID := strings.TrimSpace(r.URL.Query().Get("site_id"))
	if siteID != "" {
		if _, err := uuid.Parse(siteID); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "site_id must be a UUID.")
			return
		}
		if !authz.CanAccessSite(principal, tenantID, siteID) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return
		}
	}
	listed, err := s.zones.List(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return
	}
	visible := make([]zones.Zone, 0, len(listed))
	for _, zone := range listed {
		if authz.CanAccessSite(principal, tenantID, zone.SiteID) {
			visible = append(visible, zone)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": visible, "meta": map[string]any{"count": len(visible), "next": nil}})
}

func (s *Server) getZone(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.zoneRequestContext(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	zone, ok := s.authorizedZone(w, r, principal, tenantID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": zone})
}

func (s *Server) createZone(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.zoneRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	idempotencyKey, requestID, ok := mutationHeaders(w, r)
	if !ok {
		return
	}
	var input createZoneRequest
	if !decodeBody(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	if !validCreateZone(input) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Zone fields or GeoJSON geometry are invalid.")
		return
	}
	if !authz.CanAccessSite(principal, tenantID, input.SiteID) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	result, err := s.zones.Create(r.Context(), zones.CreateCommand{TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, IdempotencyKey: idempotencyKey, SiteID: strings.TrimSpace(input.SiteID), CameraID: strings.TrimSpace(input.CameraID), Floor: strings.TrimSpace(input.Floor), Name: strings.TrimSpace(input.Name), Kind: strings.TrimSpace(input.Kind), Geometry: input.Geometry, Enabled: *input.Enabled, LoiterSeconds: input.LoiterSeconds, SubjectClasses: input.SubjectClasses})
	if errors.Is(err, zones.ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, "IDEMPOTENCY_REPLAY", "Idempotency-Key was already used for a different request.")
		return
	}
	if errors.Is(err, zones.ErrSiteNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": result.Zone})
}

func (s *Server) updateZone(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.zoneRequestContext(w, r, authz.CapabilityConfigWrite)
	if !ok {
		return
	}
	zone, ok := s.authorizedZone(w, r, principal, tenantID)
	if !ok {
		return
	}
	requestID, ok := correlationHeaderOnly(w, r)
	if !ok {
		return
	}
	var input updateZoneRequest
	if !decodeBody(w, r, &input) {
		return
	}
	if !validUpdateZone(&input, zone.Kind) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Zone update fields or GeoJSON geometry are invalid.")
		return
	}
	updated, err := s.zones.Update(r.Context(), zones.UpdateCommand{TenantID: tenantID, ActorID: principal.UserID, RequestID: requestID, ZoneID: zone.ID, ExpectedVersion: input.ConfigVersion, Name: input.Name, Floor: input.Floor, Geometry: input.Geometry, Enabled: input.Enabled, LoiterSeconds: input.LoiterSeconds, SubjectClasses: input.SubjectClasses})
	if errors.Is(err, zones.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if errors.Is(err, zones.ErrVersionConflict) {
		writeError(w, http.StatusConflict, "RESOURCE_CONFLICT", "Zone configuration version conflicts with this update.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (s *Server) zoneRequestContext(w http.ResponseWriter, r *http.Request, capability authz.Capability) (identityPrincipal, string, bool) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return identityPrincipal{}, "", false
	}
	tenantID, err := requestTenant(r, principal)
	if errors.Is(err, authz.ErrDenied) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return identityPrincipal{}, "", false
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Tenant header is required.")
		return identityPrincipal{}, "", false
	}
	if err := authz.Authorize(principal, authz.Request{Capability: capability, TenantID: tenantID}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return identityPrincipal{}, "", false
	}
	if s.zones == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return identityPrincipal{}, "", false
	}
	return principal, tenantID, true
}

func (s *Server) authorizedZone(w http.ResponseWriter, r *http.Request, principal identityPrincipal, tenantID string) (zones.Zone, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return zones.Zone{}, false
	}
	zone, err := s.zones.Get(r.Context(), tenantID, id)
	if errors.Is(err, zones.ErrNotFound) || (err == nil && !authz.CanAccessSite(principal, tenantID, zone.SiteID)) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return zones.Zone{}, false
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Zone repository unavailable.")
		return zones.Zone{}, false
	}
	return zone, true
}

func validCreateZone(input createZoneRequest) bool {
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.CameraID = strings.TrimSpace(input.CameraID)
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	if _, err := uuid.Parse(input.SiteID); err != nil || len(input.Name) == 0 || len(input.Name) > 120 || len(input.Floor) > 120 || !zoneKinds[input.Kind] || (input.CameraID != "" && !validUUID(input.CameraID)) || !validLoiterSeconds(input.Kind, input.LoiterSeconds) || !validSubjectClasses(input.SubjectClasses) {
		return false
	}
	return validGeometry(input.Geometry, input.Kind)
}
func validUpdateZone(input *updateZoneRequest, kind string) bool {
	if input.ConfigVersion < 1 || (input.Name == nil && input.Floor == nil && input.Geometry == nil && input.Enabled == nil && input.LoiterSeconds == nil && input.SubjectClasses == nil) {
		return false
	}
	if input.Name != nil {
		*input.Name = strings.TrimSpace(*input.Name)
		if *input.Name == "" || len(*input.Name) > 120 {
			return false
		}
	}
	if input.Floor != nil {
		*input.Floor = strings.TrimSpace(*input.Floor)
		if len(*input.Floor) > 120 {
			return false
		}
	}
	return validLoiterSeconds(kind, input.LoiterSeconds) && (input.SubjectClasses == nil || validSubjectClasses(*input.SubjectClasses)) && (input.Geometry == nil || validGeometry(*input.Geometry, kind))
}
func validLoiterSeconds(kind string, value *int) bool {
	if kind != "loitering" {
		return value == nil
	}
	if value == nil {
		return true
	}
	return *value >= zones.MinimumLoiterSeconds && *value <= zones.MaximumLoiterSeconds
}
func validSubjectClasses(values []string) bool {
	_, err := zones.NormalizeSubjectClasses(values)
	return err == nil
}
func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func validGeometry(raw json.RawMessage, kind string) bool {
	var shape struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &shape) != nil {
		return false
	}
	requiredType := "Polygon"
	if kind == "tripwire" {
		requiredType = "LineString"
	}
	if shape.Type != requiredType {
		return false
	}
	var points [][]float64
	if requiredType == "Polygon" {
		var rings [][][]float64
		if json.Unmarshal(shape.Coordinates, &rings) != nil || len(rings) != 1 {
			return false
		}
		points = rings[0]
		if len(points) < 4 || !samePoint(points[0], points[len(points)-1]) {
			return false
		}
	} else if json.Unmarshal(shape.Coordinates, &points) != nil || len(points) < 2 {
		return false
	}
	for _, point := range points {
		if len(point) != 2 || math.IsNaN(point[0]) || math.IsNaN(point[1]) || math.IsInf(point[0], 0) || math.IsInf(point[1], 0) || math.Abs(point[0]) > 1000000 || math.Abs(point[1]) > 1000000 {
			return false
		}
	}
	return true
}
func samePoint(a, b []float64) bool {
	return len(a) == 2 && len(b) == 2 && a[0] == b[0] && a[1] == b[1]
}
