# Goldfinger WebFinger service

## Conversation Evidence

> user (turn 1): "I'm planning on using PocketID as my identity provider for Tailscale and I want to host it on Fly.io."
> user (turn 1): "My challenge is that I don't have anywhere to host a static webfinger file for the OIDC lookup that Tailscale performs when you create a new Tailnet and choose a custom identity provider."
> user (turn 1): "I want to decide between building a static Go binary with `go/http` that serves the Webfinger request, or the https://github.com/PierreZ/goStatic Docker container. I also want to be able to provide directives for robots and HTTP redirects."
> user (turn 1): "2) serving a rudimentary 404 page with as HTML/CSS only"
> user (turn 3): "I want this to be a small open source utility under MIT license that people can self host as a container or run in Fly or another PaaS that supports CNAME flattening"
> user (turn 4): "The only content the service will respond with is the OIDC material and a simple 404 page that uses built in cross browser CSS support for light/dark mode."
> user (turn 4): "The pattern is to provide one TLD for OIDC (Webfinger and the PocketID app), and it can be the identify provider for another domains services that use custom OIDC."
> user (turn 6): "Can we add the healthcheck to the Go service?"
> user (turn 6): "We won't need to allow multiple domains with this service as hosting in Fly.io will be on our identity domain TLD. And the utility has a small enough memory footprint that multiple instances could be self-hosted with separate PORT and the email address to hand back in the OIDC response."
> user (turn 6): "The TLD on both should be checked to match so if the user provides ISSUER_URL of "https://oidc.example.id" then they are required to provide an email address as variable such as "admin@example.id""
> user (turn 6): "What log handling should be built in? Is there a standard library for this that provide similar to a syslog output and provides info, verbose and debug type levels?"
> user (turn 7): "We'll call the email variable ISSUER_ADMIN_EMAIL."
> user (turn 7): "so someone could probably run (for testing) the Go service on a machine and query switch usage with both `--help` and `--usage`."
> user (turn 8): "1) We won't use cobra, stdlib flag is sufficient"
> user (turn 8): "The Direct assertion test cases here don't need to be exhaustive, just capture the big things."
> user (turn 9): "I want to keep fuzzing lightweight, not deep"
> user (strategy interview): "It's robust because it needs to pass through failure testing gates for it to be packaged into a release for the operator. The testing gates ensure the application operates as expected and provides feedback if it's not configured correctly or experiencing any errors."

## Goal & Context

Operators who create a Tailscale account with their own OIDC provider, for example PocketID, must publish a WebFinger endpoint on the domain of their sign-up email. Today that means running and configuring a general-purpose web server such as Caddy or Nginx. Goldfinger is a single-purpose Go service that answers only that lookup and runs anywhere a container runs, so the operator needs no web-server knowledge.

The audience is operators who are comfortable with PaaS platforms, containers, and identity providers. The MVP is the WebFinger response, a 404 page, robots directives, a health endpoint, and logging. The project is open source under the MIT license.

<!-- Source: 60% user / 35% [paraphrase] / 5% [inferred] -->

## Architecture & Data Models

One statically linked Go binary built from the standard library only. It holds no state and makes no outbound calls.

- **Config:** reads flags and environment variables into one validated settings value at startup.
- **WebFinger handler:** answers the lookup from that settings value.
- **Static responses:** the 404 page, `robots.txt`, and the health endpoint, with the 404 page embedded in the binary.
- **Logging:** structured logs on standard output.

The settings value holds the issuer URL, the admin email, the email domain (the text after the last `@`), the port, the log level, and the log format.

<!-- Source: 20% user / 40% [paraphrase] / 40% [inferred] -->

## API Contracts

All routes accept GET and HEAD. Every response carries `X-Robots-Tag: noindex, nofollow, noarchive`. [inferred]

- `GET /.well-known/webfinger?resource=acct:<user>@<email domain>[&rel=<rel>]` returns `200` with `Content-Type: application/jrd+json` and `Access-Control-Allow-Origin: *`. [inferred]
- `GET /robots.txt` returns `200` with `text/plain` and the two lines `User-agent: *` and `Disallow: /`. [inferred]
- `GET /healthz` returns `200` with the body `ok`. [paraphrase]
- Any other path returns `404` with an HTML page. [paraphrase]

The WebFinger success body is exactly this, where `subject` echoes the requested `resource` and `href` is the configured issuer URL. The fields shown are the whole contract.

```json
{
  "subject": "acct:admin@example.id",
  "links": [
    {
      "rel": "http://openid.net/specs/connect/1.0/issuer",
      "href": "https://oidc.example.id"
    }
  ]
}
```

Settings. Each has a flag and an environment variable. [paraphrase]

| Flag | Variable | Required | Default |
|---|---|---|---|
| `--issuer-url` | `ISSUER_URL` | yes | none |
| `--issuer-admin-email` | `ISSUER_ADMIN_EMAIL` | yes | none |
| `--port` | `PORT` | no | `8080` [inferred] |
| `--log-level` | `LOG_LEVEL` | no | `info` [inferred] |
| `--log-format` | `LOG_FORMAT` | no | `json` [inferred] |

Also `--healthcheck` (self-check), `--usage`, and `-h`/`--help`.

## Edge Cases & Constraints

