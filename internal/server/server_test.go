package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/arrrgi/goldfinger/internal/config"
)

var testConfig = config.Config{
	IssuerURL:   "https://oidc.example.id/",
	AdminEmail:  "admin@example.id",
	EmailDomain: "example.id",
	Port:        8080,
}

func newHandler(t testing.TB, level slog.Level) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: level}))
	return Handler(testConfig, logger), &logs
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func lookup(account string, rels ...string) string {
	q := url.Values{"resource": {account}}
	for _, r := range rels {
		q.Add("rel", r)
	}
	return "/.well-known/webfinger?" + q.Encode()
}

func TestWebFingerStatuses(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	tests := []struct {
		name   string
		method string
		target string
		want   int
	}{
		{"match", "GET", lookup("acct:admin@example.id"), 200},
		{"match HEAD", "HEAD", lookup("acct:admin@example.id"), 200},
		{"any user on the domain", "GET", lookup("acct:someone.else@example.id"), 200},
		{"domain is case-insensitive", "GET", lookup("acct:admin@EXAMPLE.ID"), 200},
		{"scheme is case-insensitive", "GET", lookup("ACCT:admin@example.id"), 200},
		{"missing resource", "GET", "/.well-known/webfinger", 400},
		{"empty resource", "GET", "/.well-known/webfinger?resource=", 400},
		{"not an acct uri", "GET", lookup("mailto:admin@example.id"), 400},
		{"acct without at", "GET", lookup("acct:admin"), 400},
		{"acct without user", "GET", lookup("acct:@example.id"), 400},
		{"acct with two separators", "GET", lookup("acct:alice@@example.id"), 400},
		{"acct with user containing at", "GET", lookup("acct:one@two@example.id"), 400},
		{"acct without domain", "GET", lookup("acct:admin@"), 400},
		{"other domain", "GET", lookup("acct:admin@other.id"), 404},
		{"subdomain is a different domain", "GET", lookup("acct:admin@sub.example.id"), 404},
		{"POST", "POST", lookup("acct:admin@example.id"), 405},
		{"DELETE", "DELETE", "/.well-known/webfinger", 405},
		{"other path", "GET", "/", 404},
		{"unknown path", "GET", "/nope", 404},
		{"healthz", "GET", "/healthz", 200},
		{"healthz POST", "POST", "/healthz", 405},
		{"robots", "GET", "/robots.txt", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, tt.method, tt.target)
			if rec.Code != tt.want {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.target, rec.Code, tt.want)
			}
			if tt.want == 405 && rec.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("Allow = %q, want %q", rec.Header().Get("Allow"), "GET, HEAD")
			}
		})
	}
}

func TestWebFingerBody(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	rec := do(h, "GET", lookup("acct:Admin@example.id"))

	if mt, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type")); mt != "application/jrd+json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	var got descriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body)
	}
	want := descriptor{
		Subject: "acct:Admin@example.id",                                    // echoes the requested resource as given
		Links:   []link{{Rel: issuerRel, Href: "https://oidc.example.id/"}}, // issuer used verbatim
	}
	if got.Subject != want.Subject || len(got.Links) != 1 || got.Links[0] != want.Links[0] {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

func TestWebFingerRel(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	tests := []struct {
		name      string
		rels      []string
		wantLinks int
	}{
		{"absent", nil, 1},
		{"issuer rel", []string{issuerRel}, 1},
		{"other rel", []string{"http://webfinger.net/rel/profile-page"}, 0},
		{"issuer among several", []string{"x", issuerRel}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, "GET", lookup("acct:admin@example.id", tt.rels...))
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			var links []link
			if err := json.Unmarshal(raw["links"], &links); err != nil || links == nil {
				t.Fatalf("links must be an array, got %s (err %v)", raw["links"], err)
			}
			if len(links) != tt.wantLinks {
				t.Errorf("len(links) = %d, want %d", len(links), tt.wantLinks)
			}
		})
	}
}

func TestRobotsPolicy(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	rec := do(h, "GET", "/robots.txt")
	if rec.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Errorf("robots.txt = %q", rec.Body)
	}
	if mt, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type")); mt != "text/plain" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestNotFoundPage(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	rec := do(h, "GET", "/missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if mt, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type")); mt != "text/html" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.Len() == 0 {
		t.Error("404 page is empty")
	}
}

func TestHeadHasNoBody(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	// Run through a real server: only net/http strips HEAD bodies.
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/healthz", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) != 0 {
		t.Errorf("HEAD /healthz = %d with %d body bytes, want 200 and none", resp.StatusCode, len(body))
	}
}

