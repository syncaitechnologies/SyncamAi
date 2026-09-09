package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
)

const consoleOrigin = "https://console.example"

func TestBrowserOriginConfiguration(t *testing.T) {
	for _, configuration := range []string{"", " ", consoleOrigin, consoleOrigin + ", https://second.example:8443", "http://localhost:5173", "http://127.0.0.1:5173"} {
		if _, err := WithBrowserOrigins(http.NotFoundHandler(), configuration); err != nil {
			t.Fatalf("valid configuration rejected: %q: %v", configuration, err)
		}
	}
	for _, configuration := range []string{"*", "null", "https://*.example", "https://con?ole.example", "https://[ab].example", "http://console.example", "https://console.example/", "https://console.example/path", "https://console.example?", "https://console.example?q=x", "https://console.example#", "https://console.example#fragment", "https://user@console.example", "https://", "https://console.example:0", "https://console.example:65536", "https://console.example:", "https://console.example:invalid", consoleOrigin + ",", "," + consoleOrigin, "https://con sole.example", "https://console.example\n.evil"} {
		t.Run(configuration, func(t *testing.T) {
			if handler, err := WithBrowserOrigins(http.NotFoundHandler(), configuration); err == nil || handler != nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}

func TestBrowserOriginRequests(t *testing.T) {
	for _, test := range []struct {
		name, config, origin, method, requestedMethod, requestedHeaders string
		want, wantCalls                                                 int
		allowOrigin                                                     bool
	}{
		{"native", "", "", "GET", "", "", 202, 1, false},
		{"empty deny", "", consoleOrigin, "GET", "", "", 403, 0, false},
		{"allowed", consoleOrigin, consoleOrigin, "GET", "", "", 202, 1, true},
		{"wrong origin", consoleOrigin, "https://evil.example", "POST", "", "", 403, 0, false},
		{"wrong scheme", consoleOrigin, "http://console.example", "GET", "", "", 403, 0, false},
		{"wrong port", consoleOrigin, consoleOrigin + ":8443", "GET", "", "", 403, 0, false},
		{"suffix", consoleOrigin, consoleOrigin + ".evil.example", "GET", "", "", 403, 0, false},
		{"opaque", consoleOrigin, "null", "GET", "", "", 403, 0, false},
		{"multiple in value", consoleOrigin, consoleOrigin + " https://evil.example", "GET", "", "", 403, 0, false},
		{"preflight", consoleOrigin, consoleOrigin, "OPTIONS", "POST", "authorization, content-type, x-sentinelvision-tenant-id, idempotency-key, x-correlation-id", 204, 0, true},
		{"patch preflight", consoleOrigin, consoleOrigin, "OPTIONS", "PATCH", "Authorization", 204, 0, true},
		{"no requested headers", consoleOrigin, consoleOrigin, "OPTIONS", "GET", "", 204, 0, true},
		{"unknown method", consoleOrigin, consoleOrigin, "OPTIONS", "TRACE", "", 403, 0, false},
		{"lowercase method", consoleOrigin, consoleOrigin, "OPTIONS", "post", "", 403, 0, false},
		{"cookie header", consoleOrigin, consoleOrigin, "OPTIONS", "POST", "Cookie", 403, 0, false},
		{"forwarded header", consoleOrigin, consoleOrigin, "OPTIONS", "GET", "X-Forwarded-Host", 403, 0, false},
		{"trailing header", consoleOrigin, consoleOrigin, "OPTIONS", "GET", "Authorization,", 403, 0, false},
		{"ordinary options", consoleOrigin, consoleOrigin, "OPTIONS", "", "", 202, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler, err := WithBrowserOrigins(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Origin") != "" && r.Context().Value(approvedBrowserOriginKey{}) != consoleOrigin {
					t.Error("validated origin was not passed to the downstream handler")
				}
				w.WriteHeader(http.StatusAccepted)
			}), test.config)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(test.method, "/v1/sites", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.requestedMethod != "" {
				request.Header.Set("Access-Control-Request-Method", test.requestedMethod)
			}
			if test.requestedHeaders != "" {
				request.Header.Set("Access-Control-Request-Headers", test.requestedHeaders)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want || calls != test.wantCalls {
				t.Fatalf("status=%d calls=%d; want %d/%d", recorder.Code, calls, test.want, test.wantCalls)
			}
			if (recorder.Header().Get("Access-Control-Allow-Origin") == test.origin && test.origin != "") != test.allowOrigin {
				t.Fatal("incorrect origin response header")
			}
			if recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("cookie credentials enabled")
			}
			vary := strings.Join(recorder.Header().Values("Vary"), ",")
			if !strings.Contains(vary, "Origin") {
				t.Fatal("missing origin cache variation")
			}
			if test.want == 204 && (!strings.Contains(vary, "Access-Control-Request-Headers") || recorder.Header().Get("Access-Control-Allow-Headers") != browserHeaders || recorder.Header().Get("Access-Control-Allow-Methods") != browserMethods) {
				t.Fatal("incomplete preflight response")
			}
		})
	}
}

