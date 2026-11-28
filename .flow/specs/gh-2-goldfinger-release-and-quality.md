# Goldfinger release and quality automation

## Conversation Evidence

> user (turn 4): "We won't need a `fly.toml` file because this repo will only be the source for building the go service, a separate project combining PocketID, S3 backing and our GHCR container definition will deploy these services together."
> user (turn 7): "I want to use release-please for tagged based release of the ko built image, I want attestation / SBOM, and because mise is our packager in this project, we can use mise tasks for appropriate steps that aren't covered by actions existing actions for attestation or ko build or release-please."
> user (turn 7): "We also need to make sure builds are available for amd64 and arm64"
> user (turn 7): "we should include Renovate config in the repo and lint that with hk, and we should use git hooks for pre-commit linting and pre-push checks, and jactionlint is also available for linting GitHub actions."
> user (turn 7): "Hk also includes some builtin go checks, use context7 to see what these are and decide what is appropriate for the solution."
> user (turn 9): "Coverage and sarif reports for security may be valuable to our small project."
> user (turn 9): "We don't want large frameworks or options that require a 3rd party platform"
> user (turn 9): "I'm planning on using CodeRabbit for reviews, we should also consider a reasonable config to add to the project."
> user (turn 10): "80% floor is locked in for coverage."
> user (turn 10): "No need for Trivy."
> user (turn 11): "We'll ignore CodeQL for now and add Codecov into the scope here."
> user (turn 12): "Codecov action can use OIDC to start with. If it doesn't work, well organise a token"
> user (turn 12): "Codecov status checks with ne authoritative"
> user (turn 12): "use the non-root base image"
> user (turn 12): "Start at 0.1.0 version"
> user (turn 14): "Take third option. I don't need a pre-push gate for coverage"
> user (spec amendment): "I have also removed the Hadolint definition from the Hk configuration, it's not required for Ko. The tooling files will need correct setup in gh-2"
> user (spec amendment 2): "Prefer actions for these [ko and gh] as they require a Github token and should never run locally as a mise task for this reason."
> user (spec amendment 3): "If that means a release cannot be released without linting, testing and build passing, then yes."
> user (spec amendment 4): "move to the documented `{{version}}`"
> user (strategy interview): "It's robust because it needs to pass through failure testing gates for it to be packaged into a release for the operator."

## Goal & Context

Operators pull a prebuilt Goldfinger image, so each release has to be trustworthy and traceable. This spec builds the automation that tests, scans, versions, packages, and attests the service from its repository. The repository is the source for building the Go service. A separate project combines PocketID, S3 backing, and the published image to deploy them together.

The tooling is mise for tasks, hk for git hooks, and GitHub Actions for CI. Anything not covered by an existing action runs as a mise task, except a step that needs a GitHub token: the image push and the attestations run only as workflow steps, because a mise task could be run locally. [paraphrase]

<!-- Source: 55% user / 35% [paraphrase] / 10% [inferred] -->

## Architecture & Data Models

- **mise tasks:** one entrypoint per local step (build, test, coverage, lint, security scans). Developers, hooks, and CI call the same tasks. No mise task needs a GitHub token. [paraphrase]
- **Workflow-only steps:** the image build and push (ko, set up by its action), the attestations (`actions/attest`), and the attestation verification (`gh`, which runners provide) run in the publish workflow and nowhere else. [paraphrase]
- **hk hooks:** pre-commit, commit-msg, and pre-push steps configured in the repository's hk file.
- **CI workflow (`ci.yml`):** lint, test, coverage upload, and SARIF upload on pull requests and pushes to the main branch, and when the publish workflow calls it.
- **Release workflow (`release.yml`):** release-please only. It opens and updates the release pull request and, when that is merged, creates the tag and GitHub release. It authenticates with a token the owner creates. [paraphrase]
- **Publish workflow (`publish.yml`):** starts on a `vX.Y.Z` tag push, runs the CI workflow as a gate on the tagged commit, then builds with ko, pushes to GHCR, attests, and verifies. It uses only the built-in `GITHUB_TOKEN`. [paraphrase]
- **Config files:** Codecov, Renovate, ko, release-please, and CodeRabbit.

## API Contracts

