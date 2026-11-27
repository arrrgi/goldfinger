// Package server holds Goldfinger's HTTP handlers: the WebFinger lookup, the
// health endpoint, robots.txt and the embedded 404 page.
package server

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arrrgi/goldfinger/internal/config"
)

const (
	issuerRel     = "http://openid.net/specs/connect/1.0/issuer"
	robotsTag     = "noindex, nofollow, noarchive"
	robotsBody    = "User-agent: *\nDisallow: /\n"
	allowedMethod = "GET, HEAD"
)

//go:embed notfound.html
var notFoundPage []byte

type link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

type descriptor struct {
	Subject string `json:"subject"`
	Links   []link `json:"links"`
}

// New returns an HTTP server for cfg with timeouts that blunt slow-client abuse.
func New(cfg config.Config, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           Handler(cfg, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
}

// Handler returns the full request handler for cfg.
func Handler(cfg config.Config, logger *slog.Logger) http.Handler {
	h := &handler{cfg: cfg, logger: logger}
	return h.withHeaders(h.withLogging(http.HandlerFunc(h.route)))
}

type handler struct {
	cfg    config.Config
	logger *slog.Logger
}

func (h *handler) route(w http.ResponseWriter, r *http.Request) {
	var serve http.HandlerFunc
	switch r.URL.Path {
	case "/.well-known/webfinger":
		serve = h.webfinger
	case "/healthz":
		serve = healthz
	case "/robots.txt":
		serve = robots
	default:
		notFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", allowedMethod)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	serve(w, r)
}

func (h *handler) webfinger(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	resource := query.Get("resource")
	account, ok := strings.CutPrefix(strings.ToLower(resource), "acct:")
	at := strings.LastIndex(account, "@")
	if !ok || at < 1 || at == len(account)-1 {
		http.Error(w, "resource must be an acct: URI such as acct:user@example.id", http.StatusBadRequest)
		return
	}
	h.logger.Debug("webfinger lookup", "resource", resource, "rel", query["rel"])
	if account[at+1:] != h.cfg.EmailDomain {
		notFound(w, r)
		return
	}

	links := []link{}
	if rels := query["rel"]; len(rels) == 0 || contains(rels, issuerRel) {
		links = append(links, link{Rel: issuerRel, Href: h.cfg.IssuerURL})
	}
	w.Header().Set("Content-Type", "application/jrd+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Encoding a struct of strings cannot fail; a write error means the client left.
	_ = json.NewEncoder(w).Encode(descriptor{Subject: resource, Links: links})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(robotsBody))
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(notFoundPage)
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (h *handler) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", robotsTag)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (h *handler) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" {
			level = slog.LevelDebug
		}
		h.logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}