func TestBrowserOriginDoesNotBypassAuthenticationOrTenantIsolation(t *testing.T) {
	handler, err := WithBrowserOrigins(New(fakeVerifier{principal: operatorPrincipal()}, leakyRepository{}), consoleOrigin)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		token, tenant string
		want          int
	}{
		{"", httpTenantID, http.StatusUnauthorized},
		{"valid", "99999999-9999-4999-8999-999999999999", http.StatusNotFound},
	} {
		request := httptest.NewRequest(http.MethodGet, "/v1/sites", nil)
		request.Header.Set("Origin", consoleOrigin)
		request.Header.Set(tenantHeader, test.tenant)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.want || recorder.Header().Get("Access-Control-Allow-Origin") != consoleOrigin {
			t.Fatalf("authorization boundary changed: %d", recorder.Code)
		}
	}
}

func TestBrowserOriginRejectsDuplicateHeaders(t *testing.T) {
	handler, err := WithBrowserOrigins(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached downstream") }), consoleOrigin)
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"Origin", "Access-Control-Request-Method"} {
		request := httptest.NewRequest(http.MethodOptions, "/v1/sites", nil)
		request.Header.Set("Origin", consoleOrigin)
		request.Header.Set("Access-Control-Request-Method", "GET")
		request.Header.Add(header, request.Header.Get(header))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("duplicate %s accepted", header)
		}
	}
}

func TestBrowserOriginWebSocketPreservesTicketOnRejection(t *testing.T) {
	tickets := realtime.NewMemoryTicketStore()
	handler, err := WithBrowserOrigins(NewWithRealtime(fakeVerifier{principal: operatorPrincipal()}, leakyRepository{}, nil, &alerting.MemoryRepository{}, &fakeRealtimeRepository{}, tickets), consoleOrigin)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticket, _, err := tickets.Issue(ctx, realtime.TicketClaims{TenantID: httpTenantID, SiteID: httpSiteID, UserID: "operator-1"})
	if err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/v1/alerts"
	for _, origin := range []string{"https://evil.example", "http://console.example", consoleOrigin} {
		connection, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			HTTPHeader:   http.Header{"Origin": []string{origin}},
			Subprotocols: []string{realtimeProtocol, "ticket." + ticket},
		})
		if origin != consoleOrigin {
			if connection != nil {
				connection.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
				t.Fatal("unapproved WebSocket origin accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Write(ctx, connection, clientCommand{Type: "subscribe", Topic: "alerts.*"}); err != nil {
			t.Fatal(err)
		}
		var snapshot realtimeEnvelope
		if err := wsjson.Read(ctx, connection, &snapshot); err != nil {
			t.Fatal(err)
		}
		connection.CloseNow()
		if snapshot.Type != "snapshot" {
			t.Fatal("expected authenticated snapshot")
		}
	}
	if _, err := tickets.Consume(ctx, ticket); err == nil {
		t.Fatal("successful connection did not consume ticket")
	}
}
