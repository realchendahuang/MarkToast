package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const sessionCookieName = "marktoast_session"

func newSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
}

func validPortNumber(port string) bool {
	if port == "" {
		return false
	}
	for i := 0; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func loopbackHost(hostport string) bool {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return false
	}

	host := hostport
	switch {
	case strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]"):
		// Bracketed IPv6 literal without port, e.g. "[::1]".
		host = hostport[1 : len(hostport)-1]
		if strings.ContainsAny(host, "[]") {
			return false
		}
	case strings.Contains(hostport, ":"):
		// Bare IPv6 address (e.g. "::1" from SplitHostPort) or host:port.
		if ip := net.ParseIP(hostport); ip != nil {
			return ip.IsLoopback()
		}
		h, port, err := net.SplitHostPort(hostport)
		if err != nil || h == "" || !validPortNumber(port) {
			return false
		}
		host = h
	default:
		if strings.ContainsAny(host, "[]") {
			return false
		}
	}

	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		if !loopbackHost(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, fmt.Errorf("invalid Host header"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r != nil && r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func cookieToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// tokenEqual compares non-empty tokens in constant time without leaking the
// expected token length.
func tokenEqual(got, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	expHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(gotHash[:], expHash[:]) == 1
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// apiGuard authenticates API calls, sets Cache-Control: no-store on sensitive
// API responses, and protects cookie-authenticated mutations against CSRF.
// Native clients may use Authorization: Bearer <hub token>.
func apiGuard(next http.Handler, mcpToken, sessionToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		bearer := bearerToken(r)
		fromBearer := tokenEqual(bearer, mcpToken)
		fromCookie := tokenEqual(cookieToken(r), sessionToken)
		if !fromBearer && !fromCookie {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, fmt.Errorf("authentication required"))
			return
		}
		if fromCookie && !fromBearer && isMutation(r.Method) && !sameOriginBrowserRequest(r) {
			writeError(w, http.StatusForbidden, fmt.Errorf("cross-site request rejected"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validHTTPURLHost(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	return u.Host, true
}

func sameOriginBrowserRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		originHost, ok := validHTTPURLHost(origin)
		if !ok || !strings.EqualFold(originHost, r.Host) {
			return false
		}
	}

	if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" {
		// Only same-origin is accepted; same-site (e.g. another localhost port),
		// cross-site, and none (direct navigation) are rejected for mutations.
		return site == "same-origin"
	}

	if origin != "" {
		return true
	}

	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer != "" {
		refHost, ok := validHTTPURLHost(referer)
		return ok && strings.EqualFold(refHost, r.Host)
	}
	return false
}

// allowedMCPOrigin enforces MCP local server security best practices: native
// non-browser clients send no Origin header and are allowed; browser requests
// carrying an Origin header must originate from a loopback HTTP(S) origin.
func allowedMCPOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	host, ok := validHTTPURLHost(origin)
	return ok && loopbackHost(host)
}
