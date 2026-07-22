# Send Failure Normalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mutable root error record and distributed Provider error assembly with an immutable, sealed `failure.Failure` and a Provider-bound semantic factory.

**Architecture:** `Sender.Send` keeps its standard `(Submission, error)` interface. A new public `failure` package owns the complete Send Failure interface and implementation; Provider adapters retain native fact extraction and category mapping, then construct normalized failures through a bound `failure.Factory`. Errors before the request crosses the Provider seam remain ordinary errors.

**Tech Stack:** Go 1.25, standard `context`/`errors` packages, existing Provider SDKs and `httptest` adapters.

## Global Constraints

- This is a breaking pre-release interface change; the repository has no local or remote version tags, so the module path remains exactly `github.com/feymanlee/go-sms`.
- `Sender.Send(context.Context, Request) (Submission, error)` remains unchanged.
- Send Failure means an unsuccessful or indeterminate Send Attempt after the request crosses the Provider seam.
- Validation, already-done Context, Provider-specific preflight rejection, request encoding, and request construction errors are ordinary errors and do not satisfy `failure.From`.
- The only Failure categories are `authentication`, `rate_limited`, `rejected`, `unavailable`, and `unknown_outcome`.
- A definitive Provider decision wins over concurrent Context cancellation.
- After invocation, any result without a definitive Provider decision is `UnknownOutcome`; there is no Failure-level `internal` category.
- Failure has no public Message, Cause, Unwrap, native error identity, Provider-native type, raw response, or arbitrary fact bag.
- `Failure.Error()` contains only canonical Provider and category. Validated Code and RequestID are available only through `Details()`.
- Code and RequestID accept bounded ASCII tokens only; invalid optional diagnostics are omitted.
- Category sentinels are removed. Callers classify through `failure.From(err)`, `Category()`, and `UnknownOutcome()`.
- `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` remain available only when an `UnknownOutcome` recorded those Context facts.
- `failure.Failure` is sealed. Built-in and third-party Provider adapters construct it through a public Provider-bound `failure.Factory`.
- Invalid decision categories safely degrade to `UnknownOutcome`; invalid diagnostics are omitted; factory methods never panic and never return a second error.
- Provider-native code/status mapping stays local to each Provider adapter.
- Existing one-attempt, no-redirect, no-retry, immutable Provider, proxy, logging, and sensitive-data constraints remain unchanged.

## File Map

- `failure/failure.go`: public immutable Failure interface, categories, details, extraction, and Provider-bound factory.
- `failure/failure_test.go`: the complete normalization, safety, wrapping, Context, and validation test surface.
- `provider/<name>/provider.go`: bind a factory, report explicit decisions, and report indeterminate post-invocation outcomes.
- `provider/<name>/errors.go`: retain only Provider-native extraction and stable category mapping.
- `internal/providerutil/providerutil.go`: preflight and transport/phone policy only; no public Failure construction or native causes.
- `validate.go`: ordinary request-validation errors.
- `error.go`, `error_test.go`: delete after all adapters migrate.
- `README.md`, `example_test.go`, `docs/superpowers/specs/2026-07-21-go-sms-design.md`, `CONTEXT.md`, `docs/adr/0004-normalize-send-failures.md`: public guidance and accepted design.

## Provider Decision Matrix

This matrix is normative. Provider adapters extract native facts locally, then call exactly one factory operation after invocation:

| Evidence after invocation | Factory operation | Category |
| --- | --- | --- |
| HTTP 401 or 403 | `Decision` | `Authentication` |
| HTTP 429 | `Decision` | `RateLimited` |
| Other HTTP 4xx | `Decision` | `Rejected` |
| HTTP 5xx | `Decision` | `Unavailable` |
| Documented native authentication code | `Decision` | `Authentication` |
| Documented native throttling code | `Decision` | `RateLimited` |
| Documented native unavailable code | `Decision` | `Unavailable` |
| Other explicit non-success body code | `Decision` | `Rejected` |
| Transport/network error, unknown SDK error, nil response, malformed body, missing acceptance identifier | `Unknown` | `UnknownOutcome` |

When one result contains both a definitive HTTP/native decision and a completed Context, use `Decision` and discard the Context fact. Call `Unknown(..., errors.Join(err, ctx.Err()))` only when no row above proves a definitive decision. Optional native Code and RequestID always pass through `Diagnostic`; the `failure` module omits any value that fails token validation.

---

### Task 1: Add the Deep Failure Module

**Files:**
- Create: `failure/failure.go`
- Create: `failure/failure_test.go`
- Modify: `CONTEXT.md`
- Create: `docs/adr/0004-normalize-send-failures.md`
- Add: `docs/superpowers/plans/2026-07-22-failure-normalization.md`