func TestOptionsStarIsHandledAndLogged(t *testing.T) {
	var logs bytes.Buffer
	srv := New(testConfig, slog.New(slog.NewJSONHandler(&logs, nil)))
	ts := httptest.NewUnstartedServer(srv.Handler)
	ts.Config = srv // use the real server settings, as production does
	ts.Start()
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL, nil)
	req.URL.Opaque = "*" // request target "*", not "/"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Errorf("OPTIONS * X-Robots-Tag = %q (status %d)", got, resp.StatusCode)
	}
	if !strings.Contains(logs.String(), `"method":"OPTIONS"`) {
		t.Errorf("OPTIONS * was not logged: %q", logs.String())
	}
}

func TestEveryResponseCarriesRobotsTag(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	for _, target := range []string{
		lookup("acct:admin@example.id"), lookup("acct:admin@other.id"), "/.well-known/webfinger",
		"/healthz", "/robots.txt", "/missing",
	} {
		if got := do(h, "GET", target).Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
			t.Errorf("GET %s: X-Robots-Tag = %q", target, got)
		}
	}
	if got := do(h, "POST", "/healthz").Header().Get("X-Robots-Tag"); got == "" {
		t.Error("405 response is missing X-Robots-Tag")
	}
}

func TestRequestLogging(t *testing.T) {
	t.Run("info level logs requests but not healthz or resource", func(t *testing.T) {
		h, logs := newHandler(t, slog.LevelInfo)
		do(h, "GET", lookup("acct:secret.person@example.id"))
		do(h, "GET", "/healthz")
		out := logs.String()
		if !strings.Contains(out, `"path":"/.well-known/webfinger"`) || !strings.Contains(out, `"status":200`) ||
			!strings.Contains(out, `"method":"GET"`) || !strings.Contains(out, `"duration"`) {
			t.Errorf("request log is missing fields: %s", out)
		}
		if strings.Contains(out, "/healthz") {
			t.Errorf("/healthz must log at debug only: %s", out)
		}
		if strings.Contains(out, "secret.person") {
			t.Errorf("resource must not be logged above debug: %s", out)
		}
	})
	t.Run("debug level adds healthz and resource", func(t *testing.T) {
		h, logs := newHandler(t, slog.LevelDebug)
		do(h, "GET", lookup("acct:secret.person@example.id"))
		do(h, "GET", "/healthz")
		out := logs.String()
		if !strings.Contains(out, "/healthz") || !strings.Contains(out, "secret.person@example.id") {
			t.Errorf("debug log is missing healthz or resource: %s", out)
		}
	})
}

func TestNewSetsTimeoutsAndPort(t *testing.T) {
	srv := New(testConfig, slog.New(slog.DiscardHandler))
	if !srv.DisableGeneralOptionsHandler {
		t.Error("OPTIONS * must reach the handler so it gets the robots header and a log line")
	}
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 ||
		srv.IdleTimeout <= 0 || srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes > 1<<20 {
		t.Errorf("server limits not set: %+v", srv)
	}
	if srv.ReadHeaderTimeout > time.Minute {
		t.Errorf("ReadHeaderTimeout = %v is too generous", srv.ReadHeaderTimeout)
	}
}

// Any account on the configured domain is answered and echoed; any account on
// another domain is a 404; anything that is not acct:user@domain is a 400.
func TestPropertyResourceParsing(t *testing.T) {
	h, _ := newHandler(t, slog.LevelInfo)
	userGen := rapid.StringMatching(`[A-Za-z0-9._+-]{1,20}`)
	otherGen := rapid.StringMatching(`[a-z][a-z0-9]{0,8}\.[a-z]{2,4}`).Filter(func(s string) bool { return s != "example.id" })

	rapid.Check(t, func(t *rapid.T) {
		user := userGen.Draw(t, "user")
		other := otherGen.Draw(t, "other")

		resource := "acct:" + user + "@example.id"
		rec := do(h, "GET", lookup(resource))
		if rec.Code != 200 {
			t.Fatalf("%q: status %d, want 200", resource, rec.Code)
		}
		var got descriptor
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Subject != resource {
			t.Fatalf("%q: subject = %q (err %v)", resource, got.Subject, err)
		}

		if code := do(h, "GET", lookup("acct:"+user+"@"+other)).Code; code != 404 {
			t.Fatalf("other domain %q: status %d, want 404", other, code)
		}
		if code := do(h, "GET", lookup(user+"@example.id")).Code; code != 400 {
			t.Fatalf("missing acct: scheme %q: status %d, want 400", user, code)
		}
		if code := do(h, "GET", lookup("acct:"+user+"@"+user+"@example.id")).Code; code != 400 {
			t.Fatalf("second separator %q: status %d, want 400", user, code)
		}
		if code := do(h, "GET", lookup("acct:"+user)).Code; code != 400 {
			t.Fatalf("missing domain %q: status %d, want 400", user, code)
		}
	})
}
