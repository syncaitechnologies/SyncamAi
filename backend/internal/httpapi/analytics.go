package httpapi

import (
	"net/http"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/analytics"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
)

// Anonymous visitor and table insights (ADR-020). Site boxes send hourly
// counts for door_line and table zones; the Insights page reads a summary.
func (s *Server) registerAnalyticsRoutes() {
	s.mux.Handle("POST /v1/site-ingest/analytics", s.authenticateSite(http.HandlerFunc(s.recordAnalytics)))
	s.mux.Handle("GET /v1/analytics/insights", s.authenticate(http.HandlerFunc(s.insights)))
}

// maxAnalyticsRows bounds one upload; a box sends at most a few hours of a
// few dozen zones at a time.
const maxAnalyticsRows = 200

var analyticsZoneKinds = map[string]bool{"door_line": true, "table": true}

type analyticsUpload struct {
	Hours []analytics.ZoneHour `json:"hours"`
}

func validZoneHour(row analytics.ZoneHour, now time.Time) bool {
	hour := row.HourStart.UTC()
	// Hours start on the site's local hour: :30 UTC in India, :45 in Nepal.
	return validUUID(row.ZoneID) && validUUID(row.RunID) &&
		hour.Equal(hour.Truncate(15*time.Minute)) && !hour.After(now.Add(time.Hour)) && !hour.Before(now.Add(-48*time.Hour)) &&
		row.Entries >= 0 && row.Entries <= 100000 && row.Exits >= 0 && row.Exits <= 100000 &&
		row.OccupiedSeconds >= 0 && row.OccupiedSeconds <= 3600 && row.Sessions >= 0 && row.Sessions <= 1000 &&
		row.SessionSeconds >= 0 && row.SessionSeconds <= 86400000 && row.PeakPeople >= 0 && row.PeakPeople <= 1000
}

func (s *Server) recordAnalytics(w http.ResponseWriter, r *http.Request) {
	scope, ok := siteScopeFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	if s.siteIngest.Analytics == nil || s.zones == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Insights are unavailable.")
		return
	}
	var input analyticsUpload
	if !decodeBodyLimit(w, r, &input, 128<<10) {
		return
	}
	if len(input.Hours) == 0 || len(input.Hours) > maxAnalyticsRows {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Send between 1 and 200 hourly rows.")
		return
	}
	now := time.Now().UTC()
	checked := map[string]bool{}
	for index, row := range input.Hours {
		if !validZoneHour(row, now) {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "An hourly row is invalid.")
			return
		}
		input.Hours[index].HourStart = row.HourStart.UTC()
		if checked[row.ZoneID] {
			continue
		}
		// Counts are accepted only for this site's door counter and table zones.
		zone, err := s.zones.Get(r.Context(), scope.TenantID, row.ZoneID)
		if err != nil || zone.SiteID != scope.SiteID || !analyticsZoneKinds[zone.Kind] {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "A zone is not a door counter or table at this site.")
			return
		}
		checked[row.ZoneID] = true
	}
	if err := s.siteIngest.Analytics.SaveHours(r.Context(), scope.TenantID, scope.SiteID, input.Hours); err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Insights are unavailable.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"saved": len(input.Hours)}})
}

// insights summarises a range of whole days in the site's time zone (from and
// to are YYYY-MM-DD, both included), like the attendance daily summary.
func (s *Server) insights(w http.ResponseWriter, r *http.Request) {
	principal, tenantID, ok := s.userTenant(w, r, authz.CapabilityConfigRead)
	if !ok {
		return
	}
	if s.siteIngest.Analytics == nil || s.tenants == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Insights are unavailable.")
		return
	}
	siteID, ok := siteFromQuery(w, r, principal, tenantID, true)
	if !ok {
		return
	}
	site, err := s.findSite(r.Context(), tenantID, siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	location, err := time.LoadLocation(site.Timezone)
	if err != nil || site.Timezone == "" {
		location = time.UTC
	}
	query := r.URL.Query()
	fromDay, errFrom := time.ParseInLocation("2006-01-02", query.Get("from"), location)
	toDay, errTo := time.ParseInLocation("2006-01-02", query.Get("to"), location)
	if errFrom != nil || errTo != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "from and to must be dates (YYYY-MM-DD).")
		return
	}
	if toDay.Before(fromDay) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "The end date must be on or after the start date.")
		return
	}
	end := toDay.AddDate(0, 0, 1)
	if end.After(fromDay.AddDate(0, 0, maxAttendanceDays)) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Choose at most 92 days.")
		return
	}
	summary, err := s.siteIngest.Analytics.Summary(r.Context(), tenantID, siteID, location, fromDay, end)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Insights are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary, "meta": map[string]any{
		"from": query.Get("from"), "to": query.Get("to"), "timezone": location.String(),
	}})
}