- Tailscale requires the issuer in the WebFinger response to match the issuer in the identity provider's discovery document exactly, so the service uses `ISSUER_URL` as given, including any trailing slash. [inferred]
- The standard library has no public-suffix list. "Match" between issuer and email domain therefore means the issuer host equals the email domain or ends with `.` plus the email domain. [inferred]
- Tailscale's docs say it reads the issuer from WebFinger only during initial setup. [inferred]
- The WebFinger `resource` contains an email address. The service logs it only at debug level. [inferred]
- The HTTP server sets header, read, and idle timeouts and a header size cap to blunt slow-client abuse. [inferred]
- The image has no shell, so the health check must be a flag on the binary. [inferred]

## Acceptance Criteria

- **R1:** A lookup `GET /.well-known/webfinger?resource=acct:<user>@<email domain>` returns `200`, `application/jrd+json`, and the body under API Contracts. Errors: missing or non-`acct:` `resource` returns `400`; a `resource` whose domain differs from the `ISSUER_ADMIN_EMAIL` domain returns `404`; methods other than GET and HEAD return `405` with `Allow: GET, HEAD`. [paraphrase] [strategy:A small, robust application]
- **R2:** A `rel` parameter equal to the issuer rel, or absent, returns the issuer link. Any other `rel` returns `200` with an empty `links` array. Errors: no error surface beyond R1. [inferred]
- **R3:** Startup exits non-zero with a one-line message naming the setting when `ISSUER_URL` is missing, is not `https`, has no host, or has a query or fragment; when `ISSUER_ADMIN_EMAIL` is missing or is not a bare address; or when the issuer host is neither equal to nor a subdomain of the email domain. [paraphrase]
- **R4:** Each setting is a command-line flag that defaults from its environment variable, using only the standard library `flag` package. `-h`/`--help` and `--usage` print the settings. Errors: an unknown flag, or an invalid port, log level, or log format, prints a message and exits non-zero. [paraphrase]
- **R5:** A flag value overrides its environment variable, which overrides the default. Errors: no error surface beyond R4. [inferred]
- **R6:** Two instances on one host with different `PORT`, `ISSUER_URL`, and `ISSUER_ADMIN_EMAIL` run independently, because the service keeps no shared state. Errors: a port already in use makes startup exit non-zero with the address in the message. [paraphrase]
- **R7:** Unknown paths return status `404` with an embedded page built from HTML and CSS only, which follows the viewer's light or dark preference through built-in CSS support. Errors: no error surface beyond the status. [paraphrase]
- **R8:** `/robots.txt` returns the `Disallow: /` policy and every response carries the `X-Robots-Tag` header. The policy has no setting. [inferred]
- **R9:** `/healthz` returns `200`. `--healthcheck` requests `/healthz` on `127.0.0.1` at the configured port and exits `0` on `200` and `1` otherwise. Errors: an unreachable server exits `1`. [paraphrase]
- **R10:** Logs use `log/slog` on standard output with levels debug, info, warn, and error (`LOG_LEVEL`, default info) and formats json or text (`LOG_FORMAT`, default json). Startup logs the sanitized settings, each request logs method, path, status, and duration, `/healthz` logs at debug only, and `resource` values appear at debug only. [paraphrase]
- **R11:** The binary depends on the Go standard library only, is built with cgo off, and runs without root privileges. Test-only modules are allowed and are never linked into the binary. [paraphrase]
- **R12:** Tests cover the failure modes before a release can ship. Table tests cover config validation and handler statuses through `httptest`. Property tests with `rapid`, at its default 100 checks, cover the issuer/email domain rule and `resource` parsing. Direct assertions cover only the large behaviors, the `404` status and the `robots.txt` content. One integration test, behind a build tag, starts the real server on an ephemeral port. The suite passes with `-race`. [strategy:A small, robust application]

## Boundaries

- The service owns the origin it listens on. It does not reverse proxy, terminate TLS, or share an apex with a website. [paraphrase]
- One configured domain per instance. Multiple domains need multiple instances. [paraphrase]
- No redirects. HTTP to HTTPS redirection belongs to the hosting platform. [paraphrase]
- No CLI framework, and no syslog output. Logs go to standard output. [paraphrase]
- No tests of CSS or markup details, and no deep fuzzing. [paraphrase]
- CI, releases, and image publishing belong to the release automation spec. README and deployment examples belong to the documentation spec. [inferred]

## Decision Context

Go with the standard library replaces the goStatic container because goStatic serves files and offers no redirects beyond HTTPS promotion, no custom 404 page (its fallback file targets single-page apps), and request logging that is off by default. [inferred]

`log/slog` gives leveled, structured logging in the standard library. The `log/syslog` package is Unix-only and effectively frozen, and container platforms capture standard output. [inferred]

`rapid` is the property-testing library. It is MPL-2.0 licensed, has no dependencies, and is imported only from test files, so it never enters the binary or its SBOM. The standard library's `testing/quick` does not shrink failures. [inferred]

Siblings from the same origin: "Goldfinger release and quality automation" and "Goldfinger documentation and deployment examples".

## Parked unknowns

- **Email claim match:** whether Tailscale requires the identity provider's `email` claim to equal the email entered at sign-up. Tailscale's docs say to sign in with that email. A live sign-up against PocketID resolves it.
