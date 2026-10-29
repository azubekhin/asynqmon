package asynqmon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func testAuthHandler(t *testing.T, root string, readOnly bool) *HTTPHandler {
	t.Helper()
	h := New(Options{RootPath: root, RedisConnOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:1"}, ReadOnly: readOnly, PrometheusAddress: "http://private-prometheus:9090", Auth: &AuthOptions{Username: "admin", Password: "test-password", InsecureCookies: true}})
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func authRequest(h http.Handler, method, url, body string, cookie *http.Cookie, header bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if header {
		r.Header.Set(requestHeader, "1")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func loginCookie(t *testing.T, h *HTTPHandler) *http.Cookie {
	t.Helper()
	w := authRequest(h, "POST", h.rootPath+"/api/auth/login", `{"username":"admin","password":"test-password"}`, nil, true)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestAuthLifecycle(t *testing.T) {
	for _, root := range []string{"", "/monitoring"} {
		for _, readOnly := range []bool{false, true} {
			t.Run(root+"/readonly="+map[bool]string{true: "true", false: "false"}[readOnly], func(t *testing.T) {
				h := testAuthHandler(t, root, readOnly)
				now := time.Now()
				h.auth.now = func() time.Time { return now }
				w := authRequest(h, "GET", root+"/queues/example", "", nil, false)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
					t.Fatalf("SPA unavailable: %d", w.Code)
				}
				for _, url := range []string{root + "/api/queues", root + "/api/metrics", root + "/api/redis_info", root + "/api/unknown", root + "/extra/../api/queues"} {
					if w := authRequest(h, "GET", url, "", nil, false); w.Code != 401 {
						t.Fatalf("anonymous %s: %d", url, w.Code)
					}
				}
				for _, body := range []string{`{"username":"wrong","password":"test-password"}`, `{"username":"admin","password":"wrong"}`} {
					w := authRequest(h, "POST", root+"/api/auth/login", body, nil, true)
					if w.Code != 401 || !strings.Contains(w.Body.String(), "Invalid username or password") {
						t.Fatalf("wrong credentials: %d", w.Code)
					}
				}
				if w := authRequest(h, "GET", root+"/api/queues", "", &http.Cookie{Name: sessionCookieName, Value: "forged"}, false); w.Code != 401 {
					t.Fatal("forged cookie accepted")
				}
				cookie := loginCookie(t, h)
				if cookie.Path != root+"/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 43200 || cookie.Secure {
					t.Fatalf("cookie attributes: %+v", cookie)
				}
				protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
				if w := authRequest(protected, "GET", root+"/metrics", "", nil, false); w.Code != 401 {
					t.Fatal("metrics exposed")
				}
				if w := authRequest(protected, "GET", root+"/metrics", "", cookie, false); w.Code != 204 {
					t.Fatalf("metrics rejected: %d", w.Code)
				}
				w = authRequest(h, "GET", root+"/api/auth/session", "", cookie, false)
				var state struct {
					Authenticated bool `json:"authenticated"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || !state.Authenticated {
					t.Fatal("session not restored")
				}
				// A known API mutation must pass authentication first, then CSRF, then ReadOnly.
				w = authRequest(h, "POST", root+"/api/queues/example:pause", "", cookie, false)
				if w.Code != 403 {
					t.Fatalf("CSRF accepted: %d", w.Code)
				}
				if readOnly {
					w = authRequest(h, "POST", root+"/api/queues/example:pause", "", cookie, true)
					if w.Code != 405 {
						t.Fatalf("ReadOnly bypass: %d", w.Code)
					}
				}
				replacement := loginCookie(t, h)
				if replacement.Value == cookie.Value || h.auth.authenticated(httptest.NewRequest("GET", "/", nil)) {
					t.Fatal("invalid rotation")
				}
				w = authRequest(h, "POST", root+"/api/auth/logout", "", replacement, true)
				if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 || w.Result().Cookies()[0].Path != cookie.Path {
					t.Fatal("logout did not clear cookie")
				}
				if w = authRequest(protected, "GET", root+"/metrics", "", replacement, false); w.Code != 401 {
					t.Fatal("logged-out token still valid")
				}
				cookie = loginCookie(t, h)
				restarted := testAuthHandler(t, root, readOnly)
				if w = authRequest(restarted, "GET", root+"/api/queues", "", cookie, false); w.Code != 401 {
					t.Fatal("session survived restart")
				}
				now = now.Add(sessionLifetime)
				if w = authRequest(protected, "GET", root+"/metrics", "", cookie, false); w.Code != 401 {
					t.Fatal("expired token accepted")
				}
			})
		}
	}
}

func TestAuthRotatesExistingSession(t *testing.T) {
	h := testAuthHandler(t, "", false)
	old := loginCookie(t, h)
	w := authRequest(h, "POST", "/api/auth/login", `{"username":"admin","password":"test-password"}`, old, true)
	if w.Code != 200 {
		t.Fatalf("login: %d", w.Code)
	}
	current := w.Result().Cookies()[0]
	if current.Value == old.Value {
		t.Fatal("token reused")
	}
	protected := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	if authRequest(protected, "GET", "/metrics", "", old, false).Code != 401 {
		t.Fatal("old session not revoked")
	}
	if authRequest(protected, "GET", "/metrics", "", current, false).Code != 204 {
		t.Fatal("replacement session rejected")
	}
}

func TestAuthCSRFAndValidation(t *testing.T) {
	h := testAuthHandler(t, "", false)
	for _, tc := range []struct {
		method, path, body string
		header             bool
		want               int
	}{
		{"POST", "/api/auth/login", `{"username":"admin","password":"test-password"}`, false, 403},
		{"POST", "/api/auth/logout", "", false, 403},
		{"GET", "/api/auth/logout", "", true, 405},
		{"GET", "/api/auth/login", "", true, 405},
		{"POST", "/api/auth/session", "", true, 405},
		{"OPTIONS", "/api/auth/login", "", true, 405},
		{"POST", "/api/auth/login", "{", true, 400},
		{"POST", "/api/auth/login", `{} {}`, true, 400},
		{"POST", "/api/auth/login", `{"unexpected":true}`, true, 400},
		{"GET", "/api/auth/unknown", "", true, 404},
	} {
		if w := authRequest(h, tc.method, tc.path, tc.body, nil, tc.header); w.Code != tc.want {
			t.Fatalf("%s %s: %d != %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	r.Header.Set(requestHeader, "1")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("cross-site request allowed")
	}
	h = testAuthHandler(t, "", false)
	r = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	r.Header.Set(requestHeader, "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("non JSON login accepted")
	}
}

func TestAuthRateLimitConcurrent(t *testing.T) {
	h := testAuthHandler(t, "", false)
	now := time.Now()
	h.auth.now = func() time.Time { return now }
	var wg sync.WaitGroup
	codes := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- authRequest(h, "POST", "/api/auth/login", `{}`, nil, true).Code }()
	}
	wg.Wait()
	close(codes)
	limited := 0
	for code := range codes {
		if code == 429 {
			limited++
		} else if code != 401 {
			t.Fatalf("unexpected %d", code)
		}
	}
	if limited != 10 {
		t.Fatalf("limited %d, want 10", limited)
	}
	now = now.Add(time.Minute)
	if w := authRequest(h, "POST", "/api/auth/login", `{}`, nil, true); w.Code != 401 {
		t.Fatal("limiter did not recover")
	}
}

func TestAuthOptions(t *testing.T) {
	h := New(Options{RedisConnOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:1"}})
	defer h.Close()
	marker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if w := authRequest(h.RequireAuth(marker), "POST", "/metrics", "", nil, false); w.Code != 204 {
		t.Fatal("library compatibility broken")
	}
	a := newAuthManager(AuthOptions{Username: "admin", Password: "test"}, "/monitoring")
	if !a.cookie("test", time.Now(), 1).Secure {
		t.Fatal("Secure must be the default")
	}
	for _, opts := range []AuthOptions{{}, {Username: "admin"}, {Password: "test"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("incomplete auth config accepted")
				}
			}()
			New(Options{RedisConnOpt: asynq.RedisClientOpt{Addr: "127.0.0.1:1"}, Auth: &opts})
		}()
	}
}
