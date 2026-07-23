# README Refresh Design

## Goal

Refresh the public project documentation after the `v0.1.0` prerelease so a Go developer can quickly determine what the module supports, install the published version, send one template SMS, and handle indeterminate outcomes safely.

## Audience and language

- `README.md` is the canonical English document for the public Go ecosystem.
- `README_zh.md` is a complete Simplified Chinese mirror.
- Each file links to the other immediately below the title and badges.
- Both files use the same heading order, tables, code blocks, and destinations. Only explanatory prose and headings are translated.

## Header and status

Both documents start with:

1. The `go-sms` title.
2. Language links: `English` and `简体中文`.
3. Three verifiable badges:
   - `[![CI](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml/badge.svg)](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml)`
   - `[![Go Reference](https://pkg.go.dev/badge/github.com/feymanlee/go-sms.svg)](https://pkg.go.dev/github.com/feymanlee/go-sms)`
   - `[![Release](https://img.shields.io/github/v/release/feymanlee/go-sms?include_prereleases&sort=semver)](https://github.com/feymanlee/go-sms/releases)`
4. A concise project description.
5. A visible prerelease note stating that `v0.1.0` passed Go 1.25 and Go 1.26 CI, while credential-gated live SMS verification for all five Providers is not yet complete.

The README must not imply production readiness or claim that live Provider sends have passed.

## Mirrored section structure

The two documents use this exact section order:

1. Installation
2. Quick start
3. Supported Providers
4. Send Failure handling
5. Behavioral guarantees
6. Non-goals
7. Live Provider verification

### Installation

- Pin the published prerelease explicitly:

  ```sh
  go get github.com/feymanlee/go-sms@v0.1.0
  ```

- State that the module requires Go 1.25 or later.

### Quick start

- Retain the compile-checked Tencent Cloud example from `example_test.go`.
- Keep the code block identical in both README files.
- Show explicit Provider construction, E.164 Recipient parsing, a bounded Context, one Template Message, structured Failure inspection, ordinary error handling, and accepted Submission logging.
- Do not introduce credentials, real identifiers, or live-send output.

### Supported Providers

Use one table with all five Providers. Each row includes the Provider name, import path, required constructor fields, and Signature Reference policy. The exact policies are:

- Tencent Cloud: required.
- Alibaba Cloud: required.
- UCloud: optional.
- Qiniu Cloud: required.
- Yunpian: not used.

The explanatory text documents the existing `WithHTTPClient` and `WithEndpoint` options and the current endpoint-shape distinction between official-SDK and direct-HTTP adapters.

### Send Failure handling

Explain the seam precisely:

- Validation, an already-done Context, Provider-specific preflight rejection, encoding, and request construction return ordinary errors.
- After Provider invocation, explicit Provider decisions use the four definitive categories: `authentication`, `rate_limited`, `rejected`, and `unavailable`.
- Indeterminate post-invocation results use `unknown_outcome` and require reconciliation before retry.
- Callers inspect Failures through `failure.From`, `Category`, `Details`, and `UnknownOutcome`.
- Safe Details may contain Provider, Code, and RequestID; native error identity, Provider text, Recipient, template values, and request bodies are not exposed.

### Behavioral guarantees

State the version-one operational guarantees:

- Exactly one Provider is chosen explicitly per Send Attempt.
- No automatic retries, redirect following, routing, or failover.
- Provider configuration remains immutable after construction, and CI runs the test suite with the race detector; do not claim shared-instance concurrency coverage for all five Providers.
- Default HTTP clients retain standard proxy discovery and bounded timeouts.
- A Submission is acceptance evidence, not a delivery receipt.

### Non-goals

Preserve the current version-one exclusions, translated faithfully in each document. Do not add roadmap commitments.

### Live Provider verification

Link to `docs/integration-testing.md`. State that live tests are credential-gated, excluded from normal CI, and have not yet been completed for all five Providers for this prerelease.

## Synchronization rules

- Heading counts and order must match.
- Provider table rows and columns must match.
- Fenced code blocks must be byte-for-byte identical.
- Internal links must resolve from each file.
- Version, Go requirement, category names, and prerelease status must match exactly.
- Future documentation changes that alter public behavior must update both files in the same commit.

## Verification

Before committing the README implementation:

1. Confirm the two heading sequences match.
2. Confirm fenced code blocks are identical.
3. Check every local Markdown link resolves.
4. Run `go test -count=1 .` to compile the example without sending an SMS.
5. Run `go test -count=1 ./...`.
6. Run `git diff --check`.
7. Search the diff for credentials, Recipient values, template parameter values, raw request bodies, and complete Provider identifiers.

## Non-goals of this refresh

- No production Go changes.
- No Provider configuration or behavior changes.
- No new release or tag.
- No live SMS sends.
- No coverage, license, download-count, or other unverifiable badges.
- No generated documentation site.
