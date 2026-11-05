package hub

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
}

func loopbackHost(hostport string) bool {
	host := hostport
	if strings.Contains(hostport, ":") {
		if h, _, err := net.SplitHostPort(hostport); err == nil {
			host = h
		}
	}
	host = strings.Trim(host, "[]")
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

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
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

func tokenEqual(got, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// apiGuard authenticates API calls and protects cookie-authenticated mutations
// against CSRF. Native clients may use Authorization: Bearer <hub token>.
func apiGuard(next http.Handler, mcpToken, sessionToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func sameOriginBrowserRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		if site == "same-origin" {
			return true
		}
		// A direct navigation has site=none; it must not be accepted for a
		// cookie-authenticated mutation.
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	referer := r.Header.Get("Referer")
	if referer != "" {
		u, err := url.Parse(referer)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	return false
}
