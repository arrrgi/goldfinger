package app

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arrrgi/goldfinger/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var validEnv = map[string]string{
	"ISSUER_URL":         "https://oidc.example.id",
	"ISSUER_ADMIN_EMAIL": "admin@example.id",
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunPrintsHelpAndUsage(t *testing.T) {
	for _, flagName := range []string{"-h", "--help", "--usage"} {
		var out, errOut bytes.Buffer
		if code := Run(t.Context(), []string{flagName}, env(nil), &out, &errOut); code != ExitOK {
			t.Errorf("%s: exit %d, want 0 (stderr %q)", flagName, code, errOut.String())
		}
		for _, want := range []string{"--issuer-url", "ISSUER_URL", "--issuer-admin-email", "ISSUER_ADMIN_EMAIL", "--port"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s output is missing %q", flagName, want)
			}
		}
	}
}

func TestRunRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"missing issuer", nil, nil, "ISSUER_URL"},
		{"unknown flag", []string{"--nope"}, validEnv, "nope"},
		{"bad port", []string{"--port", "99999"}, validEnv, "--port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(t.Context(), tt.args, env(tt.env), &out, &errOut); code != ExitUsage {
				t.Errorf("exit %d, want %d", code, ExitUsage)
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Errorf("stderr %q should name %q", errOut.String(), tt.want)
			}
			if out.Len() != 0 {
				t.Errorf("stdout should stay empty, got %q", out.String())
			}
			if lines := strings.Count(strings.TrimSuffix(errOut.String(), "\n"), "\n") + 1; lines != 1 {
				t.Errorf("stderr must be one line, got %d: %q", lines, errOut.String())
			}
		})
	}
}

func TestRunPortInUseExitsNonZeroNamingAddress(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)

	e := map[string]string{"PORT": port, "ISSUER_URL": validEnv["ISSUER_URL"], "ISSUER_ADMIN_EMAIL": validEnv["ISSUER_ADMIN_EMAIL"]}
	var out, errOut bytes.Buffer
	if code := Run(t.Context(), nil, env(e), &out, &errOut); code != ExitFailure {
		t.Errorf("exit %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(errOut.String(), ":"+port) {
		t.Errorf("stderr %q should name the address :%s", errOut.String(), port)
	}
}

func TestRunHealthcheck(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer healthy.Close()
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer sick.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer redirecting.Close()

	gone := httptest.NewServer(http.NotFoundHandler())
	gonePort := portOf(t, gone.URL)
	gone.Close() // nothing listens here any more

	tests := []struct {
		name string
		port int
		want int
	}{
		{"healthy", portOf(t, healthy.URL), ExitOK},
		{"non-200", portOf(t, sick.URL), ExitFailure},
		{"redirect is not healthy", portOf(t, redirecting.URL), ExitFailure},
		{"unreachable", gonePort, ExitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			args := []string{"--healthcheck", "--port", strconv.Itoa(tt.port)}
			if code := Run(t.Context(), args, env(nil), &out, &errOut); code != tt.want {
				t.Errorf("exit %d, want %d (stderr %q)", code, tt.want, errOut.String())
			}
		})
	}
}

func TestNewLoggerFormats(t *testing.T) {
	var j, x bytes.Buffer
	NewLogger(&j, slog.LevelInfo, "json").Info("hello", "k", "v")
	NewLogger(&x, slog.LevelInfo, "text").Info("hello", "k", "v")
	if !strings.HasPrefix(j.String(), "{") || !strings.Contains(j.String(), `"msg":"hello"`) {
		t.Errorf("json log = %q", j.String())
	}
	if !strings.Contains(x.String(), "msg=hello") {
		t.Errorf("text log = %q", x.String())
	}
	var quiet bytes.Buffer
	NewLogger(&quiet, slog.LevelWarn, "json").Info("hidden")
	if quiet.Len() != 0 {
		t.Errorf("info should be filtered at warn level, got %q", quiet.String())
	}
}

func TestServeShutsDownCleanlyAndLogsSanitizedSettings(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		IssuerURL: "https://oidc.example.id", AdminEmail: "admin@example.id",
		EmailDomain: "example.id", Port: 8080, LogLevel: slog.LevelInfo, LogFormat: "json",
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, NewLogger(&logs, slog.LevelInfo, "json"), ln) }()

	// Wait until the server answers, then stop it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}

	out := logs.String()
	if !strings.Contains(out, "example.id") || !strings.Contains(out, `"issuer_url":"https://oidc.example.id"`) {
		t.Errorf("startup log is missing settings: %s", out)
	}
	if strings.Contains(out, "admin@example.id") {
		t.Errorf("startup log must not contain the admin email: %s", out)
	}
}
