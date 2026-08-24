package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/privacymasks"
)

const privacyReleaseStatusBodyLimit = 1024

type privacyReleaseStatusRequest struct {
	ReleaseID string `json:"release_id"`
	Version   int64  `json:"version"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (s *Server) pullPrivacyMaskRelease(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.verifiedDeviceRequest(w, r)
	if !ok {
		return
	}
	if s.privacyReleaseTransport == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy release transport unavailable.")
		return
	}
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_version")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "after_version must be a non-negative integer.")
			return
		}
		after = value
	}
	result, err := s.privacyReleaseTransport.Pull(r.Context(), deviceID, after)
	if errors.Is(err, privacymasks.ErrReleaseDeviceNotFound) {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy release transport unavailable.")
		return
	}
	if result.Manifest == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result.Manifest})
}

func (s *Server) reportPrivacyMaskRelease(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.verifiedDeviceRequest(w, r)
	if !ok {
		return
	}
	if s.privacyReleaseTransport == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy release transport unavailable.")
		return
	}
	if _, ok := correlationHeaderOnly(w, r); !ok {
		return
	}
	var input privacyReleaseStatusRequest
	if !decodeBodyLimit(w, r, &input, privacyReleaseStatusBodyLimit) {
		return
	}
	input.ReleaseID, input.State, input.ErrorCode = strings.TrimSpace(input.ReleaseID), strings.TrimSpace(input.State), strings.TrimSpace(input.ErrorCode)
	status, err := s.privacyReleaseTransport.Report(r.Context(), privacymasks.ReportReleaseCommand{DeviceID: deviceID, ReleaseID: input.ReleaseID, Version: input.Version, State: input.State, ErrorCode: input.ErrorCode})
	if errors.Is(err, privacymasks.ErrReleaseDeviceNotFound) {
		writeError(w, http.StatusUnauthorized, "DEVICE_AUTH_REQUIRED", "Verified device authentication required.")
		return
	}
	if errors.Is(err, privacymasks.ErrInvalidReleaseStatus) {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Privacy release status fields are invalid.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Privacy release transport unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}
