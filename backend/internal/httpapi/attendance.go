package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/attendance"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
)

func (s *Server) registerAttendanceRoutes() {
	s.mux.Handle("POST /v1/site-ingest/attendance", s.authenticateSite(http.HandlerFunc(s.recordAttendance)))
	s.mux.Handle("GET /v1/staff", s.authenticate(http.HandlerFunc(s.listStaff)))
	s.mux.Handle("POST /v1/staff", s.authenticate(http.HandlerFunc(s.createStaff)))
	s.mux.Handle("PATCH /v1/staff/{id}", s.authenticate(http.HandlerFunc(s.updateStaff)))
	s.mux.Handle("GET /v1/attendance", s.authenticate(http.HandlerFunc(s.listAttendance)))
}

func (s *Server) attendanceAvailable(w http.ResponseWriter) bool {
	if s.siteIngest.Attendance == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return false
	}
	return true
}

type attendanceSightingRequest struct {
	ID         string    `json:"id"`
	StaffID    string    `json:"staff_id"`
	CameraID   string    `json:"camera_id"`
	OccurredAt time.Time `json:"occurred_at"`
	Confidence float64   `json:"confidence"`
}

func (s *Server) recordAttendance(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if !s.attendanceAvailable(w) {
		return
	}
	var input attendanceSightingRequest
	if !decodeBodyLimit(w, r, &input, 4<<10) {
		return
	}
	now := time.Now().UTC()
	if !validUUID(input.ID) || !validUUID(input.StaffID) || !validUUID(input.CameraID) || input.OccurredAt.IsZero() ||
		input.OccurredAt.After(now.Add(5*time.Minute)) || input.OccurredAt.Before(now.Add(-48*time.Hour)) ||
		math.IsNaN(input.Confidence) || input.Confidence < 0 || input.Confidence > 1 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Attendance sighting is invalid.")
		return
	}
	created, err := s.siteIngest.Attendance.RecordSighting(r.Context(), attendance.Record{
		ID: input.ID, TenantID: scope.TenantID, SiteID: scope.SiteID, StaffID: input.StaffID,
		CameraID: input.CameraID, OccurredAt: input.OccurredAt.UTC(), Confidence: input.Confidence,
	})
	if errors.Is(err, attendance.ErrStaffNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Staff member is not active at this site.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"id": input.ID, "created": created}})
}

func (s *Server) listStaff(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok || !s.attendanceAvailable(w) {
		return
	}
	siteID, ok := siteFromQuery(w, r, principal, tenantID, true)
	if !ok {
		return
	}
	staff, err := s.siteIngest.Attendance.ListStaff(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": staff, "meta": map[string]any{"count": len(staff), "next": nil}})
}

type createStaffRequest struct {
	SiteID           string `json:"site_id"`
	DisplayName      string `json:"display_name"`
	EmployeeCode     string `json:"employee_code"`
	ConsentConfirmed bool   `json:"consent_confirmed"`
}

func (s *Server) createStaff(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok || !s.attendanceAvailable(w) {
		return
	}
	var input createStaffRequest
	if !decodeBody(w, r, &input) {
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.EmployeeCode = strings.TrimSpace(input.EmployeeCode)
	if !validUUID(strings.TrimSpace(input.SiteID)) || input.DisplayName == "" || len(input.DisplayName) > 120 || len(input.EmployeeCode) > 64 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Staff fields are invalid.")
		return
	}
	if !input.ConsentConfirmed {
		writeError(w, http.StatusUnprocessableEntity, "CONSENT_REQUIRED", "Record the staff member's written consent before enrolling them for face attendance.")
		return
	}
	if err := authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: input.SiteID}); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	staff, err := s.siteIngest.Attendance.CreateStaff(r.Context(), attendance.CreateStaffCommand{
		TenantID: tenantID, SiteID: strings.TrimSpace(input.SiteID), ActorID: principal.UserID,
		DisplayName: input.DisplayName, EmployeeCode: input.EmployeeCode,
	})
	if errors.Is(err, attendance.ErrSiteNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": staff})
}

type updateStaffRequest struct {
	Active *bool `json:"active"`
}

func (s *Server) updateStaff(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigWrite)
	if !ok || !s.attendanceAvailable(w) {
		return
	}
	var input updateStaffRequest
	if !decodeBody(w, r, &input) {
		return
	}
	if input.Active == nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Only active can be changed.")
		return
	}
	staffID := r.PathValue("id")
	staff, err := s.siteIngest.Attendance.ListStaff(r.Context(), tenantID, "")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return
	}
	for _, member := range staff {
		if member.ID != staffID {
			continue
		}
		if authz.Authorize(principal, authz.Request{Capability: authz.CapabilityConfigWrite, TenantID: tenantID, SiteID: member.SiteID}) != nil {
			break
		}
		updated, err := s.siteIngest.Attendance.SetStaffActive(r.Context(), tenantID, staffID, *input.Active)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": updated})
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
}

func (s *Server) listAttendance(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok || !s.attendanceAvailable(w) {
		return
	}
	siteID, ok := siteFromQuery(w, r, principal, tenantID, true)
	if !ok {
		return
	}
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	var err error
	if raw := r.URL.Query().Get("from"); raw != "" {
		if from, err = time.Parse(time.RFC3339, raw); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "from must be RFC3339.")
			return
		}
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		if to, err = time.Parse(time.RFC3339, raw); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "to must be RFC3339.")
			return
		}
	}
	if !from.Before(to) || to.Sub(from) > 32*24*time.Hour {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "The range must be positive and at most 32 days.")
		return
	}
	records, err := s.siteIngest.Attendance.ListRecords(r.Context(), tenantID, siteID, from, to)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Attendance is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": records, "meta": map[string]any{"count": len(records), "from": from, "to": to}})
}

// activeStaff lists the enrolled staff a site box may match against.
func (s *Server) activeStaff(r *http.Request, tenantID, siteID string) []map[string]string {
	result := make([]map[string]string, 0)
	if s.siteIngest.Attendance == nil {
		return result
	}
	staff, err := s.siteIngest.Attendance.ListStaff(r.Context(), tenantID, siteID)
	if err != nil {
		return result
	}
	for _, member := range staff {
		if member.Active && member.SiteID == siteID {
			result = append(result, map[string]string{"id": member.ID, "display_name": member.DisplayName})
		}
	}
	return result
}
