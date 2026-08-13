package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
)

const realtimeProtocol = "syncam.realtime.v1"

type ticketRequest struct {
	SiteID string `json:"site_id"`
}

type clientCommand struct {
	Type    string `json:"type"`
	Topic   string `json:"topic,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
	LastSeq int64  `json:"last_seq,omitempty"`
}

type realtimeEnvelope struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
	Topic   string `json:"topic,omitempty"`
	Seq     int64  `json:"seq"`
	TS      string `json:"ts"`
	Payload any    `json:"payload,omitempty"`
}

func (s *Server) issueRealtimeTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required.")
		return
	}
	tenantID, err := requestTenant(r, principal)
	if errors.Is(err, authz.ErrDenied) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Tenant header is required.")
		return
	}
	if s.tickets == nil || s.realtime == nil || s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Realtime gateway unavailable.")
		return
	}
	var input ticketRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Ticket request is invalid.")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Request body must contain one JSON object.")
		return
	}
	input.SiteID = strings.TrimSpace(input.SiteID)
	if _, err := uuid.Parse(input.SiteID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "site_id must be a UUID.")
		return
	}
	if err := authz.Authorize(principal, authz.Request{
		Capability: authz.CapabilityAlertsRead, TenantID: tenantID, SiteID: input.SiteID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Access denied.")
		return
	}
	ticket, expires, err := s.tickets.Issue(r.Context(), realtime.TicketClaims{
		TenantID: tenantID, SiteID: input.SiteID, UserID: principal.UserID,
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Realtime gateway unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"ticket": ticket, "expires_at": expires, "protocol": realtimeProtocol,
	}})
}

func (s *Server) streamAlerts(w http.ResponseWriter, r *http.Request) {
	ticket := websocketTicket(r.Header.Values("Sec-WebSocket-Protocol"))
	if ticket == "" || s.tickets == nil || s.realtime == nil || s.alerts == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "A valid realtime ticket is required.")
		return
	}
	claims, err := s.tickets.Consume(r.Context(), ticket)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "A valid realtime ticket is required.")
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{realtimeProtocol}})
	if err != nil {
		return
	}
	defer connection.Close(websocket.StatusNormalClosure, "stream closed")
	if connection.Subprotocol() != realtimeProtocol {
		_ = connection.Close(websocket.StatusPolicyViolation, "realtime protocol required")
		return
	}
	connection.SetReadLimit(64 << 10)
	ctx := r.Context()
	initialContext, cancelInitial := context.WithTimeout(ctx, 5*time.Second)
	var initial clientCommand
	err = wsjson.Read(initialContext, connection, &initial)
	cancelInitial()
	if err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "subscribe or resume required")
		return
	}
	last, ok := s.startRealtimeStream(ctx, connection, claims, initial)
	if !ok {
		return
	}
	commands := make(chan clientCommand, 21)
	readErrors := make(chan error, 1)
	go readRealtimeCommands(ctx, connection, commands, readErrors)
	poll := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(30 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	windowStart, inboundCount := time.Now(), 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-readErrors:
			return
		case command := <-commands:
			if time.Since(windowStart) >= time.Second {
				windowStart, inboundCount = time.Now(), 0
			}
			inboundCount++
			if inboundCount > 20 {
				_ = connection.Close(websocket.StatusPolicyViolation, "message rate exceeded")
				return
			}
			switch command.Type {
			case "ping":
				if err := writeRealtime(ctx, connection, realtimeEnvelope{Version: 1, Type: "pong", Seq: last, TS: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
					return
				}
			case "unsubscribe":
				return
			default:
				_ = connection.Close(websocket.StatusPolicyViolation, "unsupported command")
				return
			}
		case <-poll.C:
			next, err := s.realtime.Replay(ctx, claims.TenantID, claims.SiteID, last)
			if err != nil {
				return
			}
			if next.Gap {
				if err := writeRealtime(ctx, connection, realtimeEnvelope{Version: 1, Type: "gap", Seq: next.CurrentSequence, TS: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
					return
				}
				last = next.CurrentSequence
				continue
			}
			for _, message := range next.Messages {
				if err := writeRealtimeMessage(ctx, connection, message); err != nil {
					return
				}
				last = message.Sequence
			}
		case <-heartbeat.C:
			pingContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := connection.Ping(pingContext)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) startRealtimeStream(ctx context.Context, connection *websocket.Conn, claims realtime.TicketClaims, command clientCommand) (int64, bool) {
	switch {
	case command.Type == "subscribe" && command.Topic == "alerts.*" && command.Seq == 0:
		base, err := s.realtime.Current(ctx, claims.TenantID, claims.SiteID)
		if err != nil {
			return 0, false
		}
		queue, err := s.alerts.ListSite(ctx, claims.TenantID, claims.SiteID)
		if err != nil {
			return 0, false
		}
		visible := make([]alerting.Alert, 0)
		for _, alert := range queue {
			if alert.TenantID == claims.TenantID && alert.SiteID == claims.SiteID {
				visible = append(visible, alert)
			}
		}
		envelope := realtimeEnvelope{Version: 1, Type: "snapshot", Topic: "alerts.*", Seq: base, TS: time.Now().UTC().Format(time.RFC3339Nano), Payload: map[string]any{"alerts": visible, "base_seq": base}}
		if err := writeRealtime(ctx, connection, envelope); err != nil {
			return 0, false
		}
		return base, true
	case command.Type == "resume" && command.LastSeq >= 0:
		replay, err := s.realtime.Replay(ctx, claims.TenantID, claims.SiteID, command.LastSeq)
		if err != nil {
			return 0, false
		}
		if replay.Gap {
			if err := writeRealtime(ctx, connection, realtimeEnvelope{Version: 1, Type: "gap", Seq: replay.CurrentSequence, TS: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return 0, false
			}
			return replay.CurrentSequence, true
		}
		last := command.LastSeq
		for _, message := range replay.Messages {
			if err := writeRealtimeMessage(ctx, connection, message); err != nil {
				return 0, false
			}
			last = message.Sequence
		}
		return last, true
	default:
		_ = connection.Close(websocket.StatusPolicyViolation, "invalid subscribe or resume command")
		return 0, false
	}
}

func readRealtimeCommands(ctx context.Context, connection *websocket.Conn, commands chan<- clientCommand, failures chan<- error) {
	for {
		var command clientCommand
		if err := wsjson.Read(ctx, connection, &command); err != nil {
			failures <- err
			return
		}
		select {
		case commands <- command:
		case <-ctx.Done():
			return
		}
	}
}

func writeRealtimeMessage(ctx context.Context, connection *websocket.Conn, message realtime.Message) error {
	var payload any
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return err
	}
	return writeRealtime(ctx, connection, realtimeEnvelope{Version: 1, Type: "event", Topic: message.Topic, Seq: message.Sequence, TS: message.Created.UTC().Format(time.RFC3339Nano), Payload: payload})
}

func writeRealtime(ctx context.Context, connection *websocket.Conn, envelope realtimeEnvelope) error {
	writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(writeContext, connection, envelope)
}

func websocketTicket(headers []string) string {
	for _, header := range headers {
		for _, protocol := range strings.Split(header, ",") {
			protocol = strings.TrimSpace(protocol)
			if strings.HasPrefix(protocol, "ticket.") {
				return strings.TrimPrefix(protocol, "ticket.")
			}
		}
	}
	return ""
}