**Interfaces:**
- Consumes: standard `context` and `errors` only.
- Produces: `failure.Category`, five category constants, `failure.Details`, `failure.Diagnostic`, sealed `failure.Failure`, `failure.From`, `failure.Factory`, `failure.NewFactory`, `Factory.Decision`, and `Factory.Unknown`.

- [ ] **Step 1: Write failing public-interface and invariant tests**

Create `failure/failure_test.go` in external package `failure_test`. Cover the exact interface and invariants:

```go
package failure_test

import (
    "context"
    "errors"
    "fmt"
    "testing"

    "github.com/feymanlee/go-sms/failure"
)

func TestDecisionProducesSafeImmutableFailure(t *testing.T) {
    factory, err := failure.NewFactory("aliyun")
    if err != nil { t.Fatal(err) }

    got := factory.Decision(failure.RateLimited, failure.Diagnostic{
        Code: "isv.BUSINESS_LIMIT_CONTROL", RequestID: "request-1",
    })
    if got.Category() != failure.RateLimited || got.UnknownOutcome() {
        t.Fatalf("category=%q unknown=%v", got.Category(), got.UnknownOutcome())
    }
    if details := got.Details(); details != (failure.Details{
        Provider: "aliyun", Code: "isv.BUSINESS_LIMIT_CONTROL", RequestID: "request-1",
    }) { t.Fatalf("details=%#v", details) }
    if got.Error() != "sms: aliyun rate limited" { t.Fatalf("Error=%q", got.Error()) }
    if errors.Unwrap(got) != nil { t.Fatalf("Unwrap=%v", errors.Unwrap(got)) }
}

func TestFromFindsWrappedFailure(t *testing.T) {
    factory, _ := failure.NewFactory("qiniu")
    original := factory.Decision(failure.Rejected, failure.Diagnostic{Code: "400"})
    got, ok := failure.From(fmt.Errorf("send welcome: %w", original))
    if !ok || got != original { t.Fatalf("got=%v ok=%v", got, ok) }
    if got, ok := failure.From(errors.New("ordinary")); ok || got != nil {
        t.Fatalf("ordinary got=%v ok=%v", got, ok)
    }
}

func TestUnknownRecordsOnlyContextSentinels(t *testing.T) {
    factory, _ := failure.NewFactory("tencent")
    native := errors.New("secret native error")
    got := factory.Unknown(failure.Diagnostic{RequestID: "request-2"}, errors.Join(native, context.DeadlineExceeded))
    if got.Category() != failure.UnknownOutcome || !got.UnknownOutcome() {
        t.Fatalf("category=%q unknown=%v", got.Category(), got.UnknownOutcome())
    }
    if !errors.Is(got, context.DeadlineExceeded) || errors.Is(got, native) {
        t.Fatalf("context/native matching: %v", got)
    }
    if got.Error() != "sms: tencent unknown outcome" { t.Fatalf("Error=%q", got.Error()) }
}
```

Add tables that assert:

```go
func TestFactoryValidationAndSafeDegradation(t *testing.T) {
    invalidProviders := []string{"", "Aliyun", " aliyun", "aliyun.example", "provider-name-that-is-far-too-long"}
    for _, provider := range invalidProviders {
        if _, err := failure.NewFactory(provider); err == nil { t.Fatalf("NewFactory(%q) succeeded", provider) }
    }

    factory, _ := failure.NewFactory("ucloud")
    got := factory.Decision(failure.Category("unsupported"), failure.Diagnostic{
        Code: "bad code with spaces", RequestID: "line\nbreak",
    })
    if got.Category() != failure.UnknownOutcome { t.Fatalf("category=%q", got.Category()) }
    if got.Details().Code != "" || got.Details().RequestID != "" { t.Fatalf("details=%#v", got.Details()) }
}
```

Add `TestDecisionCategories`, `TestUnknownContextEvidence`, `TestZeroFactory`, and `TestDiagnosticValidation` table tests. Their rows must cover all four decision categories; canceled, deadline, joined canceled+deadline, and non-Context evidence; zero-value `Factory.Decision(Authentication, Diagnostic{})` producing Provider `unknown`; Code lengths 1/128/129; RequestID lengths 1/256/257; empty values; spaces; newline/control bytes; and Unicode. Assert non-Context evidence cannot be recovered with either `errors.Is` or `errors.As`. Do not add exported category sentinels.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./failure -count=1`

Expected: FAIL because package `failure` and all named symbols do not exist.

- [ ] **Step 3: Implement the exact sealed interface and factory**

Create `failure/failure.go` with this implementation shape:

```go
package failure

import (
    "context"
    "errors"
    "fmt"
    "regexp"
    "strings"
)

type Category string

