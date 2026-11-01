package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed dashboard.html
var tutorDashboardHTML string

const dashboardProtocol = 1
const dashboardCookie = "english_guard_dashboard"

type dashboardState struct {
	URL            string    `json:"url"`
	Token          string    `json:"token"`
	InstanceID     string    `json:"instance_id"`
	PID            int       `json:"pid"`
	Protocol       int       `json:"protocol"`
	Version        string    `json:"version"`
	Executable     string    `json:"executable"`
	ExecutableHash string    `json:"executable_sha256"`
	StartedAt      time.Time `json:"started_at"`
}

func dashboardStatePath(home string) string {
	return filepath.Join(home, "english-guard", "dashboard.json")
}

func dashboardCommand(args []string) error {
	if len(args) == 1 && args[0] == "--serve" {
		return serveTutorDashboard()
	}
	if len(args) != 0 {
		return errors.New("usage: english-guard dashboard")
	}
	address, err := launchTutorDashboard()
	if address != "" {
		fmt.Println(address)
	}
	return err
}

func dashboardExecutable() (string, string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", "", err
	}
	return path, hex.EncodeToString(hash.Sum(nil)), nil
}

// launchTutorDashboard uses the current executable: in a hook this is the
// installed version, while a developer build must not launch an older install.
func launchTutorDashboard() (string, error) {
	home, err := codexHome()
	if err != nil {
		return "", err
	}
	executable, hash, err := dashboardExecutable()
	if err != nil {
		return "", err
	}
	release, err := lockDashboardLaunch(home)
	if err != nil {
		return "", err
	}
	state, stateErr := readDashboardState(home)
	if stateErr != nil || !dashboardMatches(state, executable, hash) || !dashboardHealthy(state) {
		command := exec.Command(executable, "dashboard", "--serve")
		detachDashboardProcess(command)
		// Do not leave the hook's protocol pipes open in the background server.
		null, openErr := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if openErr != nil {
			release()
			return "", openErr
		}
		command.Stdin, command.Stdout, command.Stderr = null, null, null
		startErr := command.Start()
		null.Close()
		if startErr != nil {
			release()
			return "", fmt.Errorf("start timing dashboard: %w", startErr)
		}
		_ = command.Process.Release()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			next, err := readDashboardState(home)
			if err == nil && dashboardMatches(next, executable, hash) && dashboardHealthy(next) {
				state = next
				break
			}
			time.Sleep(60 * time.Millisecond)
		}
		if !dashboardMatches(state, executable, hash) || !dashboardHealthy(state) {
			release()
			return "", errors.New("timing dashboard did not start within 6 seconds")
		}
	}
	release()
	address := state.URL + "/?token=" + url.QueryEscape(state.Token)
	if err := openDashboardBrowser(address); err != nil {
		return address, fmt.Errorf("dashboard is running; open the URL above in your browser: %w", err)
	}
	return address, nil
}

func dashboardMatches(state dashboardState, executable, hash string) bool {
	return state.Protocol == dashboardProtocol && state.Version == version &&
		state.Executable == executable && state.ExecutableHash == hash
}

func dashboardBrowserCommand(platform, address string) (string, []string) {
	switch platform {
	case "darwin":
		return "open", []string{address}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		return "xdg-open", []string{address}
	}
}

func openDashboardBrowser(address string) error {
	name, args := dashboardBrowserCommand(runtime.GOOS, address)
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func lockDashboardLaunch(home string) (func(), error) {
	directory := filepath.Dir(dashboardStatePath(home))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "dashboard-start.lock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		if err := os.Mkdir(path, 0700); err == nil {
			return func() { _ = os.Remove(path) }, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, err := os.Lstat(path); err == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another timing dashboard is starting; try again shortly")
		}
		time.Sleep(60 * time.Millisecond)
	}
}