- Image: `ghcr.io/arrrgi/goldfinger`, one multi-platform index covering `linux/amd64` and `linux/arm64`, tagged `X.Y.Z`, `X.Y`, and `latest`. The git tag and GitHub release keep the `v` prefix (`vX.Y.Z`); the image tags drop it, as the metadata action documents. A pre-release tag gets only its full version tag, with no `X.Y` and no `latest`. [paraphrase]
- Base image: a non-root distroless static image, pinned in the ko configuration. [paraphrase]
- Workflow permissions for the publish job: `packages: write`, `id-token: write`, `attestations: write`. The CI gate adds `contents: read`, `id-token: write`, and `security-events: write` to its own jobs. [inferred]
- Release token: the release workflow authenticates release-please with the repository secret `RELEASE_PLEASE_TOKEN`, a fine-grained personal access token limited to this repository with contents, issues, and pull-requests write, the permissions release-please documents. [paraphrase]
- Version: the first release is `0.1.0`, driven by conventional commits. [paraphrase]

## Edge Cases & Constraints

- Events created with the default `GITHUB_TOKEN` do not start other workflows. release-please therefore uses a token the owner creates: its release pull request then runs CI, and its tag push starts the publish workflow. The token expires within a year, and an expired token silently stops releases. [inferred]
- Branch protection on the main branch must require the `Lint`, `Test`, and `Security scans` checks and the Codecov project status, so a failing change cannot merge into a release. The owner sets this once the checks have reported. [paraphrase]
- The tag and GitHub release page exist before the image does. A failing gate stops the image, not the tag. [inferred]
- SARIF upload to code scanning is free on public repositories and needs GitHub Advanced Security on private ones. This repository is public. [inferred]
- `govulncheck` needs network access, so it runs at pre-push and in CI, not at pre-commit. [inferred]
- Artifact attestations are free on public repositories. [inferred]
- Fork pull requests cannot use the Codecov OIDC token. The Codecov action then uploads tokenless, exposing no repository secret, so coverage from public fork pull requests still reaches the authoritative status check. Whether Codecov accepts tokenless uploads depends on the account's settings, which the first fork pull request confirms. [inferred]

## Acceptance Criteria

- **R1:** Build, test, coverage, lint, and scan steps are mise tasks, and CI and hooks call those tasks instead of repeating their commands. The image push, attestations, and attestation verification need a GitHub token, so they are workflow steps and no mise task runs them. Errors: a missing tool fails the task with the tool's name. [paraphrase]
- **R2:** The coverage task runs the tests with `-race`, writes a coverage profile, and prints the total percentage. It never fails on the percentage. Errors: a failing test fails the task. [paraphrase]
- **R3:** CI runs on pull requests, on pushes to the main branch, and when the publish workflow calls it: the lint steps, the tests with `-race`, and the Codecov upload using OIDC. If the OIDC upload is rejected, the workflow switches to a repository token secret. Errors: an upload failure reports the Codecov error and does not hide a test failure. [paraphrase]
- **R4:** The Codecov project status check is the authoritative coverage gate, with an 80% target and the program entrypoint excluded from the measurement. No coverage threshold exists in mise tasks or git hooks. [paraphrase]
- **R5:** Pre-commit runs Go format, golangci-lint, `go mod tidy` drift, workflow lint, Renovate config validation, and the existing markdown and JSON checks. Commit-msg enforces conventional commits. Pre-push runs the tests with `-race` and `govulncheck`. Errors: a failing step blocks the commit or push, and hooks are never bypassed. [paraphrase]
- **R6:** `govulncheck` and `zizmor` results upload as SARIF to GitHub code scanning with a distinct category per tool, on pull requests and pushes to the main branch. Errors: a scan that cannot upload fails visibly. [paraphrase]
- **R7:** release-please runs for a Go project on the main branch with conventional commits, opens a release pull request that runs CI, and creates the tag and GitHub release on merge. The first release is `0.1.0`. [paraphrase]
- **R8:** The publish workflow starts when release-please pushes a `vX.Y.Z` tag and runs on the tagged commit. It first runs the CI workflow (lint, build, tests with `-race`, and scans) and publishes only if that passed, so an image never ships from a failing commit. ko, installed and logged in to GHCR by its action, builds `linux/amd64` and `linux/arm64` in one run, with no CI matrix, and pushes the index to GHCR with the tags `X.Y.Z`, `X.Y`, and `latest`, which the metadata action computes from the git tag. Errors: a failed gate or step fails the run and later steps do not run; a failure after the push leaves the tags on the pushed image until a re-run of the same tag succeeds. [paraphrase] [strategy:Automated testing and packaging]
- **R9:** ko's SPDX SBOM is written to disk. The publish job attests build provenance and that SBOM for the image digest using `actions/attest`, with only the permissions listed under API Contracts. A workflow step verifies both attestations with `gh attestation verify`. Errors: a failed attestation fails the job. [paraphrase]
- **R10:** A Renovate configuration exists and is validated by a hook. Workflows are linted with jactionlint. [paraphrase]
- **R11:** Every GitHub Action reference is pinned to a full commit SHA, and workflows pass `zizmor`. In CI, zizmor runs online with the job's own `github.token` (passed as `ZIZMOR_GITHUB_TOKEN` on the lint and SARIF steps only), so its known-vulnerable-action, impostor-commit and ref-confusion audits run; locally it stays offline. [inferred]
- **R12:** A CodeRabbit configuration at the repository root tells reviewers to flag new third-party dependencies, weakened or rewritten tests, unpinned actions, and removal of the README's apex warning, and enables its golangci-lint and actionlint checks. [paraphrase]
- **R13:** The tooling files already in the repository (the mise, hk, Biome, and rumdl configuration and the editor settings) are committed and set up correctly: `mise install` provides every tool they pin, and the pre-commit and pre-push steps run cleanly on the repository. Errors: a step that cannot run fails the hook and names the missing tool. [paraphrase]