const (
    Authentication Category = "authentication"
    RateLimited    Category = "rate_limited"
    Rejected       Category = "rejected"
    Unavailable    Category = "unavailable"
    UnknownOutcome Category = "unknown_outcome"
)

type Details struct { Provider, Code, RequestID string }
type Diagnostic struct { Code, RequestID string }

type Failure interface {
    error
    Category() Category
    Details() Details
    UnknownOutcome() bool
    isFailure()
}

type normalized struct {
    category Category
    details Details
    canceled bool
    deadline bool
}

func (f *normalized) Error() string {
    return fmt.Sprintf("sms: %s %s", f.details.Provider, strings.ReplaceAll(string(f.category), "_", " "))
}
func (f *normalized) Category() Category { return f.category }
func (f *normalized) Details() Details { return f.details }
func (f *normalized) UnknownOutcome() bool { return f.category == UnknownOutcome }
func (*normalized) isFailure() {}
func (f *normalized) Is(target error) bool {
    return target == context.Canceled && f.canceled || target == context.DeadlineExceeded && f.deadline
}

func From(err error) (Failure, bool) {
    if err == nil { return nil, false }
    var got Failure
    if !errors.As(err, &got) { return nil, false }
    return got, true
}

type Factory struct { provider string }

var (
    providerToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
    codeToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
    requestIDToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=-]{0,255}$`)
)

func NewFactory(provider string) (Factory, error) {
    if !providerToken.MatchString(provider) { return Factory{}, fmt.Errorf("failure: invalid Provider identifier") }
    return Factory{provider: provider}, nil
}

func (f Factory) Decision(category Category, diagnostic Diagnostic) Failure {
    if !isDecision(category) { return f.Unknown(diagnostic, nil) }
    return f.new(category, diagnostic, nil)
}

func (f Factory) Unknown(diagnostic Diagnostic, contextEvidence error) Failure {
    return f.new(UnknownOutcome, diagnostic, contextEvidence)
}

func (f Factory) new(category Category, diagnostic Diagnostic, contextEvidence error) Failure {
    provider := f.provider
    if !providerToken.MatchString(provider) { provider = "unknown" }
    details := Details{Provider: provider}
    if codeToken.MatchString(diagnostic.Code) { details.Code = diagnostic.Code }
    if requestIDToken.MatchString(diagnostic.RequestID) { details.RequestID = diagnostic.RequestID }
    return &normalized{
        category: category, details: details,
        canceled: errors.Is(contextEvidence, context.Canceled),
        deadline: errors.Is(contextEvidence, context.DeadlineExceeded),
    }
}

func isDecision(category Category) bool {
    return category == Authentication || category == RateLimited || category == Rejected || category == Unavailable
}
```

- [ ] **Step 4: Run focused tests and static checks**

Run: `gofmt -w failure/*.go && go test -count=1 ./failure && go vet ./failure`

Expected: PASS with pristine output.

- [ ] **Step 5: Commit the accepted interface and design records**

```bash
git add failure CONTEXT.md docs/adr/0004-normalize-send-failures.md docs/superpowers/plans/2026-07-22-failure-normalization.md
git commit -m "feat(failure): add normalized Send Failure module"
```

---

### Task 2: Migrate UCloud Failure Decisions

**Files:**
- Modify: `provider/ucloud/provider.go`
- Modify: `provider/ucloud/errors.go`
- Modify: `provider/ucloud/provider_test.go`

**Interfaces:**
- Consumes: `failure.NewFactory`, `Factory.Decision`, `Factory.Unknown`, `failure.From` from Task 1.
- Produces: UCloud post-invocation paths that return only normalized Failure values.

- [ ] **Step 1: Rewrite UCloud post-invocation tests to the new interface**

Add a test helper in `provider/ucloud/provider_test.go`:

```go
func requireFailure(t *testing.T, err error, category failure.Category) failure.Failure {
    t.Helper()
    got, ok := failure.From(err)
    if !ok { t.Fatalf("error is not a Failure: %v", err) }
    if got.Category() != category { t.Fatalf("category=%q want=%q", got.Category(), category) }
    return got
}
```

Use the helper in the existing HTTP-status, Provider-body, malformed-response, and transport tests. HTTP 401/403, 429, other 4xx, and 5xx must assert Authentication, RateLimited, Rejected, and Unavailable respectively and retain the decimal HTTP status as `Details().Code`. An unclassified non-2xx status such as a 3xx redirect must assert UnknownOutcome while retaining its decimal status Code and any `ctx.Err()` evidence. A nonzero RetCode must assert Rejected and retain its decimal code. Nil response, decode failure, trailing JSON, missing RetCode, and missing SessionNo must assert UnknownOutcome. The transport test must assert `!errors.Is(err, transportErr)`; cancellation and deadline rows must still assert their matching Context sentinel.

