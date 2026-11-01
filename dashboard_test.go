package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func dashboardTestState() dashboardState {
	return dashboardState{
		URL: "http://127.0.0.1:51723", Token: strings.Repeat("ab", 32),
		InstanceID: strings.Repeat("cd", 16), Protocol: dashboardProtocol,
		Version: version, Executable: "/example/english-guard", ExecutableHash: "test-hash",
		StartedAt: time.Now(),
	}
}

func dashboardRequest(handler http.Handler, state dashboardState, method, path string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, state.URL+path, nil)
	request.RemoteAddr = "127.0.0.1:49152"
	if mutate != nil {
		mutate(request)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestDashboardPrivateAccess(t *testing.T) {
	state := dashboardTestState()
	handler := newDashboardHandler(t.TempDir(), state, nil)
	for _, test := range []struct {
		name   string
		method string
		mutate func(*http.Request)
		want   int
	}{
		{"no token", http.MethodGet, nil, http.StatusUnauthorized},
		{"wrong token", http.MethodGet, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, http.StatusUnauthorized},
		{"valid bearer", http.MethodGet, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+state.Token) }, http.StatusOK},
		{"raw token is not bearer", http.MethodGet, func(r *http.Request) { r.Header.Set("Authorization", state.Token) }, http.StatusUnauthorized},
		{"valid cookie", http.MethodGet, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: dashboardCookie, Value: state.Token}) }, http.StatusOK},
		{"foreign peer", http.MethodGet, func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:49152"
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusForbidden},
		{"foreign host", http.MethodGet, func(r *http.Request) {
			r.Host = "attacker.example:51723"
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusForbidden},
		{"foreign origin", http.MethodGet, func(r *http.Request) {
			r.Header.Set("Origin", "https://attacker.example")
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusForbidden},
		{"null origin", http.MethodGet, func(r *http.Request) {
			r.Header.Set("Origin", "null")
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusForbidden},
		{"same origin", http.MethodGet, func(r *http.Request) {
			r.Header.Set("Origin", state.URL)
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusOK},
		{"cross site navigation", http.MethodGet, func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("Authorization", "Bearer "+state.Token)
		}, http.StatusForbidden},
		{"mutation rejected", http.MethodPost, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+state.Token) }, http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := dashboardRequest(handler, state, test.method, "/api/runs", test.mutate)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private dashboard response must not be cached")
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("dashboard must not allow cross-origin reading")
			}
		})
	}
}

func TestDashboardBrowserBootstrapAndPage(t *testing.T) {
	state := dashboardTestState()
	handler := newDashboardHandler(t.TempDir(), state, nil)
	response := dashboardRequest(handler, state, http.MethodGet, "/?token="+state.Token, nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
		t.Fatalf("bootstrap must remove access token from URL: %d %v", response.Code, response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != state.Token || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("bootstrap cookie is not private: %#v", cookies)
	}
	response = dashboardRequest(handler, state, http.MethodGet, "/", func(r *http.Request) { r.AddCookie(cookies[0]) })
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Where the seconds go.") {
		t.Fatalf("dashboard page failed: %d", response.Code)
	}
	if strings.Contains(response.Body.String(), state.Token) || strings.Contains(response.Body.String(), "__CSP_NONCE__") {
		t.Fatal("dashboard must neither expose the access token nor leave CSP placeholders")
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "script-src 'nonce-") || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("dashboard must constrain scripts and prevent token referrer leaks")
	}
}

