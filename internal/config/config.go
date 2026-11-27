// Package config reads Goldfinger's settings from flags and environment
// variables and validates them into one Config value.
//
// Precedence is flag over environment variable over default.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// Action says what the caller should do with the parsed arguments.
type Action int

const (
	// ActionRun starts the server.
	ActionRun Action = iota
	// ActionHealthcheck probes a running server and exits.
	ActionHealthcheck
	// ActionHelp prints the help text and exits successfully.
	ActionHelp
	// ActionUsage prints the usage summary and exits successfully.
	ActionUsage
)

// Config is the validated settings value.
type Config struct {
	IssuerURL   string
	AdminEmail  string
	EmailDomain string // lower-cased text after the last "@" of AdminEmail
	Port        int
	LogLevel    slog.Level
	LogFormat   string // "json" or "text"
}

type setting struct {
	flag     string
	env      string
	def      string
	required bool
	desc     string
}

const (
	flagIssuerURL  = "issuer-url"
	flagAdminEmail = "issuer-admin-email"
	flagPort       = "port"
	flagLogLevel   = "log-level"
	flagLogFormat  = "log-format"
)

var settings = []setting{
	{flag: flagIssuerURL, env: "ISSUER_URL", required: true,
		desc: "OIDC issuer URL returned in the WebFinger response, used exactly as given (https only)"},
	{flag: flagAdminEmail, env: "ISSUER_ADMIN_EMAIL", required: true,
		desc: "email address entered at Tailscale sign-up; its domain is the only domain answered"},
	{flag: flagPort, env: "PORT", def: "8080",
		desc: "TCP port to listen on"},
	{flag: flagLogLevel, env: "LOG_LEVEL", def: "info",
		desc: "log level: debug, info, warn or error"},
	{flag: flagLogFormat, env: "LOG_FORMAT", def: "json",
		desc: "log format: json or text"},
}