func readDashboardState(home string) (dashboardState, error) {
	var state dashboardState
	path := dashboardStatePath(home)
	info, err := os.Lstat(path)
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return state, errors.New("dashboard address file is not private")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	parsed, err := url.Parse(state.URL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return dashboardState{}, errors.New("invalid dashboard address")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 || len(state.Token) != 64 || len(state.InstanceID) != 32 {
		return dashboardState{}, errors.New("invalid dashboard address state")
	}
	if _, err := hex.DecodeString(state.Token); err != nil {
		return dashboardState{}, errors.New("invalid dashboard access token")
	}
	return state, nil
}

func writeDashboardState(home string, state dashboardState) error {
	directory := filepath.Dir(dashboardStatePath(home))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".dashboard-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), dashboardStatePath(home))
}

func dashboardHealthy(state dashboardState) bool {
	request, err := http.NewRequest(http.MethodGet, state.URL+"/api/health", nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+state.Token)
	client := &http.Client{
		Timeout:       400 * time.Millisecond,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var health struct {
		Protocol       int    `json:"protocol"`
		InstanceID     string `json:"instance_id"`
		ExecutableHash string `json:"executable_sha256"`
	}
	return response.StatusCode == http.StatusOK &&
		json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) == nil &&
		health.Protocol == dashboardProtocol && health.InstanceID == state.InstanceID &&
		health.ExecutableHash == state.ExecutableHash
}

// stopTutorDashboard only contacts the private, authenticated instance recorded
// by this installation. A stale address is removed without signalling a PID.
func stopTutorDashboard(home string) error {
	state, err := readDashboardState(home)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if dashboardHealthy(state) {
		request, err := http.NewRequest(http.MethodPost, state.URL+"/api/shutdown", nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+state.Token)
		request.Header.Set("X-Dashboard-Instance", state.InstanceID)
		client := &http.Client{
			Timeout:       2 * time.Second,
			Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("stop timing dashboard: %w", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			return errors.New("timing dashboard rejected the shutdown request")
		}
		deadline := time.Now().Add(2 * time.Second)
		for dashboardHealthy(state) {
			if time.Now().After(deadline) {
				return errors.New("timing dashboard did not stop within 2 seconds")
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	if current, err := readDashboardState(home); err == nil && current.InstanceID == state.InstanceID && current.Token == state.Token {
		if err := os.Remove(dashboardStatePath(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	lock := filepath.Join(filepath.Dir(dashboardStatePath(home)), "dashboard-start.lock")
	if info, err := os.Lstat(lock); err == nil && time.Since(info.ModTime()) > 30*time.Second {
		_ = os.Remove(lock)
	}
	return nil
}

func dashboardRandom(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func serveTutorDashboard() error {
	home, err := codexHome()
	if err != nil {
		return err
	}
	executable, hash, err := dashboardExecutable()
	if err != nil {
		return err
	}
	token, err := dashboardRandom(32)
	if err != nil {
		return err
	}
	instance, err := dashboardRandom(16)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	state := dashboardState{
		URL: "http://" + listener.Addr().String(), Token: token, InstanceID: instance,
		PID: os.Getpid(), Protocol: dashboardProtocol, Version: version,
		Executable: executable, ExecutableHash: hash, StartedAt: time.Now(),
	}
	var lastUsed atomic.Int64
	lastUsed.Store(time.Now().UnixNano())
	server := &http.Server{
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024,
	}
	server.Handler = newDashboardHandler(home, state, &lastUsed, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	if err := writeDashboardState(home, state); err != nil {
		return err
	}
	defer func() {
		if current, err := readDashboardState(home); err == nil && current.InstanceID == state.InstanceID {
			_ = os.Remove(dashboardStatePath(home))
		}
	}()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-finished:
				return
			case <-ticker.C:
				current, err := readDashboardState(home)
				superseded := err != nil || current.InstanceID != state.InstanceID
				if superseded || time.Since(time.Unix(0, lastUsed.Load())) > 30*time.Minute {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = server.Shutdown(ctx)
					cancel()
					return
				}
			}
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newDashboardHandler(home string, state dashboardState, lastUsed *atomic.Int64, shutdown ...func()) http.Handler {
	expectedHost := strings.TrimPrefix(state.URL, "http://")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(peer).IsLoopback() || r.Host != expectedHost ||
			(r.Header.Get("Origin") != "" && r.Header.Get("Origin") != state.URL) ||
			r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Local dashboard access only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && !(r.Method == http.MethodPost && r.URL.Path == "/api/shutdown") {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		matches := func(candidate string) bool {
			return len(candidate) == len(state.Token) && subtle.ConstantTimeCompare([]byte(candidate), []byte(state.Token)) == 1
		}
		// A one-time local URL establishes an HttpOnly cookie, then removes the
		// token from the address bar. Neither JavaScript nor localStorage sees it.
		if r.URL.Path == "/" && matches(r.URL.Query().Get("token")) {
			http.SetCookie(w, &http.Cookie{
				Name: dashboardCookie, Value: state.Token, Path: "/", HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		bearer := r.Header.Get("Authorization")
		bearerAuthorized := strings.HasPrefix(bearer, "Bearer ") && matches(strings.TrimPrefix(bearer, "Bearer "))
		authorized := bearerAuthorized
		if cookie, err := r.Cookie(dashboardCookie); err == nil {
			authorized = authorized || matches(cookie.Value)
		}
		if !authorized {
			http.Error(w, "Run 'english-guard dashboard' to open this private dashboard.", http.StatusUnauthorized)
			return
		}
		if lastUsed != nil {
			lastUsed.Store(time.Now().UnixNano())
		}
		switch r.URL.Path {
		case "/":
			nonce, err := dashboardRandom(16)
			if err != nil {
				http.Error(w, "Could not open dashboard", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; connect-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			_, _ = io.WriteString(w, strings.ReplaceAll(tutorDashboardHTML, "__CSP_NONCE__", nonce))
		case "/api/health":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"protocol": dashboardProtocol, "instance_id": state.InstanceID,
				"executable_sha256": state.ExecutableHash, "version": state.Version,
			})
		case "/api/runs":
			records, err := readTutorTraces(home)
			if err != nil {
				http.Error(w, "Could not read timing history", http.StatusInternalServerError)
				return
			}
			if records == nil {
				records = []tutorTraceRecord{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"runs": records, "server_time": time.Now(), "version": state.Version,
			})
		case "/api/shutdown":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !bearerAuthorized || r.Header.Get("X-Dashboard-Instance") != state.InstanceID {
				http.Error(w, "Dashboard instance authentication required", http.StatusForbidden)
				return
			}
			if len(shutdown) == 0 || shutdown[0] == nil {
				http.Error(w, "Shutdown unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "{\"status\":\"stopping\"}\n")
			go shutdown[0]()
		default:
			http.NotFound(w, r)
		}
	})
}
