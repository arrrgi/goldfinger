package config

import (
	"log/slog"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var validEnv = map[string]string{
	"ISSUER_URL":         "https://oidc.example.id",
	"ISSUER_ADMIN_EMAIL": "admin@example.id",
}

func TestParseDefaultsFromEnv(t *testing.T) {
	cfg, action, err := Parse(nil, env(validEnv))
	if err != nil || action != ActionRun {
		t.Fatalf("Parse() = %v, %v; want run, nil", action, err)
	}
	want := Config{
		IssuerURL:   "https://oidc.example.id",
		AdminEmail:  "admin@example.id",
		EmailDomain: "example.id",
		Port:        8080,
		LogLevel:    slog.LevelInfo,
		LogFormat:   "json",
	}
	if cfg != want {
		t.Errorf("Parse() = %+v, want %+v", cfg, want)
	}
}

func TestParsePrecedenceFlagOverEnvOverDefault(t *testing.T) {
	e := map[string]string{
		"ISSUER_URL": "https://oidc.example.id", "ISSUER_ADMIN_EMAIL": "admin@example.id",
		"PORT": "9000", "LOG_LEVEL": "warn", "LOG_FORMAT": "text",
	}
	cfg, _, err := Parse(nil, env(e))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.LogLevel != slog.LevelWarn || cfg.LogFormat != "text" {
		t.Errorf("env should override defaults, got %+v", cfg)
	}

	cfg, _, err = Parse([]string{"--port", "9100", "--log-level=debug", "--log-format", "json",
		"--issuer-url", "https://id.example.id/", "--issuer-admin-email=ops@example.id"}, env(e))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9100 || cfg.LogLevel != slog.LevelDebug || cfg.LogFormat != "json" ||
		cfg.IssuerURL != "https://id.example.id/" || cfg.AdminEmail != "ops@example.id" {
		t.Errorf("flags should override env, got %+v", cfg)
	}
}

func TestParseKeepsIssuerURLVerbatim(t *testing.T) {
	e := map[string]string{"ISSUER_URL": "https://oidc.example.id/", "ISSUER_ADMIN_EMAIL": "admin@example.id"}
	cfg, _, err := Parse(nil, env(e))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IssuerURL != "https://oidc.example.id/" {
		t.Errorf("IssuerURL = %q, want trailing slash preserved", cfg.IssuerURL)
	}
}

func TestParseActions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want Action
		port int
	}{
		{"help short", []string{"-h"}, nil, ActionHelp, 0},
		{"help long", []string{"--help"}, nil, ActionHelp, 0},
		{"usage", []string{"--usage"}, nil, ActionUsage, 0},
		{"healthcheck needs no issuer", []string{"--healthcheck"}, map[string]string{"PORT": "9001"}, ActionHealthcheck, 9001},
		{"healthcheck flag port", []string{"--healthcheck", "--port", "9002"}, nil, ActionHealthcheck, 9002},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, action, err := Parse(tt.args, env(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if action != tt.want {
				t.Errorf("action = %v, want %v", action, tt.want)
			}
			if tt.port != 0 && cfg.Port != tt.port {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.port)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range validEnv {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	without := func(k string) map[string]string {
		m := with(k, "")
		delete(m, k)
		return m
	}
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string // substring naming the setting
	}{
		{"issuer missing", nil, without("ISSUER_URL"), "ISSUER_URL"},
		{"issuer http", nil, with("ISSUER_URL", "http://oidc.example.id"), "must use https"},
		{"issuer no host", nil, with("ISSUER_URL", "https://"), "must include a host"},
		{"issuer query", nil, with("ISSUER_URL", "https://oidc.example.id/?a=b"), "query or fragment"},
		{"issuer fragment", nil, with("ISSUER_URL", "https://oidc.example.id/#x"), "query or fragment"},
		{"issuer credentials", nil, with("ISSUER_URL", "https://user:hunter2@oidc.example.id"), "credentials"},
		{"issuer username only", nil, with("ISSUER_URL", "https://token@oidc.example.id"), "credentials"},
		{"issuer unparsable", nil, with("ISSUER_URL", "https://exa mple.id"), "ISSUER_URL"},
		{"email missing", nil, without("ISSUER_ADMIN_EMAIL"), "ISSUER_ADMIN_EMAIL"},
		{"email display name", nil, with("ISSUER_ADMIN_EMAIL", "Admin <admin@example.id>"), "bare email"},
		{"email no at", nil, with("ISSUER_ADMIN_EMAIL", "example.id"), "bare email"},
		{"domain mismatch", nil, with("ISSUER_ADMIN_EMAIL", "admin@other.id"), "subdomain"},
		{"suffix without dot boundary", nil, with("ISSUER_URL", "https://evilexample.id"), "subdomain"},
		{"bad port text", nil, with("PORT", "abc"), "PORT"},
		{"bad port range", []string{"--port", "70000"}, validEnv, "--port"},
		{"bad port zero", []string{"--port=0"}, validEnv, "--port"},
		{"bad level", nil, with("LOG_LEVEL", "verbose"), "LOG_LEVEL"},
		{"bad format", nil, with("LOG_FORMAT", "xml"), "LOG_FORMAT"},
		{"unknown flag", []string{"--nope"}, validEnv, "nope"},
		{"positional argument", []string{"extra"}, validEnv, "unexpected argument"},
		{"healthcheck bad port", []string{"--healthcheck", "--port", "x"}, nil, "--port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Parse(tt.args, env(tt.env))
			if err == nil {
				t.Fatalf("Parse() succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("error %q should be one line", err)
			}
		})
	}
}

// A rejected issuer URL must never be echoed back, because it may hold secrets.
func TestParseErrorsNeverEchoIssuerCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://user:hunter2@oidc.example.id",
		"http://user:hunter2@oidc.example.id",
		"https://user:hunter2@oidc.example.id/?a=b",
		"https://user:hunter2@exa mple.id",
		"https://user:hunter2@other.test",
	} {
		e := map[string]string{"ISSUER_URL": raw, "ISSUER_ADMIN_EMAIL": "admin@example.id"}
		_, _, err := Parse(nil, env(e))
		if err == nil {
			t.Errorf("%q was accepted", raw)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%q: error leaks the credentials: %v", raw, err)
		}
	}
}