- [ ] **Step 2: Run UCloud tests and verify RED**

Run: `go test -count=1 ./provider/ucloud`

Expected: FAIL because UCloud still returns `sms.SendError` and has no bound factory.

- [ ] **Step 3: Bind a factory and migrate the implementation**

Add `failures failure.Factory` to `Provider`, create it in `New`, and replace post-invocation construction exactly as follows:

```go
failures, err := failure.NewFactory("ucloud")
if err != nil { return nil, err }
// store failures in Provider.failures

// transport error after client.Do
return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))

// explicit non-2xx
diagnostic := failure.Diagnostic{Code: strconv.Itoa(response.StatusCode)}
if category, ok := httpErrorCategory(response.StatusCode); ok {
    return sms.Submission{}, p.failures.Decision(category, diagnostic)
}
return sms.Submission{}, p.failures.Unknown(diagnostic, ctx.Err())

// explicit nonzero RetCode
return sms.Submission{}, p.failures.Decision(failure.Rejected, failure.Diagnostic{Code: strconv.Itoa(*body.RetCode)})

// nil/decode/trailing/missing-field response after invocation
return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
```

Rename `httpErrorKind` to `httpErrorCategory` and return `(failure.Category, bool)` using the normative matrix: only 401/403, 429, other 4xx, and 5xx return a decision. Remove `providerRejection`, `providerErrorMessage`, and `internalError`. Replace the pre-invocation request-construction branch with `errors.New("ucloud: cannot create request")`; leave phone conversion on the legacy preflight path until Task 7.

- [ ] **Step 4: Verify UCloud and full compatibility**

Run: `gofmt -w provider/ucloud/*.go && go test -count=1 -race ./provider/ucloud && go test -count=1 ./...`

Expected: PASS; packages not yet migrated may still use the old model.

- [ ] **Step 5: Commit UCloud migration**

```bash
git add provider/ucloud
git commit -m "refactor(ucloud): normalize Send Failures"
```

---

### Task 3: Migrate Qiniu Failure Decisions

**Files:**
- Modify: `provider/qiniu/provider.go`
- Modify: `provider/qiniu/errors.go`
- Modify: `provider/qiniu/provider_test.go`

**Interfaces:**
- Consumes: the Task 1 failure interface and UCloud migration pattern.
- Produces: Qiniu post-invocation paths that return normalized Failure values with safe RequestID.

- [ ] **Step 1: Write failing Qiniu Failure-interface tests**

Add the exact `requireFailure` helper from Task 2. Apply it to the existing HTTP-status, malformed-response, and transport tests. HTTP 401/403, 429, other 4xx, and 5xx must assert the matrix category, decimal status Code, and `X-Reqid`. An unclassified non-2xx status such as a 3xx redirect must assert UnknownOutcome while retaining its decimal status Code, `X-Reqid`, and any `ctx.Err()` evidence. Nil response, decode failure, trailing JSON, and missing JobID must assert UnknownOutcome while preserving a valid `X-Reqid`. The transport test must assert `!errors.Is(err, transportErr)`; cancellation and deadline rows must still match their Context sentinel.

- [ ] **Step 2: Run Qiniu tests and verify RED**

Run: `go test -count=1 ./provider/qiniu`

Expected: FAIL because Qiniu still constructs the old error model.

- [ ] **Step 3: Bind the Qiniu factory and replace post-invocation errors**

Use these exact constructions:

```go
failures, err := failure.NewFactory("qiniu")
if err != nil { return nil, err }

return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))

diagnostic := failure.Diagnostic{
    Code: strconv.Itoa(response.StatusCode), RequestID: requestID,
}
if category, ok := httpErrorCategory(response.StatusCode); ok {
    return sms.Submission{}, p.failures.Decision(category, diagnostic)
}
return sms.Submission{}, p.failures.Unknown(diagnostic, ctx.Err())

return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{RequestID: requestID}, ctx.Err())
```

Add `failures failure.Factory` to `Provider` and store the validated `failure.NewFactory("qiniu")` result in `New`. Keep HMAC signing, request bytes, response RequestID extraction, and Provider acceptance mapping unchanged. Rename `httpErrorKind` to `httpErrorCategory`, return `(failure.Category, bool)`, and report a decision only for 401/403, 429, other 4xx, and 5xx. Delete `providerError`, `internalError`, and `non2xxMessage`. Replace the two pre-invocation branches with `errors.New("qiniu: cannot encode request")` and `errors.New("qiniu: cannot create request")`; leave phone conversion on the legacy preflight path until Task 7.

- [ ] **Step 4: Verify Qiniu and full compatibility**

