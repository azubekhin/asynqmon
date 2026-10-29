package asynqmon

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

const (
	sessionLifetime   = 12 * time.Hour
	sessionCookieName = "asynqmon_session"
	requestHeader     = "X-Asynqmon-Request"
)

// AuthOptions enables a single shared account without a user database.
type AuthOptions struct {
	Username string
	Password string
	// InsecureCookies allows HTTP for local development or explicitly insecure deployments.
	// Leave false when using HTTPS, including behind a TLS-terminating proxy.
	InsecureCookies bool
}

type authManager struct {
	username   [32]byte
	password   [32]byte
	cookiePath string
	secure     bool
	now        func() time.Time
	mu         sync.Mutex
	sessions   map[[32]byte]time.Time
	attempts   []time.Time
}

func newAuthManager(opts AuthOptions, rootPath string) *authManager {
	return &authManager{
		username:   sha256.Sum256([]byte(opts.Username)),
		password:   sha256.Sum256([]byte(opts.Password)),
		cookiePath: rootPath + "/",
		secure:     !opts.InsecureCookies,
		now:        time.Now,
		sessions:   make(map[[32]byte]time.Time),
	}
}

// RequireAuth protects an additional handler (for example a metrics exporter)
// using the same sessions as the UI. With Options.Auth unset it is a no-op.
func (h *HTTPHandler) RequireAuth(next http.Handler) http.Handler {
	if h.auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !h.auth.authenticated(r) {
			authJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !validAuthRequest(r) {
			authJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid request"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Custom headers cannot be sent cross-origin without a successful CORS preflight.
// No auth or protected endpoint opts into CORS. Reject cross-site browser requests too.
func validAuthRequest(r *http.Request) bool {
	return r.Header.Get(requestHeader) == "1" && r.Header.Get("Sec-Fetch-Site") != "cross-site" && r.Method != http.MethodOptions
}

func authJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *authManager) authenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	key := sha256.Sum256([]byte(c.Value))
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.sessions[key]
	if !ok {
		return false
	}
	if !a.now().Before(expires) {
		delete(a.sessions, key)
		return false
	}
	return true
}

func (a *authManager) serveAuth(w http.ResponseWriter, r *http.Request, requestPath string) {
	base := a.cookiePath + "api/auth/"
	switch requestPath {
	case base + "session":
		if r.Method != http.MethodGet {
			authMethodNotAllowed(w, "GET")
			return
		}
		authJSON(w, http.StatusOK, map[string]bool{"authenticated": a.authenticated(r)})
	case base + "login":
		if r.Method != http.MethodPost {
			authMethodNotAllowed(w, "POST")
			return
		}
		if !validAuthRequest(r) {
			authJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid request"})
			return
		}
		a.login(w, r)
	case base + "logout":
		if r.Method != http.MethodPost {
			authMethodNotAllowed(w, "POST")
			return
		}
		if !validAuthRequest(r) {
			authJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid request"})
			return
		}
		a.mu.Lock()
		if c, err := r.Cookie(sessionCookieName); err == nil {
			delete(a.sessions, sha256.Sum256([]byte(c.Value)))
		}
		a.mu.Unlock()
		http.SetCookie(w, a.cookie("", time.Unix(1, 0), -1))
		authJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
	default:
		authJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func authMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	authJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
}

func (a *authManager) login(w http.ResponseWriter, r *http.Request) {
	now := a.now()
	a.mu.Lock()
	recent := a.attempts[:0]
	for _, attempt := range a.attempts {
		if attempt.After(now.Add(-time.Minute)) {
			recent = append(recent, attempt)
		}
	}
	a.attempts = recent
	if len(a.attempts) >= 10 {
		a.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		authJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many login attempts. Try again in a minute."})
		return
	}
	a.attempts = append(a.attempts, now)
	a.mu.Unlock()
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		authJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Expected application/json"})
		return
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil {
		authJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		authJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return
	}
	username := sha256.Sum256([]byte(credentials.Username))
	password := sha256.Sum256([]byte(credentials.Password))
	if subtle.ConstantTimeCompare(username[:], a.username[:])&subtle.ConstantTimeCompare(password[:], a.password[:]) != 1 {
		authJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid username or password"})
		return
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		authJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not create session"})
		return
	}
	value := base64.RawURLEncoding.EncodeToString(token[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, expires := range a.sessions {
		if !now.Before(expires) {
			delete(a.sessions, key)
		}
	}
	if len(a.sessions) >= 1024 {
		authJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Too many active sessions"})
		return
	}
	// Rotate the current session instead of accepting or reusing a supplied token.
	if c, err := r.Cookie(sessionCookieName); err == nil {
		delete(a.sessions, sha256.Sum256([]byte(c.Value)))
	}
	a.sessions[sha256.Sum256([]byte(value))] = now.Add(sessionLifetime)
	http.SetCookie(w, a.cookie(value, now.Add(sessionLifetime), int(sessionLifetime/time.Second)))
	authJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *authManager) cookie(value string, expires time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: value, Path: a.cookiePath, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge, Expires: expires}
}