func TestDashboardAPIEmptyHistoryAndHealth(t *testing.T) {
	state := dashboardTestState()
	handler := newDashboardHandler(t.TempDir(), state, nil)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+state.Token) }
	response := dashboardRequest(handler, state, http.MethodGet, "/api/runs", auth)
	var payload struct {
		Runs       []json.RawMessage `json:"runs"`
		ServerTime time.Time         `json:"server_time"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Runs == nil || len(payload.Runs) != 0 || payload.ServerTime.IsZero() {
		t.Fatalf("empty API history must be a timestamped array: %s (%v)", response.Body.String(), err)
	}
	response = dashboardRequest(handler, state, http.MethodGet, "/api/health", auth)
	var health map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["instance_id"] != state.InstanceID || health["executable_sha256"] != state.ExecutableHash || health["token"] != nil {
		t.Fatalf("incorrect or sensitive health response: %v", health)
	}
}

func TestDashboardStatePrivacyAndValidation(t *testing.T) {
	home := t.TempDir()
	state := dashboardTestState()
	if err := writeDashboardState(home, state); err != nil {
		t.Fatal(err)
	}
	read, err := readDashboardState(home)
	if err != nil || read.Token != state.Token || read.InstanceID != state.InstanceID {
		t.Fatalf("state round trip failed: %v", err)
	}
	info, err := os.Stat(dashboardStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited ACLs rather than POSIX permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("dashboard token file must have private permissions: %v", err)
	}
	for _, address := range []string{"https://127.0.0.1:51723", "http://example.com:51723", "http://127.0.0.1:0", "http://127.0.0.1:51723/path", "http://user@127.0.0.1:51723", "http://127.0.0.1:51723?redirect=elsewhere"} {
		state.URL = address
		if err := writeDashboardState(home, state); err != nil {
			t.Fatal(err)
		}
		if _, err := readDashboardState(home); err == nil {
			t.Errorf("accepted unsafe dashboard address %q", address)
		}
	}
	if runtime.GOOS != "windows" {
		state = dashboardTestState()
		if err := writeDashboardState(home, state); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dashboardStatePath(home), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := readDashboardState(home); err == nil {
			t.Fatal("accepted a world-readable token file")
		}
	}
}

func TestDashboardShutdownRequiresInstanceAndBearer(t *testing.T) {
	state := dashboardTestState()
	stopped := make(chan struct{}, 1)
	handler := newDashboardHandler(t.TempDir(), state, nil, func() { stopped <- struct{}{} })
	for _, test := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"cookie only", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: dashboardCookie, Value: state.Token})
			r.Header.Set("X-Dashboard-Instance", state.InstanceID)
		}},
		{"wrong instance", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+state.Token)
			r.Header.Set("X-Dashboard-Instance", "stale-instance")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := dashboardRequest(handler, state, http.MethodPost, "/api/shutdown", test.mutate)
			if response.Code != http.StatusForbidden {
				t.Fatalf("unsafe shutdown accepted: %d", response.Code)
			}
		})
	}
	response := dashboardRequest(handler, state, http.MethodPost, "/api/shutdown", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+state.Token)
		r.Header.Set("X-Dashboard-Instance", state.InstanceID)
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("authenticated shutdown rejected: %d", response.Code)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback was not called")
	}
}

func TestDashboardStopRemovesStaleState(t *testing.T) {
	home := t.TempDir()
	if err := stopTutorDashboard(home); err != nil {
		t.Fatal(err)
	}
	state := dashboardTestState()
	// Reserve and close our own port to avoid contacting another local app.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listeners unavailable: %v", err)
	}
	state.URL = "http://" + listener.Addr().String()
	listener.Close()
	if err := writeDashboardState(home, state); err != nil {
		t.Fatal(err)
	}
	if err := stopTutorDashboard(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "english-guard", "dashboard.json")); !os.IsNotExist(err) {
		t.Fatalf("stale address file remains: %v", err)
	}
}

func TestDashboardBrowserCommands(t *testing.T) {
	address := "http://127.0.0.1:51723/?token=example"
	for _, test := range []struct {
		platform string
		name     string
		args     []string
	}{
		{"darwin", "open", []string{address}},
		{"linux", "xdg-open", []string{address}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", address}},
	} {
		name, args := dashboardBrowserCommand(test.platform, address)
		if name != test.name || !reflect.DeepEqual(args, test.args) {
			t.Fatalf("browser command for %s = %s %v", test.platform, name, args)
		}
	}
}