## Boundaries

- No Trivy scan and no CodeQL for now. [paraphrase]
- No coverage gate in mise tasks, git hooks, or pre-push. [paraphrase]
- No Dockerfile and no CI build matrix, because ko builds both platforms. [paraphrase]
- No mise task pushes an image, creates an attestation, or verifies one, because each needs a GitHub token. [paraphrase]
- No fly.toml or compose file for deployment in this spec. Those belong to the documentation spec. [paraphrase]
- The existing `LICENSE` stays as is. [paraphrase]
- Service behavior belongs to the WebFinger service spec. [inferred]

## Decision Context

ko cross-compiles for each platform and assembles the index without QEMU, and it writes an SPDX SBOM by default, so one job produces both platforms. [inferred]

`actions/attest` replaces `actions/attest-sbom`, which is deprecated, and `actions/attest-build-provenance`, which wraps it. One pinned action covers both attestations. [inferred]

`hadolint` is not used: the design has no Dockerfile because ko builds the image, so the hk configuration does not include it. [paraphrase]

hk's Go builtins used: `go_fmt`, `golangci_lint`, and `gomod_tidy` at commit, and `go_vuln_check` at push. `go_vet`, `staticcheck`, `revive`, `err_check`, and `go_sec` are left out because golangci-lint covers them. [inferred]

`jactionlint`, `zizmor`, `pinact`, and `check_conventional_commit` are existing hk builtins. conventional commits are what release-please versions from. [inferred]

ko writes one SPDX SBOM per platform image and one for the index, so the index SBOM is the one attested for the index digest. [inferred]

`actions/attest` lists `artifact-metadata: write` only for the storage record, which GitHub creates for organization-owned repositories. This repository is user-owned, so the publish job sets `create-storage-record: false` and omits that permission. [inferred]

`jactionlint` overlaps `zizmor` on dangerous triggers, template injection, unpinned actions, artifact credentials, and permissions, and reports a missing job timeout, which zizmor does not. `zizmor` reports more of the security findings (a second template injection, excessive permissions, anonymous definitions) and writes the SARIF that code scanning needs, so both stay: `jactionlint` as the workflow linter and `zizmor` as the security audit. [inferred]

Three workflows keep each one short: CI checks, release-please versions, and publish ships. Publish starts from the tag push, so it runs on the tagged commit and the checks, the build, and the provenance describe the same commit; a failed run is re-run from the Actions page. Only release-please needs a created token, because the built-in token's tag and pull request would start no workflow and the release pull request could never satisfy required checks. [inferred]

The metadata action computes the tags from the pushed git tag with its documented `{{version}}` and `{{major}}.{{minor}}` patterns, so `v1.2.3` becomes `1.2.3` and `1.2`, and its default `latest` handling adds `latest` for a stable release only. With no image name it emits bare tag names, which `ko build --tags` takes, so no shell slicing is needed. [inferred]

Codecov is authoritative so the percentage is defined in one place. A second threshold in mise would drift from it. [paraphrase]

Siblings from the same origin: "Goldfinger WebFinger service" (the code under test and packaged) and "Goldfinger documentation and deployment examples".