Run: `gofmt -w provider/qiniu/*.go && go test -count=1 -race ./provider/qiniu && go test -count=1 ./...`

Expected: PASS.

- [ ] **Step 5: Commit Qiniu migration**

```bash
git add provider/qiniu
git commit -m "refactor(qiniu): normalize Send Failures"
```

---

### Task 4: Migrate Yunpian Failure Decisions

**Files:**
- Modify: `provider/yunpian/provider.go`
- Modify: `provider/yunpian/errors.go`
- Modify: `provider/yunpian/provider_test.go`

**Interfaces:**
- Consumes: Task 1 failure interface.
- Produces: Yunpian explicit HTTP/body decisions and indeterminate response paths as normalized Failure values.

- [ ] **Step 1: Write failing Yunpian Failure-interface tests**

Add the exact `requireFailure` helper from Task 2. HTTP 401/403, 429, other 4xx, and 5xx must assert the matrix category and decimal status Code. An unclassified non-2xx status such as a 3xx redirect must assert UnknownOutcome while retaining its decimal status Code and any `ctx.Err()` evidence. Every nonzero body Code must assert Rejected; valid body Codes are preserved, while invalid negative Codes are omitted by shared diagnostic validation. Nil response, decode failure, trailing JSON, missing Code, and missing SID must assert UnknownOutcome. Empty parameter values, pre-canceled Context, and request-construction failures must assert `failure.From(err)` is false. The transport test must assert `!errors.Is(err, transportErr)` while cancellation and deadline rows still match their Context sentinel.

- [ ] **Step 2: Run Yunpian tests and verify RED**

Run: `go test -count=1 ./provider/yunpian`

Expected: FAIL against the old error model.

- [ ] **Step 3: Bind the factory and migrate the implementation**

Add `failures failure.Factory` to `Provider` and store the validated `failure.NewFactory("yunpian")` result in `New`. Make `httpErrorCategory` return `(failure.Category, bool)` and call `Decision(category, diagnostic)` only for 401/403, 429, other 4xx, and 5xx; call `Unknown(diagnostic, ctx.Err())` for every other non-2xx status. Use `Decision(failure.Rejected, Diagnostic{Code: body.Code.String()})` for explicit body rejection, and `Unknown(Diagnostic{}, ctx.Err())` for every nil/decode/trailing/missing-field response. Use `Unknown(Diagnostic{}, errors.Join(err, ctx.Err()))` for transport errors. Delete both message constants and `internalError`. Return `errors.New("yunpian: template parameter value is required")` and `errors.New("yunpian: cannot create request")` on their pre-invocation branches.

- [ ] **Step 4: Verify Yunpian and full compatibility**

Run: `gofmt -w provider/yunpian/*.go && go test -count=1 -race ./provider/yunpian && go test -count=1 ./...`

Expected: PASS.

- [ ] **Step 5: Commit Yunpian migration**

```bash
git add provider/yunpian
git commit -m "refactor(yunpian): normalize Send Failures"
```

---

### Task 5: Migrate Tencent SDK Failure Decisions

**Files:**
- Modify: `provider/tencent/provider.go`
- Modify: `provider/tencent/errors.go`
- Modify: `provider/tencent/provider_test.go`

**Interfaces:**
- Consumes: Task 1 failure interface and existing Tencent SDK fact extraction.
- Produces: Tencent explicit native/body decisions and indeterminate SDK outcomes through `failure.Factory`.

- [ ] **Step 1: Write failing Tencent normalization tests**

Replace category-sentinel and `*sms.SendError` assertions with `failure.From`. Require:

```go
got := requireFailure(t, err, failure.RateLimited)
if details := got.Details(); details.Code != "RequestLimitExceeded" || details.RequestID != "request-3" {
    t.Fatalf("details=%#v", details)
}
```

Add the exact `requireFailure` helper from Task 2. Body codes use `classifyStatusCode`: documented authentication, throttling, and unavailable prefixes keep those categories; every other explicit non-`Ok` body code is Rejected. SDK errors use `classifyCode`: documented prefixes are decisions, while `ClientError.NetworkError` and every unknown SDK code are UnknownOutcome. Nil response, nil response body, zero/multiple/nil status rows, missing status Code, and an `Ok` status with an empty `SerialNo` are UnknownOutcome. Isolate the acceptance requirement from Context evidence with three rows: missing `SerialNo` under `context.Background()` is UnknownOutcome; missing `SerialNo` when Context is canceled during invocation is UnknownOutcome with a Context match; and a valid `SerialNo` when Context is canceled during invocation returns a successful Submission with no error. Valid Code/RequestID survive. Native identity and `*TencentCloudSDKError` must not be recoverable. Add a canceled-Context row carrying a known authentication SDK code and assert Authentication with no Context match.

