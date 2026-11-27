//go:build integration

package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/arrrgi/goldfinger/internal/config"
)

// TestIntegrationRealServer starts the real server on an ephemeral port and
// talks to it over TCP, the way Tailscale and a container health check would.
func TestIntegrationRealServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		IssuerURL: "https://oidc.example.id", AdminEmail: "admin@example.id",
		EmailDomain: "example.id", Port: ln.Addr().(*net.TCPAddr).Port,
		LogLevel: slog.LevelInfo, LogFormat: "json",
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, slog.New(slog.DiscardHandler), ln) }()
	base := "http://" + ln.Addr().String()

	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		var lastErr error
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			resp, err := http.Get(base + path)
			if err != nil {
				lastErr = err
				continue
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return resp, body
		}
		t.Fatalf("GET %s: %v", path, lastErr)
		return nil, nil
	}

	resp, body := get("/.well-known/webfinger?resource=acct:admin@example.id")
	var jrd struct {
		Subject string `json:"subject"`
		Links   []struct{ Rel, Href string }
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/jrd+json" {
		t.Fatalf("webfinger = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if err := json.Unmarshal(body, &jrd); err != nil || jrd.Subject != "acct:admin@example.id" ||
		len(jrd.Links) != 1 || jrd.Links[0].Href != "https://oidc.example.id" {
		t.Errorf("webfinger body = %s (err %v)", body, err)
	}

	if resp, _ := get("/nothing-here"); resp.StatusCode != 404 {
		t.Errorf("unknown path = %d, want 404", resp.StatusCode)
	}
	if err := Healthcheck(t.Context(), cfg.Port); err != nil {
		t.Errorf("Healthcheck against the live server: %v", err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Serve: %v", err)
	}
	if err := Healthcheck(t.Context(), cfg.Port); err == nil {
		t.Error("Healthcheck should fail once the server has stopped")
	}
}
