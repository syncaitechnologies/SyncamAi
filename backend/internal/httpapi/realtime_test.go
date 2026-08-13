package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
)

const (
	httpTenantID = "11111111-1111-4111-8111-111111111111"
	httpSiteID   = "22222222-2222-4222-8222-222222222222"
	httpAlertID  = "33333333-3333-4333-8333-333333333333"
)

type fakeRealtimeRepository struct {
	mu       sync.Mutex
	current  int64
	messages []realtime.Message
	gap      bool
}

func (r *fakeRealtimeRepository) Current(_ context.Context, tenantID, siteID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current, nil
}

func (r *fakeRealtimeRepository) Replay(_ context.Context, tenantID, siteID string, after int64) (realtime.Replay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := realtime.Replay{CurrentSequence: r.current, Messages: make([]realtime.Message, 0), Gap: r.gap}
	for _, message := range r.messages {
		if message.TenantID == tenantID && message.SiteID == siteID && message.Sequence > after {
			result.Messages = append(result.Messages, message)
		}
	}
	return result, nil
}

type alertRepositoryStub struct {
	alert     alerting.Alert
	getErr    error
	ackResult alerting.AcknowledgeResult
	ackErr    error
}

func (r *alertRepositoryStub) List(context.Context, string) ([]alerting.Alert, error) {
	return []alerting.Alert{r.alert}, r.getErr
}

func (r *alertRepositoryStub) ListSite(context.Context, string, string) ([]alerting.Alert, error) {
	return []alerting.Alert{r.alert}, r.getErr
}

func (r *alertRepositoryStub) Get(context.Context, string, string) (alerting.Alert, error) {
	return r.alert, r.getErr
}

func (r *alertRepositoryStub) Acknowledge(context.Context, alerting.AcknowledgeCommand) (alerting.AcknowledgeResult, error) {
	return r.ackResult, r.ackErr
}

type failingTicketStore struct{ err error }

func (s failingTicketStore) Issue(context.Context, realtime.TicketClaims) (string, time.Time, error) {
	return "", time.Time{}, s.err
}

func (s failingTicketStore) Consume(context.Context, string) (realtime.TicketClaims, error) {
	return realtime.TicketClaims{}, s.err
}

func (r *fakeRealtimeRepository) append(message realtime.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, message)
	if message.Sequence > r.current {
		r.current = message.Sequence
	}
}

func operatorPrincipal() identity.Principal {
	return identity.Principal{
		UserID: "operator-1", TenantID: httpTenantID, SiteIDs: []string{httpSiteID},
		Roles:  []identity.Role{identity.RoleOperator},
		Scopes: []string{"alerts:read", "alerts:write"}, DataClasses: []string{"metadata"}, MFALevel: "password",
	}
}

