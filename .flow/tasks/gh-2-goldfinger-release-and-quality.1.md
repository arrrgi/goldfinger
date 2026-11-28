---
satisfies: [R1, R2, R3, R4, R5, R6, R7, R8, R9, R10, R11, R12, R13]
---
# gh-2-goldfinger-release-and-quality.1 Implement Goldfinger release and quality automation

## Description
TBD

## Acceptance
Every R-ID in the parent spec's ## Acceptance Criteria is satisfied; judge this task against the spec's criteria directly.

## Done summary
Set up the release and quality automation (R1-R13): pinned mise tools and tasks (build, test, coverage, lint, vuln and SARIF scans, setup), hk hooks (pre-commit, check, commit-msg, pre-push), golangci-lint, biome, rumdl and editorconfig settings, ko, release-please, Codecov, Renovate and CodeRabbit config, and three workflows: ci.yml (lint, test with Codecov OIDC and token fallback, SARIF scans), release.yml (release-please with RELEASE_PLEASE_TOKEN) and publish.yml (CI gate on the tagged commit, then a ko multi-platform build, provenance and SBOM attestations, and attestation verification). All actions are pinned to full SHAs, every step is named, and mise.toml holds no task that needs a GitHub token.

Tests: mise run test, coverage (93.7% total, -race, integration tag), vuln (no vulnerabilities), lint (nine steps green) all pass locally. jactionlint and zizmor report nothing on the workflows. The commit-msg hook rejects a non-conventional message and accepts a conventional one. Nothing has run on GitHub yet.

OVERRIDDEN: 2 findings accepted by the owner. Alias race: a re-run or late finish of an older release can move `latest` and `X.Y` backwards; the owner said "No, don't handle this" and "Mark alias race and the required-checks time as accepted now". Required checks timing: the Lint, Test, Security scans and Codecov checks cannot be required on main until CI has reported once, so the owner sets them after the first CI run; accepted by the owner in the same words.

Review rounds: round 1 (three reviewers) NEEDS_WORK, ten findings, fixed or declined with reasons; round 2 NEEDS_WORK, four findings; round 3 NEEDS_WORK after the three-workflow redesign, three findings (token secret now set, the two above accepted).

stage: impl-review - ran [43e3abc..38e1c65] NEEDS_WORK -> OVERRIDDEN by the owner (model: gpt-6.2-sol)
stage: completion-review - skipped(policy: single-task, per-task review override covers spec surface)

stage: plan-sync - skipped(config: planSync.enabled != true)
## Evidence
- Commits: 50111eb971efb2c60e16ef021ff51a434ede9741, 569fedf2a2dbba1224eadd123b1f40fc82e3b61d, a7b7760cd5e0b64c0ea72b31e18969a69bc518a4, 82d8476c81ac72f37370e0db5ed459c61e5bdf0d, 5ad7fa530ba0efd4049bfbfd45fed4f4dfe8a321, 172ffef21f3d5ac34640b4ed7242b0c3892249e9, 68b678904cc8bb79926dfbdf11ca4e451e645a12, f918c2768ec174640b1e2d87fa7abfe0df0b2ea5, 38e1c6566e36ffd56b5ab5d9ba3e52d82d281405
- Tests: mise run lint; mise run test; mise run coverage; mise run vuln
- PRs: