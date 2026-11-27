// Package app wires Goldfinger together: it parses settings, builds the
// logger, runs the server, and implements the --healthcheck self-probe.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/arrrgi/goldfinger/internal/config"
	"github.com/arrrgi/goldfinger/internal/server"
)

// Exit codes returned by Run.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

const shutdownTimeout = 10 * time.Second

// Run is the whole program. It returns the process exit code and never
// calls os.Exit, so tests can drive it.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, action, err := config.Parse(args, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "goldfinger: %v (see --help)\n", err)
		return ExitUsage
	}

	switch action {
	case config.ActionHelp:
		fmt.Fprint(stdout, config.Help())
		return ExitOK
	case config.ActionUsage:
		fmt.Fprint(stdout, config.Usage())
		return ExitOK
	case config.ActionHealthcheck:
		if err := Healthcheck(ctx, cfg.Port); err != nil {
			fmt.Fprintf(stderr, "goldfinger: healthcheck failed: %v\n", err)
			return ExitFailure
		}
		return ExitOK
	}

	logger := NewLogger(stdout, cfg.LogLevel, cfg.LogFormat)
	addr := ":" + strconv.Itoa(cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "goldfinger: cannot listen on %s: %v\n", addr, err)
		return ExitFailure
	}
	if err := Serve(ctx, cfg, logger, ln); err != nil {
		fmt.Fprintf(stderr, "goldfinger: %v\n", err)
		return ExitFailure
	}
	return ExitOK
}

// NewLogger returns a slog logger writing to w in the given format ("json" or
// "text") at the given level.
func NewLogger(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// Serve runs the server on ln until ctx is cancelled, then shuts it down
// gracefully. It returns nil after a clean shutdown.
func Serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error {
	srv := server.New(cfg, logger)
	// Only the email domain is logged, never the full admin address.
	logger.Info("starting",
		"addr", ln.Addr().String(),
		"issuer_url", cfg.IssuerURL,
		"email_domain", cfg.EmailDomain,
		"log_level", cfg.LogLevel.String(),
		"log_format", cfg.LogFormat,
	)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Healthcheck requests /healthz on 127.0.0.1 at port and returns an error
// unless the answer is 200.
func Healthcheck(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	return nil
}