func TestAcknowledgeAlertRequiresOperatorScopeAndTransitions(t *testing.T) {
	repository := &alerting.MemoryRepository{Alerts: []alerting.Alert{{
		ID: httpAlertID, TenantID: httpTenantID, SiteID: httpSiteID, Status: "unacknowledged",
	}}}
	handler := NewWithAlerts(fakeVerifier{principal: operatorPrincipal()}, leakyRepository{}, nil, repository)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/alerts/"+httpAlertID+"/acknowledge", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set(tenantHeader, httpTenantID)
	request.Header.Set(idempotencyHeader, "ack-1")
	request.Header.Set(correlationHeader, "44444444-4444-4444-8444-444444444444")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"acknowledged"`) {
		t.Fatalf("acknowledgment failed: %d %s", recorder.Code, recorder.Body.String())
	}

	viewerPrincipal := operatorPrincipal()
	viewerPrincipal.Roles = []identity.Role{identity.RoleViewer}
	viewerPrincipal.Scopes = []string{"alerts:read"}
	handler = NewWithAlerts(fakeVerifier{principal: viewerPrincipal}, leakyRepository{}, nil, repository)
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/v1/alerts/"+httpAlertID+"/acknowledge", nil)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set(tenantHeader, httpTenantID)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("viewer acknowledgment must be denied: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAcknowledgeAlertMapsValidationAndRepositoryFailures(t *testing.T) {
	principal := operatorPrincipal()
	baseAlert := alerting.Alert{ID: httpAlertID, TenantID: httpTenantID, SiteID: httpSiteID, Status: "unacknowledged"}
	tests := []struct {
		name        string
		tenant      string
		pathID      string
		key         string
		correlation string
		repository  alerting.Repository
		want        int
	}{
		{"missing tenant", "", httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert}, http.StatusUnprocessableEntity},
		{"cross tenant", "99999999-9999-4999-8999-999999999999", httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert}, http.StatusNotFound},
		{"missing repository", httpTenantID, httpAlertID, "ack", "", nil, http.StatusServiceUnavailable},
		{"invalid alert", httpTenantID, "bad", "ack", "", &alertRepositoryStub{alert: baseAlert}, http.StatusNotFound},
		{"not found", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, getErr: alerting.ErrAlertNotFound}, http.StatusNotFound},
		{"get unavailable", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, getErr: context.Canceled}, http.StatusServiceUnavailable},
		{"missing idempotency", httpTenantID, httpAlertID, "", "", &alertRepositoryStub{alert: baseAlert}, http.StatusUnprocessableEntity},
		{"bad correlation", httpTenantID, httpAlertID, "ack", "bad", &alertRepositoryStub{alert: baseAlert}, http.StatusUnprocessableEntity},
		{"state conflict", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, ackErr: alerting.ErrAlertStateConflict}, http.StatusConflict},
		{"idempotency conflict", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, ackErr: alerting.ErrIdempotencyConflict}, http.StatusConflict},
		{"lost alert", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, ackErr: alerting.ErrAlertNotFound}, http.StatusNotFound},
		{"write unavailable", httpTenantID, httpAlertID, "ack", "", &alertRepositoryStub{alert: baseAlert, ackErr: context.Canceled}, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewWithAlerts(fakeVerifier{principal: principal}, leakyRepository{}, nil, test.repository)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/alerts/"+test.pathID+"/acknowledge", nil)
			request.Header.Set("Authorization", "Bearer valid")
			if test.tenant != "" {
				request.Header.Set(tenantHeader, test.tenant)
			}
			if test.key != "" {
				request.Header.Set(idempotencyHeader, test.key)
			}
			if test.correlation != "" {
				request.Header.Set(correlationHeader, test.correlation)
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("want %d, got %d: %s", test.want, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestRealtimeTicketSnapshotEventAndSingleUse(t *testing.T) {
	alerts := &alerting.MemoryRepository{Alerts: []alerting.Alert{
		{ID: httpAlertID, TenantID: httpTenantID, SiteID: httpSiteID, Status: "unacknowledged"},
		{ID: "other-site", TenantID: httpTenantID, SiteID: "55555555-5555-4555-8555-555555555555", Status: "unacknowledged"},
	}}
	stream := &fakeRealtimeRepository{}
	tickets := realtime.NewMemoryTicketStore()
	handler := NewWithRealtime(fakeVerifier{principal: operatorPrincipal()}, leakyRepository{}, nil, alerts, stream, tickets)
	server := httptest.NewServer(handler)
	defer server.Close()

	ticketRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/auth/ws-ticket", strings.NewReader(`{"site_id":"`+httpSiteID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	ticketRequest.Header.Set("Authorization", "Bearer valid")
	ticketRequest.Header.Set(tenantHeader, httpTenantID)
	ticketResponse, err := http.DefaultClient.Do(ticketRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer ticketResponse.Body.Close()
	var issued struct {
		Data struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.NewDecoder(ticketResponse.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if ticketResponse.StatusCode != http.StatusCreated || issued.Data.Ticket == "" || ticketResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("ticket issue failed: %d %+v", ticketResponse.StatusCode, issued)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/v1/alerts"
	connection, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{realtimeProtocol, "ticket." + issued.Data.Ticket}})
	if err != nil {
		if response != nil {
			t.Fatalf("websocket dial failed: %d %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	defer connection.Close(websocket.StatusNormalClosure, "test complete")
	if connection.Subprotocol() != realtimeProtocol {
		t.Fatalf("unexpected protocol %q", connection.Subprotocol())
	}
	if err := wsjson.Write(ctx, connection, clientCommand{Type: "subscribe", Topic: "alerts.*", Seq: 0}); err != nil {
		t.Fatal(err)
	}
	var snapshot realtimeEnvelope
	if err := wsjson.Read(ctx, connection, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshotJSON, _ := json.Marshal(snapshot.Payload)
	if snapshot.Type != "snapshot" || !strings.Contains(string(snapshotJSON), httpAlertID) || strings.Contains(string(snapshotJSON), "other-site") {
		t.Fatalf("snapshot isolation failed: %+v", snapshot)
	}
	stream.append(realtime.Message{TenantID: httpTenantID, SiteID: httpSiteID, Sequence: 1, Topic: realtime.TopicAlertState, Payload: json.RawMessage(`{"alert":{"id":"` + httpAlertID + `","status":"acknowledged"}}`), Created: time.Now().UTC()})
	var event realtimeEnvelope
	if err := wsjson.Read(ctx, connection, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "event" || event.Topic != realtime.TopicAlertState || event.Seq != 1 {
		t.Fatalf("unexpected realtime event: %+v", event)
	}

	replayed, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{realtimeProtocol, "ticket." + issued.Data.Ticket}})
	if replayed != nil {
		replayed.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket replay accepted: response=%v err=%v", response, err)
	}
}

func TestRealtimeTicketValidationAndFailClosedDependencies(t *testing.T) {
	principal := operatorPrincipal()
	stream := &fakeRealtimeRepository{}
	alerts := &alerting.MemoryRepository{}
	tests := []struct {
		name    string
		tenant  string
		body    string
		alerts  alerting.Repository
		stream  realtime.Repository
		tickets realtime.TicketStore
		want    int
	}{
		{"missing tenant", "", `{"site_id":"` + httpSiteID + `"}`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusUnprocessableEntity},
		{"cross tenant", "99999999-9999-4999-8999-999999999999", `{"site_id":"` + httpSiteID + `"}`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusNotFound},
		{"missing dependency", httpTenantID, `{"site_id":"` + httpSiteID + `"}`, alerts, nil, realtime.NewMemoryTicketStore(), http.StatusServiceUnavailable},
		{"bad json", httpTenantID, `{`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusUnprocessableEntity},
		{"two objects", httpTenantID, `{"site_id":"` + httpSiteID + `"}{}`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusUnprocessableEntity},
		{"invalid site", httpTenantID, `{"site_id":"bad"}`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusUnprocessableEntity},
		{"unauthorized site", httpTenantID, `{"site_id":"55555555-5555-4555-8555-555555555555"}`, alerts, stream, realtime.NewMemoryTicketStore(), http.StatusForbidden},
		{"ticket unavailable", httpTenantID, `{"site_id":"` + httpSiteID + `"}`, alerts, stream, failingTicketStore{err: context.Canceled}, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewWithRealtime(fakeVerifier{principal: principal}, leakyRepository{}, nil, test.alerts, test.stream, test.tickets)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/ws-ticket", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer valid")
			if test.tenant != "" {
				request.Header.Set(tenantHeader, test.tenant)
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("want %d, got %d: %s", test.want, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestRealtimeResumePingGapAndMissingTicket(t *testing.T) {
	principal := operatorPrincipal()
	alerts := &alerting.MemoryRepository{}
	created := time.Now().UTC()
	stream := &fakeRealtimeRepository{current: 2, messages: []realtime.Message{
		{TenantID: httpTenantID, SiteID: httpSiteID, Sequence: 1, Topic: realtime.TopicAlertCreated, Payload: json.RawMessage(`{"alert":{"id":"one"}}`), Created: created},
		{TenantID: httpTenantID, SiteID: httpSiteID, Sequence: 2, Topic: realtime.TopicAlertState, Payload: json.RawMessage(`{"alert":{"id":"one"}}`), Created: created.Add(time.Second)},
	}}
	tickets := realtime.NewMemoryTicketStore()
	handler := NewWithRealtime(fakeVerifier{principal: principal}, leakyRepository{}, nil, alerts, stream, tickets)
	server := httptest.NewServer(handler)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/v1/alerts"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if connection, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{realtimeProtocol}}); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		if connection != nil {
			connection.CloseNow()
		}
		t.Fatalf("missing ticket accepted: response=%v err=%v", response, err)
	}
	ticket, _, err := tickets.Issue(ctx, realtime.TicketClaims{TenantID: httpTenantID, SiteID: httpSiteID, UserID: principal.UserID})
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{realtimeProtocol, "ticket." + ticket}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err := wsjson.Write(ctx, connection, clientCommand{Type: "resume", LastSeq: 0}); err != nil {
		t.Fatal(err)
	}
	for sequence := int64(1); sequence <= 2; sequence++ {
		var event realtimeEnvelope
		if err := wsjson.Read(ctx, connection, &event); err != nil || event.Seq != sequence || event.Type != "event" {
			t.Fatalf("resume event %d failed: %+v %v", sequence, event, err)
		}
	}
	if err := wsjson.Write(ctx, connection, clientCommand{Type: "ping"}); err != nil {
		t.Fatal(err)
	}
	var pong realtimeEnvelope
	if err := wsjson.Read(ctx, connection, &pong); err != nil || pong.Type != "pong" || pong.Seq != 2 {
		t.Fatalf("pong failed: %+v %v", pong, err)
	}
	if err := wsjson.Write(ctx, connection, clientCommand{Type: "unsubscribe"}); err != nil {
		t.Fatal(err)
	}

	gapStream := &fakeRealtimeRepository{current: 9, gap: true}
	gapTickets := realtime.NewMemoryTicketStore()
	gapServer := httptest.NewServer(NewWithRealtime(fakeVerifier{principal: principal}, leakyRepository{}, nil, alerts, gapStream, gapTickets))
	defer gapServer.Close()
	gapTicket, _, _ := gapTickets.Issue(ctx, realtime.TicketClaims{TenantID: httpTenantID, SiteID: httpSiteID, UserID: principal.UserID})
	gapConnection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(gapServer.URL, "http")+"/ws/v1/alerts", &websocket.DialOptions{Subprotocols: []string{realtimeProtocol, "ticket." + gapTicket}})
	if err != nil {
		t.Fatal(err)
	}
	defer gapConnection.CloseNow()
	if err := wsjson.Write(ctx, gapConnection, clientCommand{Type: "resume", LastSeq: 1}); err != nil {
		t.Fatal(err)
	}
	var gap realtimeEnvelope
	if err := wsjson.Read(ctx, gapConnection, &gap); err != nil || gap.Type != "gap" || gap.Seq != 9 {
		t.Fatalf("gap failed: %+v %v", gap, err)
	}
}

func TestWebsocketTicketParserDoesNotAcceptBearerProtocols(t *testing.T) {
	if ticket := websocketTicket([]string{"syncam.realtime.v1, bearer.secret"}); ticket != "" {
		t.Fatalf("unexpected ticket %q", ticket)
	}
	if ticket := websocketTicket([]string{"syncam.realtime.v1", "ticket.once"}); ticket != "once" {
		t.Fatalf("ticket parse failed: %q", ticket)
	}
}