// Parse reads args (without the program name) and getenv into a Config.
// The returned Config is only meaningful for ActionRun and, for its Port
// field, ActionHealthcheck.
func Parse(args []string, getenv func(string) string) (Config, Action, error) {
	fs := flag.NewFlagSet("goldfinger", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	values := make(map[string]*string, len(settings))
	for _, s := range settings {
		def := s.def
		if v := getenv(s.env); v != "" {
			def = v
		}
		values[s.flag] = fs.String(s.flag, def, s.desc)
	}
	healthcheck := fs.Bool("healthcheck", false, "probe /healthz on 127.0.0.1 and exit 0 when healthy")
	usage := fs.Bool("usage", false, "print a usage summary and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, ActionHelp, nil
		}
		return Config{}, ActionRun, err
	}
	if fs.NArg() > 0 {
		return Config{}, ActionRun, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *usage {
		return Config{}, ActionUsage, nil
	}

	port, err := parsePort(*values[flagPort])
	if err != nil {
		return Config{}, ActionRun, err
	}
	if *healthcheck {
		return Config{Port: port}, ActionHealthcheck, nil
	}

	level, err := parseLevel(*values[flagLogLevel])
	if err != nil {
		return Config{}, ActionRun, err
	}
	format := strings.ToLower(*values[flagLogFormat])
	if format != "json" && format != "text" {
		return Config{}, ActionRun, settingError(flagLogFormat, "must be json or text, got %q", *values[flagLogFormat])
	}

	issuer, host, err := parseIssuer(*values[flagIssuerURL])
	if err != nil {
		return Config{}, ActionRun, err
	}
	email, domain, err := parseEmail(*values[flagAdminEmail])
	if err != nil {
		return Config{}, ActionRun, err
	}
	if !domainMatches(host, domain) {
		return Config{}, ActionRun, fmt.Errorf(
			"--%s (%s): host %q must equal or be a subdomain of the %s domain %q",
			flagIssuerURL, envOf(flagIssuerURL), host, envOf(flagAdminEmail), domain)
	}

	return Config{
		IssuerURL:   issuer,
		AdminEmail:  email,
		EmailDomain: domain,
		Port:        port,
		LogLevel:    level,
		LogFormat:   format,
	}, ActionRun, nil
}

// Help returns the full help text printed for -h and --help.
func Help() string {
	var b strings.Builder
	b.WriteString("Goldfinger answers Tailscale's OIDC WebFinger lookup for a custom identity provider.\n\n")
	b.WriteString(Usage())
	b.WriteString("\nEvery setting is a flag that defaults from its environment variable.\n")
	b.WriteString("A flag overrides the variable, which overrides the default.\n\n")
	b.WriteString("Settings:\n")
	for _, s := range settings {
		fmt.Fprintf(&b, "  --%s  (env %s)", s.flag, s.env)
		switch {
		case s.required:
			b.WriteString("  required")
		default:
			fmt.Fprintf(&b, "  default %q", s.def)
		}
		fmt.Fprintf(&b, "\n      %s\n", s.desc)
	}
	b.WriteString("\nOther flags:\n")
	b.WriteString("  --healthcheck  probe /healthz on 127.0.0.1 at the configured port; exit 0 when healthy\n")
	b.WriteString("  --usage        print the usage summary\n")
	b.WriteString("  -h, --help     print this help\n")
	return b.String()
}

// Usage returns the short usage summary printed for --usage.
func Usage() string {
	var b strings.Builder
	b.WriteString("Usage: goldfinger --issuer-url URL --issuer-admin-email EMAIL")
	b.WriteString(" [--port N] [--log-level LEVEL] [--log-format FORMAT]\n")
	b.WriteString("       goldfinger --healthcheck [--port N]\n")
	b.WriteString("       goldfinger --usage | --help\n\n")
	b.WriteString("Settings (flag, environment variable, default):\n")
	for _, s := range settings {
		def := "required"
		if !s.required {
			def = s.def
		}
		fmt.Fprintf(&b, "  --%-19s %-19s %s\n", s.flag, s.env, def)
	}
	return b.String()
}

func envOf(flagName string) string {
	for _, s := range settings {
		if s.flag == flagName {
			return s.env
		}
	}
	return ""
}

func settingError(flagName, format string, args ...any) error {
	return fmt.Errorf("--%s (%s): %s", flagName, envOf(flagName), fmt.Sprintf(format, args...))
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, settingError(flagPort, "must be a number from 1 to 65535, got %q", raw)
	}
	return port, nil
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, settingError(flagLogLevel, "must be debug, info, warn or error, got %q", raw)
}

// parseIssuer validates the issuer URL and returns it unchanged, with its
// lower-cased host name. The URL is kept verbatim because Tailscale compares
// it exactly with the issuer in the identity provider's discovery document.
//
// Error messages never echo the URL: it may carry credentials, which must not
// reach logs or the public WebFinger response.
func parseIssuer(raw string) (issuer, host string, err error) {
	if raw == "" {
		return "", "", settingError(flagIssuerURL, "is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Never include the parser's error: it can quote part of the URL,
		// such as a password read as a port.
		return "", "", settingError(flagIssuerURL, "is not a valid URL")
	}
	if u.User != nil {
		return "", "", settingError(flagIssuerURL, "must not contain credentials")
	}
	if strings.ContainsAny(raw, "?#") {
		return "", "", settingError(flagIssuerURL, "must not have a query or fragment")
	}
	if u.Scheme != "https" {
		return "", "", settingError(flagIssuerURL, "must use https")
	}
	if u.Hostname() == "" {
		return "", "", settingError(flagIssuerURL, "must include a host")
	}
	return raw, strings.ToLower(u.Hostname()), nil
}

// parseEmail accepts only a bare address such as "admin@example.id", not a
// display-name form like "Admin <admin@example.id>".
func parseEmail(raw string) (email, domain string, err error) {
	if raw == "" {
		return "", "", settingError(flagAdminEmail, "is required")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return "", "", settingError(flagAdminEmail, "must be a bare email address such as admin@example.id, got %q", raw)
	}
	at := strings.LastIndex(raw, "@")
	return raw, strings.ToLower(raw[at+1:]), nil
}

// domainMatches reports whether host equals domain or is a subdomain of it.
// The standard library has no public-suffix list, so this is a plain
// label-boundary suffix test.
func domainMatches(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}