func TestHelpAndUsageListEverySetting(t *testing.T) {
	for name, text := range map[string]string{"help": Help(), "usage": Usage()} {
		for _, s := range settings {
			if !strings.Contains(text, "--"+s.flag) || !strings.Contains(text, s.env) {
				t.Errorf("%s output is missing %s / %s", name, s.flag, s.env)
			}
		}
	}
	if !strings.Contains(Help(), "--healthcheck") {
		t.Error("help should list --healthcheck")
	}
}

func labelGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9]{0,8}`)
}

func domainGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		labels := rapid.SliceOfN(labelGen(), 2, 4).Draw(t, "labels")
		return strings.Join(labels, ".")
	})
}

// The issuer host may equal the email domain or sit beneath it on a label
// boundary; anything else, including a bare string suffix, is rejected.
func TestPropertyIssuerHostMustBeInEmailDomain(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		domain := domainGen().Draw(t, "domain")
		sub := labelGen().Draw(t, "sub")
		cases := []struct {
			host string
			ok   bool
		}{
			{domain, true},
			{sub + "." + domain, true},
			{strings.ToUpper(domain), true},
			{sub + domain, false}, // shares the suffix but not a label boundary
			{"x." + sub + "." + domain + ".invalid", false},
		}
		for _, c := range cases {
			_, _, err := Parse([]string{
				"--issuer-url", "https://" + c.host,
				"--issuer-admin-email", "user@" + domain,
			}, env(nil))
			if (err == nil) != c.ok {
				t.Fatalf("host %q with domain %q: err = %v, want accepted = %v", c.host, domain, err, c.ok)
			}
		}
	})
}
