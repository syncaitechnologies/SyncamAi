package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type approvedBrowserOriginKey struct{}

const browserMethods = "GET, HEAD, POST, PATCH, DELETE"
const browserHeaders = "Accept, Authorization, Content-Type, Idempotency-Key, X-Correlation-Id, X-SentinelVision-Tenant-ID, X-SentinelVision-Request-ID"

// WithBrowserOrigins adds an exact-origin boundary, not authorization.
// Remote origins require HTTPS; explicit localhost HTTP origins support dev.
// Empty configuration denies requests carrying Origin; native clients still
// pass through the existing JWT, tenant and device-certificate checks.
func WithBrowserOrigins(next http.Handler, configuration string) (http.Handler, error) {
	origins := make(map[string]string)
	if strings.TrimSpace(configuration) != "" {
		for _, entry := range strings.Split(configuration, ",") {
			origin := strings.TrimSpace(entry)
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1"))) || parsed.Host == "" || parsed.Hostname() == "" ||
				parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
				parsed.Fragment != "" || strings.ContainsAny(origin, "*?[]\\ \t\r\n#") {
				return nil, errors.New("browser origins must be explicit HTTPS origins (HTTP only for localhost) without paths or wildcards")
			}
			if strings.Contains(parsed.Host, ":") {
				port, err := strconv.Atoi(parsed.Port())
				if err != nil || port < 1 || port > 65535 {
					return nil, errors.New("browser origin port is invalid")
				}
			}
			origins[origin] = origin
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		values := r.Header.Values("Origin")
		if len(values) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		origin, allowed := origins[values[0]]
		if len(values) != 1 || !allowed {
			writeError(w, http.StatusForbidden, "ORIGIN_DENIED", "Browser origin is not allowed.")
			return
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			if len(r.Header.Values("Access-Control-Request-Method")) != 1 ||
				!containsHeaderToken(browserMethods, r.Header.Get("Access-Control-Request-Method"), false) {
				writeError(w, http.StatusForbidden, "ORIGIN_DENIED", "Browser request method is not allowed.")
				return
			}
			for _, header := range r.Header.Values("Access-Control-Request-Headers") {
				for _, name := range strings.Split(header, ",") {
					if !containsHeaderToken(browserHeaders, strings.TrimSpace(name), true) {
						writeError(w, http.StatusForbidden, "ORIGIN_DENIED", "Browser request header is not allowed.")
						return
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Origin", values[0])
			w.Header().Set("Access-Control-Allow-Methods", browserMethods)
			w.Header().Set("Access-Control-Allow-Headers", browserHeaders)
			w.Header().Set("Access-Control-Max-Age", "300")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", values[0])
		w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-Id, Retry-After")
		// Only this validated, private context value may extend the WebSocket
		// library's origin allowlist. Never trust forwarded or arbitrary headers.
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), approvedBrowserOriginKey{}, origin)))
	}), nil
}

func containsHeaderToken(list, token string, fold bool) bool {
	for _, entry := range strings.Split(list, ", ") {
		if entry == token || (fold && strings.EqualFold(entry, token)) {
			return true
		}
	}
	return false
}