- [ ] **Step 2: Run Tencent tests and verify RED**

Run: `go test -count=1 ./provider/tencent`

Expected: FAIL against `sms.SendError` and old Internal behavior.

- [ ] **Step 3: Bind a factory and reduce errors.go to extraction/mapping**

Add `failures failure.Factory` to `Provider`, store `failure.NewFactory("tencent")` in `New`, and call `classifyError(ctx, p.failures, err)`. Implement the classifier in this order so a native decision wins over Context:

```go
func classifyError(ctx context.Context, failures failure.Factory, err error) error {
    var native *tcerr.TencentCloudSDKError
    if errors.As(err, &native) {
        diagnostic := failure.Diagnostic{Code: native.Code, RequestID: native.RequestId}
        if native.Code != "ClientError.NetworkError" {
            if category, ok := classifyCode(native.Code); ok {
                return failures.Decision(category, diagnostic)
            }
        }
        return failures.Unknown(diagnostic, errors.Join(err, ctx.Err()))
    }
    return failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))
}
```

Make `classifyCode(string) (failure.Category, bool)` preserve the existing documented prefix groups and return `("", false)` otherwise. Make `classifyStatusCode(string) failure.Category` return a known category or Rejected. In `Send`, use `p.failures.Decision(classifyStatusCode(code), Diagnostic{Code: code, RequestID: requestID})` for non-`Ok` body codes. After that explicit-decision branch, require a nonempty `SerialNo`; otherwise use `p.failures.Unknown(Diagnostic{Code: code, RequestID: requestID}, ctx.Err())`. Use `p.failures.Unknown(Diagnostic{RequestID: requestID}, ctx.Err())` for other malformed responses. Delete Provider message constants, network helper functions, opaque causes, and `internalError`.

- [ ] **Step 4: Verify Tencent and full compatibility**

Run: `gofmt -w provider/tencent/*.go && go test -count=1 -race ./provider/tencent && go test -count=1 ./...`

Expected: PASS with retry and redirect policy tests unchanged.

- [ ] **Step 5: Commit Tencent migration**

```bash
git add provider/tencent
git commit -m "refactor(tencent): normalize Send Failures"
```

---

### Task 6: Migrate Alibaba SDK Failure Decisions

**Files:**
- Modify: `provider/aliyun/provider.go`
- Modify: `provider/aliyun/errors.go`
- Modify: `provider/aliyun/provider_test.go`

**Interfaces:**
- Consumes: Task 1 failure interface and existing Alibaba SDK fact extraction.
- Produces: Alibaba explicit body/SDK decisions and indeterminate outcomes through `failure.Factory`.

- [ ] **Step 1: Write failing Alibaba normalization tests**

Add the exact `requireFailure` helper from Task 2. Body responses use `classifyBodyCode`: known authentication/throttling/unavailable codes keep those categories and every other explicit non-`OK` code is Rejected. SDK rows assert, in order, HTTP 429 RateLimited; HTTP 401/403 Authentication; HTTP 5xx Unavailable; documented native code category; other HTTP 4xx Rejected; and no decision for all other status/code pairs. Preserve the existing HTTP-429-plus-auth-code regression row and expect RateLimited. Network/Context/non-SDK errors, undecidable SDK errors, nil response/body, missing body Code, and an `OK` body with an empty `BizId` become UnknownOutcome. Isolate the acceptance requirement from Context evidence with three rows: missing `BizId` under `context.Background()` is UnknownOutcome; missing `BizId` when Context is canceled during invocation is UnknownOutcome with a Context match; and a valid `BizId` when Context is canceled during invocation returns a successful Submission with no error. Valid RequestID survives. Native SDK identity and type are unrecoverable. Add a canceled-Context row with HTTP 429 and assert RateLimited with no Context match.

- [ ] **Step 2: Run Alibaba tests and verify RED**

Run: `go test -count=1 ./provider/aliyun`

Expected: FAIL against the old error model.

- [ ] **Step 3: Bind a factory and replace classification output**

Add `failures failure.Factory` to `Provider`, store `failure.NewFactory("aliyun")` in `New`, and call `classifyError(ctx, p.failures, err)`. Keep `sdkErrorDetails`, `requestIDFromData`, and native mapping local. Implement `classifySDKDecision` with exact precedence:

```go
func classifySDKDecision(status int, code string) (failure.Category, bool) {
    switch {
    case status == http.StatusTooManyRequests:
        return failure.RateLimited, true
    case status == http.StatusUnauthorized || status == http.StatusForbidden:
        return failure.Authentication, true
    case status >= 500 && status <= 599:
        return failure.Unavailable, true
    }
    if category, ok := classifyKnownCode(code); ok {
        return category, true
    }
    if status >= 400 && status <= 499 {
        return failure.Rejected, true
    }
    return "", false
}
```

`classifyKnownCode` returns a category and true only for the current authentication, throttling, and unavailable code sets. `classifyBodyCode` uses it and defaults to Rejected. `classifyError` extracts diagnostics, calls `Decision` only when `classifySDKDecision` succeeds, and otherwise calls `Unknown(diagnostic, errors.Join(err, ctx.Err()))`; non-SDK errors always call `Unknown` with empty diagnostics. A non-`OK` body response uses `Decision`. After that explicit-decision branch, require a nonempty `BizId`; otherwise use `Unknown(Diagnostic{Code: code, RequestID: requestID}, ctx.Err())`. Other nil/malformed responses use `Unknown` with any safe RequestID. Replace the pre-invocation JSON branch with `errors.New("aliyun: cannot encode template parameters")`; delete message constants, network helpers, opaque causes, and `internalError`.

- [ ] **Step 4: Verify Alibaba and full compatibility**

Run: `gofmt -w provider/aliyun/*.go && go test -count=1 -race ./provider/aliyun && go test -count=1 ./...`

Expected: PASS.

- [ ] **Step 5: Commit Alibaba migration**

```bash
git add provider/aliyun
git commit -m "refactor(aliyun): normalize Send Failures"
```

---

### Task 7: Remove the Old Error Model and Make Preflight Ordinary

**Files:**
- Delete: `error.go`
- Delete: `error_test.go`
- Modify: `validate.go`
- Modify: `validate_test.go`
- Modify: `internal/providerutil/providerutil.go`
- Modify: `internal/providerutil/providerutil_test.go`
- Modify: all `provider/*/provider.go` preflight call sites
- Modify: all `provider/*/provider_test.go` preflight assertions
- Modify: `example_test.go`

**Interfaces:**
- Consumes: all Provider post-invocation migrations from Tasks 2-6.
- Produces: no remaining root `SendError`, category sentinel, Failure constructor, opaque cause, or preflight Failure.

- [ ] **Step 1: Write failing ordinary-preflight tests**

Update root and Provider tests to assert:

```go
err := req.Validate()
if err == nil { t.Fatal("Validate succeeded") }
if _, ok := failure.From(err); ok { t.Fatalf("validation returned Failure: %v", err) }
```

Add the same `failure.From(err)` false assertion to nil/already-done Context, missing Signature Reference, unsupported country, empty Yunpian parameter value, marshal/request-construction faults, and constructor validation tests. An already-done Context must still match its Context sentinel directly. Update `example_test.go` to the `failure.From` caller pattern shown in Task 8 before deleting the old root symbols.

- [ ] **Step 2: Run full tests and verify RED**

Run: `go test -count=1 ./...`

Expected: FAIL because preflight paths still return `sms.SendError` and old symbols remain.

- [ ] **Step 3: Make root validation and providerutil preflight ordinary**

Replace `invalidRequest` with static ordinary errors:

```go
func invalidRequest(message string) error { return errors.New("sms: " + message) }
```

Change the preflight interface to:

```go
func Prepare(ctx context.Context, req sms.Request, defaultSignature string, signatureRequired bool) (string, error)
```

Implement its branches exactly as follows and update all five Provider call sites to remove the Provider-name argument:

```go
if ctx == nil {
    return "", errors.New("sms: context is required")
}
select {
case <-ctx.Done():
    return "", ctx.Err()
default:
}
if err := req.Validate(); err != nil {
    return "", err
}
signature := req.SignatureRef
if signature == "" {
    signature = defaultSignature
}
if signatureRequired && signature == "" {
    return "", errors.New("sms: SignatureRef is required")
}
return signature, nil
```

For UCloud number conversion, Qiniu/Alibaba +86 restrictions, and Yunpian empty parameter values, return new static `errors.New` values prefixed with the Provider name; do not wrap parser errors or include the Recipient. Request encoding/construction branches were made ordinary in Tasks 2-6.

Delete `UnknownOutcome`, `UnknownOutcomeWithDetails`, `OpaqueCause`, `opaqueCause`, `Sanitize`, and their tests from `providerutil`. Delete `error.go` and `error_test.go`.

- [ ] **Step 4: Prove the old interface is gone**

Run:

```bash
rg -n 'SendError|ErrorKind|KindInvalidRequest|KindAuthentication|KindRateLimited|KindRejected|KindUnavailable|KindUnknownOutcome|KindInternal|ErrInvalidRequest|ErrAuthentication|ErrRateLimited|ErrRejected|ErrUnavailable|ErrUnknownOutcome|ErrInternal|OpaqueCause|UnknownOutcomeWithDetails' --glob '*.go' --glob '!docs/superpowers/plans/2026-07-21-go-sms-implementation.md'
```

Expected: no output.

- [ ] **Step 5: Verify root, providerutil, and all Providers**

Run: `gofmt -w validate.go validate_test.go example_test.go internal/providerutil/*.go provider/*/*.go && go test -count=1 -race ./... && go vet ./...`

Expected: PASS with pristine output.

- [ ] **Step 6: Commit removal**

```bash
git add -A -- error.go error_test.go validate.go validate_test.go example_test.go internal/providerutil provider
git commit -m "refactor: remove legacy SendError model"
```

---

### Task 8: Migrate Guidance and Verify the Breaking Release

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-07-21-go-sms-design.md`
- Verify: `CONTEXT.md`
- Verify: `docs/adr/0004-normalize-send-failures.md`

**Interfaces:**
- Consumes: final `failure` package and all migrated Provider adapters.
- Produces: compile-checked caller guidance and an updated design specification.

- [ ] **Step 1: Update compile-checked examples before implementation docs**

Mirror the already compile-checked `example_test.go` caller pattern in README:

```go
submission, err := provider.Send(ctx, request)
if err != nil {
    if got, ok := failure.From(err); ok {
        details := got.Details()
        log.Printf("SMS Send Attempt failed: category=%s provider=%s code=%s request_id=%s",
            got.Category(), details.Provider, details.Code, details.RequestID)
        if got.UnknownOutcome() { log.Print("reconcile before retry") }
    } else {
        log.Print(err)
    }
    return
}
_ = submission
```

Do not log Recipient, Template Parameter values, Provider messages, native errors, or request bodies.

- [ ] **Step 2: Run examples and verify they compile**

Run: `go test -count=1 .`

Expected: PASS without executing a live send because the example has no `Output:` comment.

- [ ] **Step 3: Replace the design specification error section**

Document the exact five categories, ordinary preflight errors, the `failure` package interface, Provider-bound semantic factory, definitive-decision precedence, `UnknownOutcome` rule, strict diagnostics, Context sentinel matching, sealing, and removal of native cause identity. Link ADR 0004 from the related-decisions section.

- [ ] **Step 4: Run the complete fresh verification suite**

Run:

```bash
go mod tidy
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 ./...
go test -count=1 -race ./...
env -u GO_SMS_TEST_RECIPIENT -u GO_SMS_TEST_PARAM_NAME -u GO_SMS_TEST_PARAM_VALUE -u TENCENT_SECRET_ID -u TENCENT_SECRET_KEY -u TENCENT_SMS_APP_ID -u TENCENT_REGION -u TENCENT_TEMPLATE_ID -u TENCENT_SIGNATURE_REF -u ALIYUN_ACCESS_KEY_ID -u ALIYUN_ACCESS_KEY_SECRET -u ALIYUN_REGION -u ALIYUN_TEMPLATE_ID -u ALIYUN_SIGNATURE_REF -u UCLOUD_PUBLIC_KEY -u UCLOUD_PRIVATE_KEY -u UCLOUD_PROJECT_ID -u UCLOUD_REGION -u UCLOUD_TEMPLATE_ID -u UCLOUD_SIGNATURE_REF -u QINIU_ACCESS_KEY -u QINIU_SECRET_KEY -u QINIU_TEMPLATE_ID -u QINIU_SIGNATURE_REF -u YUNPIAN_API_KEY -u YUNPIAN_TEMPLATE_ID go test -count=1 -tags=integration ./provider/... -run TestIntegrationSend -v
git diff --check
```

Expected: all commands exit 0; all five integration tests skip before Provider construction when variables are unset; no live send occurs.

- [ ] **Step 5: Commit documentation and migration guidance**

```bash
git add README.md docs/superpowers/specs/2026-07-21-go-sms-design.md CONTEXT.md docs/adr/0004-normalize-send-failures.md
git commit -m "docs: describe normalized Send Failures"
```

---

## Self-Review Checklist

- [x] Every accepted grilling decision appears in Global Constraints and at least one implementation task.
- [x] The exact `failure` package names and signatures are consistent across all tasks.
- [x] No task retains native messages, causes, raw identity, category sentinels, `SendError`, or Failure-level Invalid/Internal categories.
- [x] Every post-invocation ambiguous path migrates to `UnknownOutcome`.
- [x] Every explicit Provider decision retains only validated Code/RequestID and the Provider-local stable category.
- [x] Every pre-invocation path is tested as an ordinary non-Failure error.
- [x] The Provider retry, redirect, Context, proxy, and concurrency policies remain under existing tests.
- [x] README, examples, design, glossary, and ADR agree with the final interface.
