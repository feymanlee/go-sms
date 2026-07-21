# Go SMS Multi-Provider Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go library that sends one template SMS through an explicitly selected Tencent Cloud, Alibaba Cloud, UCloud, Qiniu Cloud, or Yunpian Provider.

**Architecture:** The root `sms` package owns immutable request/value types, the `Sender` interface, validation, submissions, and normalized errors. Five `provider/*` packages implement that interface; Tencent and Alibaba use context-aware official SDK clients, while UCloud, Qiniu, and Yunpian use one-shot HTTP clients with locally tested request signing and encoding.

**Tech Stack:** Go 1.25, standard library `context`/`net/http`/`httptest`, Tencent Cloud Go SDK `sms@v1.3.93` and `common@v1.3.139`, Alibaba Cloud Dysmsapi SDK `v5.6.0`, `github.com/nyaruka/phonenumbers@v1.8.1`, GitHub Actions.

## Global Constraints

- Module path is exactly `github.com/feymanlee/go-sms`; minimum Go version is exactly 1.25.
- A `Send` call targets exactly one E.164 Recipient through exactly one Provider and performs no automatic retry or failover.
- Version 1 supports only Provider-native template IDs, ordered named template parameters, and one Recipient.
- Credentials are constructor inputs; production library code never reads environment variables or configuration files.
- Provider instances are immutable after construction and safe for concurrent `Send` calls.
- `context.Context` cancellation and deadlines reach the outbound call; an already-done Context creates no request.
- SDK types and raw responses never appear in the root public API. Credentials, complete request bodies, and complete phone numbers never appear in default error text, Metadata, or logs.
- Ordinary CI runs without credentials on Go 1.25 and 1.26. Live sends are opt-in integration tests guarded by the `integration` build tag.
- Do not add a Manager, routing, failover, retries, batch sends, raw text messages, template/signature registries, delivery receipts, callbacks, queues, logging, metrics, environment-based production config, Provider-specific per-send fields, or `Raw any` escape hatches.

## File Map

- `go.mod`, `go.sum`: module identity and pinned dependencies.
- `sender.go`: `Sender`, `Request`, `TemplateMessage`, `TemplateParam`, and `Submission`.
- `recipient.go`: strict E.164 `Recipient` value object.
- `validate.go`: common request validation.
- `error.go`: stable error categories, sentinels, `SendError`, `errors.Is/As` behavior.
- `internal/providerutil/providerutil.go`: preflight, SignatureRef resolution, redaction, timeout client, and phone formatting shared by Provider implementations.
- `provider/<name>/config.go`: typed immutable construction config and options.
- `provider/<name>/provider.go`: request mapping and `Send` implementation.
- `provider/<name>/errors.go`: native error/response classification.
- `provider/<name>/*_test.go`: fake SDK or `httptest.Server` contract tests.
- `provider/<name>/integration_test.go`: credential-gated live send.
- `.github/workflows/ci.yml`: Go 1.25/1.26 test matrix and race test.
- `README.md`: installation, core example, Provider construction, error handling, and explicit non-goals.
- `docs/integration-testing.md`: secret names and five-Provider release verification procedure.

The five adapters depend on the same core types and release as one module, so they remain one ordered plan. Each adapter is still a separate task and commit so a reviewer can reject one integration without blocking review of another.

---

### Task 1: Core Value Types and Sender Contract

**Files:**
- Create: `go.mod`
- Create: `sender.go`
- Create: `recipient.go`
- Test: `recipient_test.go`
- Test: `sender_test.go`

**Interfaces:**
- Produces: `ParseRecipient(string) (Recipient, error)`, `Recipient.String() string`, `Sender.Send(context.Context, Request) (Submission, error)`, `Request`, `TemplateMessage`, `TemplateParam`, and `Submission`.
- Consumes: no project code.

- [ ] **Step 1: Write failing contract and E.164 tests**

```go
package sms

import (
    "context"
    "testing"
)

func TestParseRecipient(t *testing.T) {
    t.Parallel()
    valid := []string{"+8613812345678", "+12025550123", "+93701234567"}
    for _, input := range valid {
        recipient, err := ParseRecipient(input)
        if err != nil || recipient.String() != input {
            t.Fatalf("ParseRecipient(%q) = %q, %v", input, recipient.String(), err)
        }
    }

    invalid := []string{"", "13812345678", "+", "+0123", "+86 13812345678", "+1234567890123456"}
    for _, input := range invalid {
        if _, err := ParseRecipient(input); err == nil {
            t.Fatalf("ParseRecipient(%q) succeeded", input)
        }
    }
}

type senderFunc func(context.Context, Request) (Submission, error)

func (f senderFunc) Send(ctx context.Context, req Request) (Submission, error) {
    return f(ctx, req)
}

func TestSenderContract(t *testing.T) {
    var _ Sender = senderFunc(func(context.Context, Request) (Submission, error) {
        return Submission{Provider: "fake", MessageID: "message-1"}, nil
    })
}
```

- [ ] **Step 2: Run the tests to verify the package does not exist yet**

Run: `go test ./...`

Expected: FAIL because `go.mod`, `ParseRecipient`, and the public types do not exist.

- [ ] **Step 3: Add the module declaration**

```go
// go.mod
module github.com/feymanlee/go-sms

go 1.25
```

- [ ] **Step 4: Add the Sender contract and data types**

```go
// sender.go
package sms

import "context"

type Sender interface {
    Send(context.Context, Request) (Submission, error)
}

type Request struct {
    Recipient    Recipient
    Message      TemplateMessage
    SignatureRef string
}

type TemplateMessage struct {
    TemplateID string
    Params     []TemplateParam
}

type TemplateParam struct {
    Name  string
    Value string
}

type Submission struct {
    Provider  string
    MessageID string
    RequestID string
    Metadata  map[string]string
}
```

- [ ] **Step 5: Add strict E.164 Recipient parsing**

```go
// recipient.go
package sms

import (
    "errors"
    "regexp"
)

var e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{0,14}$`)

type Recipient struct {
    e164 string
}

func ParseRecipient(value string) (Recipient, error) {
    if !e164Pattern.MatchString(value) {
        return Recipient{}, errors.New("sms: invalid E.164 recipient")
    }
    return Recipient{e164: value}, nil
}

func (r Recipient) String() string { return r.e164 }

func (r Recipient) valid() bool { return e164Pattern.MatchString(r.e164) }
```

- [ ] **Step 6: Format and run the focused tests**

Run: `gofmt -w sender.go recipient.go recipient_test.go sender_test.go && go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit the core contract**

```bash
git add go.mod sender.go recipient.go recipient_test.go sender_test.go
git commit -m "feat: define core SMS sending contract"
```

---

### Task 2: Request Validation and Stable Error Model

**Files:**
- Create: `error.go`
- Create: `validate.go`
- Test: `error_test.go`
- Test: `validate_test.go`

**Interfaces:**
- Consumes: `Request`, `Recipient`, and `TemplateParam` from Task 1.
- Produces: `ErrorKind`, seven `Kind*` constants, seven `Err*` sentinels, `SendError`, and `Request.Validate() error`.

- [ ] **Step 1: Write failing validation and matching tests**

```go
package sms

import (
    "context"
    "errors"
    "testing"
)

func validRequest(t *testing.T) Request {
    t.Helper()
    recipient, err := ParseRecipient("+8613812345678")
    if err != nil { t.Fatal(err) }
    return Request{Recipient: recipient, Message: TemplateMessage{
        TemplateID: "template-1",
        Params: []TemplateParam{{Name: "code", Value: "123456"}},
    }}
}

func TestRequestValidate(t *testing.T) {
    tests := []struct{name string; mutate func(*Request)}{
        {"zero recipient", func(r *Request) { r.Recipient = Recipient{} }},
        {"blank template", func(r *Request) { r.Message.TemplateID = " " }},
        {"blank parameter name", func(r *Request) { r.Message.Params[0].Name = " " }},
        {"duplicate parameter", func(r *Request) { r.Message.Params = append(r.Message.Params, TemplateParam{Name: "code"}) }},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := validRequest(t)
            tt.mutate(&req)
            err := req.Validate()
            if !errors.Is(err, ErrInvalidRequest) { t.Fatalf("error = %v", err) }
        })
    }
}

func TestRequestValidateAllowsNoParamsAndEmptyValues(t *testing.T) {
    req := validRequest(t)
    req.Message.Params = nil
    if err := req.Validate(); err != nil { t.Fatal(err) }
    req.Message.Params = []TemplateParam{{Name: "optional", Value: ""}}
    if err := req.Validate(); err != nil { t.Fatal(err) }
}

func TestSendErrorMatchesKindAndCause(t *testing.T) {
    err := &SendError{Kind: KindUnknownOutcome, Provider: "fake", Code: "timeout", Cause: context.DeadlineExceeded}
    if !errors.Is(err, ErrUnknownOutcome) || !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("matching failed: %v", err)
    }
    var detail *SendError
    if !errors.As(err, &detail) || detail.Provider != "fake" { t.Fatalf("detail = %#v", detail) }
    if got := err.Error(); got != "sms: fake unknown outcome (code=timeout)" { t.Fatalf("Error() = %q", got) }
}
```

- [ ] **Step 2: Run focused tests and confirm missing symbols**

Run: `go test ./... -run 'Test(RequestValidate|SendError)'`

Expected: FAIL with undefined `SendError`, `ErrInvalidRequest`, or `Request.Validate`.

- [ ] **Step 3: Implement the error categories and matching behavior**

```go
// error.go
package sms

import (
    "errors"
    "fmt"
)

type ErrorKind string

const (
    KindInvalidRequest ErrorKind = "invalid_request"
    KindAuthentication ErrorKind = "authentication"
    KindRateLimited ErrorKind = "rate_limited"
    KindRejected ErrorKind = "rejected"
    KindUnavailable ErrorKind = "unavailable"
    KindUnknownOutcome ErrorKind = "unknown_outcome"
    KindInternal ErrorKind = "internal"
)

var (
    ErrInvalidRequest = errors.New("sms: invalid request")
    ErrAuthentication = errors.New("sms: authentication failed")
    ErrRateLimited = errors.New("sms: rate limited")
    ErrRejected = errors.New("sms: request rejected")
    ErrUnavailable = errors.New("sms: provider unavailable")
    ErrUnknownOutcome = errors.New("sms: outcome unknown")
    ErrInternal = errors.New("sms: internal error")
)

type SendError struct {
    Kind      ErrorKind
    Provider  string
    Code      string
    Message   string
    RequestID string
    Cause     error
}

func (e *SendError) Error() string {
    label := map[ErrorKind]string{
        KindInvalidRequest: "invalid request", KindAuthentication: "authentication failed",
        KindRateLimited: "rate limited", KindRejected: "rejected",
        KindUnavailable: "unavailable", KindUnknownOutcome: "unknown outcome",
        KindInternal: "internal error",
    }[e.Kind]
    prefix := "sms: "
    if e.Provider != "" { prefix += e.Provider + " " }
    if e.Code != "" { return fmt.Sprintf("%s%s (code=%s)", prefix, label, e.Code) }
    return prefix + label
}

func (e *SendError) Unwrap() error { return e.Cause }

func (e *SendError) Is(target error) bool {
    sentinels := map[ErrorKind]error{
        KindInvalidRequest: ErrInvalidRequest, KindAuthentication: ErrAuthentication,
        KindRateLimited: ErrRateLimited, KindRejected: ErrRejected,
        KindUnavailable: ErrUnavailable, KindUnknownOutcome: ErrUnknownOutcome,
        KindInternal: ErrInternal,
    }
    return target == sentinels[e.Kind] || errors.Is(e.Cause, target)
}
```

- [ ] **Step 4: Implement request validation**

```go
// validate.go
package sms

import "strings"

func (r Request) Validate() error {
    if !r.Recipient.valid() { return invalidRequest("recipient must be E.164") }
    if strings.TrimSpace(r.Message.TemplateID) == "" { return invalidRequest("template ID is required") }
    seen := make(map[string]struct{}, len(r.Message.Params))
    for _, param := range r.Message.Params {
        if strings.TrimSpace(param.Name) == "" { return invalidRequest("template parameter name is required") }
        if _, exists := seen[param.Name]; exists { return invalidRequest("template parameter names must be unique") }
        seen[param.Name] = struct{}{}
    }
    return nil
}

func invalidRequest(message string) error {
    return &SendError{Kind: KindInvalidRequest, Message: message}
}
```

- [ ] **Step 5: Run validation, error, and race tests**

Run: `gofmt -w error.go validate.go error_test.go validate_test.go && go test -race ./...`

Expected: PASS with one package and no race reports.

- [ ] **Step 6: Commit the error contract**

```bash
git add error.go validate.go error_test.go validate_test.go
git commit -m "feat: add request validation and send errors"
```

---

### Task 3: Shared Provider Preflight, Phone Conversion, and HTTP Policy

**Files:**
- Create: `internal/providerutil/providerutil.go`
- Test: `internal/providerutil/providerutil_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `sms.Request`, `sms.SendError`, and error kinds from Tasks 1-2.
- Produces: `Prepare`, `UnknownOutcome`, `Sanitize`, `SplitE164`, `ChinaNational`, `UCloudNumber`, and `NewHTTPClient` for all Provider packages.

- [ ] **Step 1: Add failing shared utility tests**

```go
package providerutil

import (
    "context"
    "errors"
    "testing"
    "time"

    sms "github.com/feymanlee/go-sms"
)

func request(t *testing.T, number string) sms.Request {
    t.Helper()
    recipient, err := sms.ParseRecipient(number)
    if err != nil { t.Fatal(err) }
    return sms.Request{Recipient: recipient, Message: sms.TemplateMessage{TemplateID: "tpl"}}
}

func TestPrepare(t *testing.T) {
    req := request(t, "+8613812345678")
    signature, err := Prepare(context.Background(), "fake", req, "default-signature", true)
    if err != nil || signature != "default-signature" { t.Fatalf("signature=%q err=%v", signature, err) }
    req.SignatureRef = "request-signature"
    signature, err = Prepare(context.Background(), "fake", req, "default-signature", true)
    if err != nil || signature != "request-signature" { t.Fatalf("signature=%q err=%v", signature, err) }
    if _, err := Prepare(context.Background(), "fake", request(t, "+8613812345678"), "", true); !errors.Is(err, sms.ErrInvalidRequest) {
        t.Fatalf("missing signature error=%v", err)
    }
}

func TestPrepareDoesNotStartWithDoneContext(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    _, err := Prepare(ctx, "fake", request(t, "+8613812345678"), "", false)
    if !errors.Is(err, context.Canceled) || errors.Is(err, sms.ErrUnknownOutcome) { t.Fatalf("error=%v", err) }
}

func TestPhoneFormats(t *testing.T) {
    if got, _ := ChinaNational("+8613812345678"); got != "13812345678" { t.Fatalf("ChinaNational=%q", got) }
    if got, _ := UCloudNumber("+60123456789"); got != "(60)123456789" { t.Fatalf("UCloudNumber=%q", got) }
}

func TestNewHTTPClientTimeout(t *testing.T) {
    if NewHTTPClient().Timeout != 10*time.Second { t.Fatalf("timeout=%s", NewHTTPClient().Timeout) }
}
```

- [ ] **Step 2: Install the phone parser and verify tests fail**

Run: `go get github.com/nyaruka/phonenumbers@v1.8.1 && go test ./internal/providerutil`

Expected: FAIL because `Prepare`, phone conversion, and `NewHTTPClient` are undefined.

- [ ] **Step 3: Implement preflight, SignatureRef resolution, and redaction**

```go
package providerutil

import (
    "context"
    "errors"
    "net"
    "net/http"
    "strconv"
    "strings"
    "time"

    sms "github.com/feymanlee/go-sms"
    "github.com/nyaruka/phonenumbers"
)

func Prepare(ctx context.Context, provider string, req sms.Request, defaultSignature string, signatureRequired bool) (string, error) {
    if ctx == nil {
        return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: "context is required"}
    }
    select {
    case <-ctx.Done(): return "", ctx.Err()
    default:
    }
    if err := req.Validate(); err != nil {
        return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: err.Error(), Cause: err}
    }
    signature := req.SignatureRef
    if signature == "" { signature = defaultSignature }
    if signatureRequired && signature == "" {
        return "", &sms.SendError{Kind: sms.KindInvalidRequest, Provider: provider, Message: "SignatureRef is required"}
    }
    return signature, nil
}

func UnknownOutcome(provider string, recipient sms.Recipient, cause error) error {
    return &sms.SendError{Kind: sms.KindUnknownOutcome, Provider: provider, Message: Sanitize(cause.Error(), recipient), Cause: cause}
}

func Sanitize(message string, recipient sms.Recipient) string {
    value := recipient.String()
    message = strings.ReplaceAll(message, value, "[recipient]")
    if _, national, err := SplitE164(value); err == nil {
        message = strings.ReplaceAll(message, national, "[recipient]")
    }
    return message
}
```

- [ ] **Step 4: Implement phone conversion and the default HTTP client**

```go
package providerutil

// Add these functions to providerutil.go, using the imports shown in Step 3.

func SplitE164(value string) (string, string, error) {
    number, err := phonenumbers.Parse(value, "")
    if err != nil { return "", "", err }
    country := strconv.FormatUint(uint64(number.GetCountryCode()), 10)
    national := strings.TrimPrefix(value, "+"+country)
    if national == value || national == "" { return "", "", errors.New("sms: cannot split E.164 recipient") }
    return country, national, nil
}

func ChinaNational(value string) (string, error) {
    country, national, err := SplitE164(value)
    if err != nil { return "", err }
    if country != "86" { return "", errors.New("sms: provider only supports +86 recipients") }
    return national, nil
}

func UCloudNumber(value string) (string, error) {
    country, national, err := SplitE164(value)
    if err != nil { return "", err }
    return "(" + country + ")" + national, nil
}

func NewHTTPClient() *http.Client {
    transport := &http.Transport{
        Proxy: http.ProxyFromEnvironment,
        DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
        ForceAttemptHTTP2: true,
        MaxIdleConns: 100,
        IdleConnTimeout: 90 * time.Second,
        TLSHandshakeTimeout: 5 * time.Second,
        ExpectContinueTimeout: time.Second,
    }
    return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}
```

- [ ] **Step 5: Verify shared helpers**

Run: `gofmt -w internal/providerutil && go test -race ./...`

Expected: PASS; `go.mod` requires `phonenumbers v1.8.1`.

- [ ] **Step 6: Commit the shared Provider utilities**

```bash
git add go.mod go.sum internal/providerutil
git commit -m "feat: add shared provider utilities"
```

---

### Task 4: Tencent Cloud SMS Provider

**Files:**
- Create: `provider/tencent/config.go`
- Create: `provider/tencent/provider.go`
- Create: `provider/tencent/errors.go`
- Test: `provider/tencent/provider_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `sms.Sender`, `sms.Request`, `providerutil.Prepare`, Tencent `v20210111.SendSmsWithContext`.
- Produces: `tencent.Config`, `tencent.Option`, `tencent.WithHTTPClient`, `tencent.WithEndpoint`, `tencent.New(Config, ...Option) (*Provider, error)`, and a compile-time `sms.Sender` implementation.

- [ ] **Step 1: Write fake-client tests for mapping, acceptance, and one call**

Define an internal interface with the exact signature below and a fake that records call count and request. Test that `+8613812345678`, template `1234`, parameters `code=123456` then `minutes=5`, and SignatureRef `Example` map to one-element `PhoneNumberSet`, ordered `TemplateParamSet`, `SmsSdkAppId`, `TemplateId`, and `SignName`. Return `Code: Ok`, `SerialNo: serial-1`, `Fee: 1`, and `RequestId: request-1`; assert the Submission uses those IDs and Metadata `fee=1`. Return a non-`Ok` status in a second test and assert `errors.Is(err, sms.ErrRejected)`.

```go
type apiClient interface {
    SendSmsWithContext(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error)
}

type fakeClient struct {
    calls int
    req *tc.SendSmsRequest
    response *tc.SendSmsResponse
    err error
}

func (f *fakeClient) SendSmsWithContext(_ context.Context, req *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
    f.calls++
    f.req = req
    return f.response, f.err
}
```

- [ ] **Step 2: Add pinned Tencent dependencies and confirm failure**

Run: `go get github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms@v1.3.93 github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common@v1.3.139 && go test ./provider/tencent`

Expected: FAIL because the Provider implementation does not exist.

- [ ] **Step 3: Implement Tencent config and constructor**

Use this exact construction policy:

```go
type Config struct {
    SecretID, SecretKey, SMSAppID, Region, DefaultSignatureRef string
}

type options struct { client *http.Client; endpoint string }
type Option func(*options)
func WithHTTPClient(client *http.Client) Option { return func(o *options) { o.client = client } }
func WithEndpoint(endpoint string) Option { return func(o *options) { o.endpoint = endpoint } }

type Provider struct { client apiClient; appID, defaultSignature string }
var _ sms.Sender = (*Provider)(nil)
```

`New` rejects blank credential, app ID, and Region fields; defaults to `providerutil.NewHTTPClient`; creates `profile.NewClientProfile()` with `NetworkFailureMaxRetries=0`, `RateLimitExceededMaxRetries=0`, and optional endpoint; creates the official client; and supplies the injected client's transport through `WithHttpTransport`. Set `HttpProfile.ReqTimeout` to 10 for the default client; for an injected client with nonzero `Timeout`, use `int(math.Ceil(client.Timeout.Seconds()))`, with a minimum of one second. An injected client with zero `Timeout` relies on Context deadlines and uses the SDK profile value 10 as a final guard.

- [ ] **Step 4: Implement Tencent request mapping and Send**

`Send` must follow this body shape:

```go
signature, err := providerutil.Prepare(ctx, "tencent", req, p.defaultSignature, true)
if err != nil { return sms.Submission{}, err }
params := make([]*string, len(req.Message.Params))
for i, param := range req.Message.Params { value := param.Value; params[i] = &value }
request := tc.NewSendSmsRequest()
request.PhoneNumberSet = []*string{tccommon.StringPtr(req.Recipient.String())}
request.SmsSdkAppId = tccommon.StringPtr(p.appID)
request.TemplateId = tccommon.StringPtr(req.Message.TemplateID)
request.SignName = tccommon.StringPtr(signature)
request.TemplateParamSet = params
response, err := p.client.SendSmsWithContext(ctx, request)
```

On transport/Context errors call `classifyError`; nil/malformed responses are `KindInternal`. Require exactly one `SendStatus`. `Code == "Ok"` produces the Submission; other status codes are classified from the status, with sanitized Message and RequestID.

- [ ] **Step 5: Implement the explicit native classification table**

`errors.go` maps code prefixes `AuthFailure.` and `InvalidCredential` to Authentication; `RequestLimitExceeded` and `LimitExceeded.` to RateLimited; `InternalError` and `ResourceUnavailable.` to Unavailable; status-set business codes to Rejected; `context.Canceled`, `context.DeadlineExceeded`, `net.Error`, and `url.Error` after invocation to UnknownOutcome; unrecognized SDK construction/decoding failures to Internal. Preserve native Code, RequestID, sanitized Message, and Cause.

- [ ] **Step 6: Run Tencent focused and full tests**

Run: `gofmt -w provider/tencent && go test -race ./provider/tencent && go test ./...`

Expected: PASS, including a test whose fake returns `context.DeadlineExceeded` and matches both `sms.ErrUnknownOutcome` and `context.DeadlineExceeded`.

- [ ] **Step 7: Commit Tencent support**

```bash
git add go.mod go.sum provider/tencent
git commit -m "feat(tencent): add SMS provider"
```

---

### Task 5: Alibaba Cloud SMS Provider

**Files:**
- Create: `provider/aliyun/config.go`
- Create: `provider/aliyun/provider.go`
- Create: `provider/aliyun/errors.go`
- Test: `provider/aliyun/provider_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: root SMS types, `providerutil.ChinaNational`, Alibaba `SendSmsWithContext`.
- Produces: `aliyun.Config`, matching `Option` functions, `aliyun.New`, and `sms.Sender` implementation.

- [ ] **Step 1: Write fake-client tests**

Use the exact client boundary:

```go
type apiClient interface {
    SendSmsWithContext(context.Context, *ali.SendSmsRequest, *dara.RuntimeOptions) (*ali.SendSmsResponse, error)
}
```

Test that `+8613812345678` becomes `13812345678`, ordered named parameters become JSON `{"code":"123456","minutes":"5"}`, `Autoretry` is false, `MaxAttempts` is 1, and the fake is called once. A body with `Code: OK`, `BizId: biz-1`, `RequestId: request-1` produces a Submission. A non-`+86` Recipient fails with `ErrInvalidRequest` before the fake is called. A body code `isv.BUSINESS_LIMIT_CONTROL` maps to RateLimited.

- [ ] **Step 2: Pin the official SDK and verify failure**

Run: `go get github.com/alibabacloud-go/dysmsapi-20170525/v5@v5.6.0 && go test ./provider/aliyun`

Expected: FAIL because `Config`, `New`, and `Provider.Send` do not exist.

- [ ] **Step 3: Implement Alibaba config, HTTP adapter, and constructor**

```go
type Config struct {
    AccessKeyID, AccessKeySecret, Region, DefaultSignatureRef string
}

type Provider struct {
    client apiClient
    runtime *dara.RuntimeOptions
    defaultSignature string
}

autoretry := false
maxAttempts := 1
runtime := &dara.RuntimeOptions{Autoretry: &autoretry, MaxAttempts: &maxAttempts}
```

`New` validates all credentials and Region, builds `openapiutil.Config` with AccessKey ID/secret, Region, optional endpoint, and this adapter for the injected `*http.Client`. It constructs the v5 Dysmsapi client and stores the immutable runtime options.

```go
type daraHTTPClient struct { client *http.Client }

func (c daraHTTPClient) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
    return c.client.Do(req)
}
```

- [ ] **Step 4: Implement Alibaba request mapping and Send**

`Send` calls `Prepare(..., true)`, converts only `+86` through `ChinaNational`, builds a fresh `map[string]string`, marshals it with `encoding/json`, and sends `PhoneNumbers`, `SignName`, `TemplateCode`, and `TemplateParam`. Never reuse the map or SDK request between goroutines.

- [ ] **Step 5: Implement Alibaba response and error mapping**

Treat body Code `OK` as accepted. Map `isv.BUSINESS_LIMIT_CONTROL` and `isv.DAY_LIMIT_CONTROL` to RateLimited; `InvalidAccessKeyId.NotFound`, `SignatureDoesNotMatch`, and HTTP 401/403 SDK errors to Authentication; explicit server unavailable/5xx errors to Unavailable; other `isv.*` body codes to Rejected; post-invocation Context/network errors to UnknownOutcome; malformed nil responses to Internal.

- [ ] **Step 6: Run Alibaba focused and full tests**

Run: `gofmt -w provider/aliyun && go test -race ./provider/aliyun && go test ./...`

Expected: PASS with exactly one fake invocation in every outbound test.

- [ ] **Step 7: Commit Alibaba support**

```bash
git add go.mod go.sum provider/aliyun
git commit -m "feat(aliyun): add SMS provider"
```

---

### Task 6: UCloud USMS Direct HTTP Provider

**Files:**
- Create: `provider/ucloud/config.go`
- Create: `provider/ucloud/provider.go`
- Create: `provider/ucloud/sign.go`
- Create: `provider/ucloud/errors.go`
- Test: `provider/ucloud/provider_test.go`
- Test: `provider/ucloud/sign_test.go`

**Interfaces:**
- Consumes: root types, `providerutil.UCloudNumber`, injected `*http.Client`.
- Produces: `ucloud.Config`, `ucloud.WithHTTPClient`, `ucloud.WithEndpoint`, `ucloud.New`, and `sms.Sender` implementation.

- [ ] **Step 1: Write failing signing and request tests**

Copy this official UCloud SDK credential vector into `sign_test.go`. It verifies sorting uses raw key/value concatenation before URL encoding.

```go
func TestSign(t *testing.T) {
    values := map[string]string{
        "Action": "CreateUHostInstance", "CPU": "2", "ChargeType": "Month",
        "DiskSpace": "10", "ImageId": "f43736e1-65a5-4bea-ad2e-8a46e18883c2",
        "LoginMode": "Password", "Memory": "2048", "Name": "Host01",
        "Password": "VUNsb3VkLmNu", "PublicKey": "ucloudsomeone@example.com1296235120854146120",
        "Quantity": "1", "Region": "cn-bj2", "SecurityToken": "some_stoken",
        "Zone": "cn-bj2-04",
    }
    got := sign(values, "46f09bb9fab4f12dfc160dae12273d5332b5debe")
    if got != "170c480ad176a247b324eb92a2cfe536aacfbd04" { t.Fatalf("signature=%s", got) }
}
```

Use `httptest.Server` to assert one POST form request with `Action=SendUSMSMessage`, `PhoneNumbers.0=(86)13812345678`, ordered `TemplateParams.0`/`.1`, `TemplateId`, `SigContent`, `ProjectId`, `Region`, `PublicKey`, and a non-empty `Signature`. Return `{"RetCode":0,"Message":"","SessionNo":"session-1"}` and assert MessageID `session-1`.

- [ ] **Step 2: Run tests and confirm the package is missing**

Run: `go test ./provider/ucloud`

Expected: FAIL because signing and Provider symbols are undefined.

- [ ] **Step 3: Implement and verify UCloud signing**

```go
func sign(values map[string]string, privateKey string) string {
    keys := make([]string, 0, len(values))
    for key := range values { keys = append(keys, key) }
    sort.Strings(keys)
    var source strings.Builder
    for _, key := range keys { source.WriteString(key); source.WriteString(values[key]) }
    source.WriteString(privateKey)
    sum := sha1.Sum([]byte(source.String()))
    return hex.EncodeToString(sum[:])
}
```

Run: `gofmt -w provider/ucloud/sign.go provider/ucloud/sign_test.go && go test ./provider/ucloud -run TestSign -count=1`

Expected: PASS against the official `170c480ad176a247b324eb92a2cfe536aacfbd04` vector.

- [ ] **Step 4: Implement UCloud config and one-shot form submission**

Config fields are `PublicKey`, `PrivateKey`, `ProjectID`, `Region`, and `DefaultSignatureRef`; endpoint defaults to `https://api.ucloud.cn`. `Send` calls `Prepare(..., false)`, formats the phone, fills indexed fields without batching, adds PublicKey, calculates Signature before URL encoding, creates exactly one `http.NewRequestWithContext` POST with `application/x-www-form-urlencoded`, and executes exactly one `client.Do`.

- [ ] **Step 5: Implement UCloud response classification**

HTTP 401/403 maps to Authentication, 429 to RateLimited, 500-599 to Unavailable, other non-2xx to Rejected. A 2xx response with nonzero `RetCode` maps conservatively to Rejected while preserving decimal RetCode and sanitized Message. JSON decode or missing `SessionNo` after RetCode 0 maps to Internal. Any `client.Do` error after invocation maps to UnknownOutcome and unwraps its Context/network cause.

- [ ] **Step 6: Run UCloud focused and full tests**

Run: `gofmt -w provider/ucloud && go test -race ./provider/ucloud && go test ./...`

Expected: PASS, including Context cancellation and call-count-one tests.

- [ ] **Step 7: Commit UCloud support**

```bash
git add provider/ucloud
git commit -m "feat(ucloud): add USMS provider"
```

---

### Task 7: Qiniu Cloud SMS Direct HTTP Provider

**Files:**
- Create: `provider/qiniu/config.go`
- Create: `provider/qiniu/provider.go`
- Create: `provider/qiniu/sign.go`
- Create: `provider/qiniu/errors.go`
- Test: `provider/qiniu/provider_test.go`
- Test: `provider/qiniu/sign_test.go`

**Interfaces:**
- Consumes: root types, `providerutil.ChinaNational`, injected HTTP client.
- Produces: `qiniu.Config`, options, `qiniu.New`, Qiniu request signer, and `sms.Sender` implementation.

- [ ] **Step 1: Write failing canonical-signature and request tests**

For signer tests construct a POST request to `https://sms.qiniuapi.com/v1/message` with `Content-Type: application/json` and body `{"signature_id":"sig-1"}`. Independently calculate HMAC-SHA1 over this exact canonical byte sequence:

```text
POST /v1/message
Host: sms.qiniuapi.com
Content-Type: application/json

{"signature_id":"sig-1"}
```

Using Access Key `access-key` and Secret Key `secret-key`, assert the Authorization header is exactly `Qiniu access-key:G60at940HrcM9TDxo_b2z-oEUAg=`. The request test uses `httptest.Server`, asserts exactly one JSON request containing `signature_id`, `template_id`, one domestic mobile without `+86`, and named `parameters`, then returns `{"job_id":"job-1"}` with `X-Reqid: request-1`.

- [ ] **Step 2: Run tests and confirm missing implementation**

Run: `go test ./provider/qiniu`

Expected: FAIL with undefined signer and Provider types.

- [ ] **Step 3: Implement and verify Qiniu signing**

```go
func authorization(req *http.Request, accessKey, secretKey string, body []byte) string {
    host := req.Host
    if host == "" { host = req.URL.Host }
    canonical := req.Method + " " + req.URL.EscapedPath()
    if req.URL.RawQuery != "" { canonical += "?" + req.URL.RawQuery }
    canonical += "\nHost: " + host
    if contentType := req.Header.Get("Content-Type"); contentType != "" { canonical += "\nContent-Type: " + contentType }
    canonical += "\n\n" + string(body)
    mac := hmac.New(sha1.New, []byte(secretKey))
    _, _ = mac.Write([]byte(canonical))
    return "Qiniu " + accessKey + ":" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
}
```

Run: `gofmt -w provider/qiniu/sign.go provider/qiniu/sign_test.go && go test ./provider/qiniu -run TestAuthorization -count=1`

Expected: PASS with `Qiniu access-key:G60at940HrcM9TDxo_b2z-oEUAg=`.

- [ ] **Step 4: Implement Qiniu config and request mapping**

Config contains AccessKey, SecretKey, and DefaultSignatureRef; endpoint defaults to `https://sms.qiniuapi.com/v1/message`. `Send` requires SignatureRef, supports only `+86` in v1, builds a fresh JSON struct with fields `signature_id`, `template_id`, `mobiles` containing one number, and `parameters map[string]string`, signs the exact marshaled body, and calls `client.Do` once with the request Context.

- [ ] **Step 5: Implement Qiniu HTTP errors**

Capture `X-Reqid` on both success and failure. Map 401/403 to Authentication, 429 to RateLimited, 500-599 to Unavailable, other non-2xx to Rejected using JSON field `error` as sanitized Message. A 2xx malformed body or empty `job_id` is Internal. Transport/Context failures after `Do` begins are UnknownOutcome.

- [ ] **Step 6: Run Qiniu focused and full tests**

Run: `gofmt -w provider/qiniu && go test -race ./provider/qiniu && go test ./...`

Expected: PASS, with the server observing one request and the error text containing no full Recipient.

- [ ] **Step 7: Commit Qiniu support**

```bash
git add provider/qiniu
git commit -m "feat(qiniu): add SMS provider"
```

---

### Task 8: Yunpian Template SMS Direct HTTP Provider

**Files:**
- Create: `provider/yunpian/config.go`
- Create: `provider/yunpian/provider.go`
- Create: `provider/yunpian/errors.go`
- Test: `provider/yunpian/provider_test.go`

**Interfaces:**
- Consumes: root SMS types and injected HTTP client.
- Produces: `yunpian.Config`, options, `yunpian.New`, template-value encoder, and `sms.Sender` implementation.

- [ ] **Step 1: Write failing form and response tests**

Use `httptest.Server` to require exactly one POST to `/v2/sms/tpl_single_send.json`. Parse the form and assert `apikey`, E.164 `mobile`, `tpl_id`, and decoded `tpl_value` equal `#code#=123456&#minutes#=5` in input order. Set a non-empty Request SignatureRef and assert no signature field is sent. Return:

```json
{"code":0,"msg":"发送成功","count":1,"fee":0.05,"unit":"RMB","mobile":"13812345678","sid":1234567890}
```

Assert MessageID `1234567890`, Metadata `count=1`, `fee=0.05`, and `unit=RMB`, with no mobile Metadata.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./provider/yunpian`

Expected: FAIL because `Config`, encoder, and Provider do not exist.

- [ ] **Step 3: Implement Yunpian template-value encoding**

Config contains only APIKey; endpoint defaults to `https://sms.yunpian.com/v2/sms/tpl_single_send.json`. Options inject HTTP client or endpoint. `Send` calls `Prepare(..., false)` and deliberately discards the resolved SignatureRef because Yunpian template sending binds the signature to the template. Before constructing the request, it rejects any empty parameter value with `KindInvalidRequest`, because Yunpian's `tpl_value` contract requires non-empty names and values.

```go
func templateValue(params []sms.TemplateParam) string {
    pairs := make([]string, len(params))
    for i, param := range params { pairs[i] = "#" + param.Name + "#=" + param.Value }
    return strings.Join(pairs, "&")
}
```

- [ ] **Step 4: Implement Yunpian config and one-shot form submission**

Place that string into `url.Values`; let the outer form encoding escape `#`, `&`, names, and values exactly once. Use `http.NewRequestWithContext`, set form content type, and call `client.Do` once.

- [ ] **Step 5: Add Yunpian response and error mapping**

Decode `sid` with `json.Number` to avoid float precision loss. Code 0 with missing sid is Internal. HTTP 401/403 maps Authentication, 429 maps RateLimited, 500-599 maps Unavailable, other non-2xx and nonzero Yunpian body codes map Rejected while retaining decimal code, sanitized `msg`, and `detail`. Transport/Context errors map UnknownOutcome.

- [ ] **Step 6: Run Yunpian focused and full tests**

Run: `gofmt -w provider/yunpian && go test -race ./provider/yunpian && go test ./...`

Expected: PASS with one outbound call and exact `tpl_value` round trip.

- [ ] **Step 7: Commit Yunpian support**

```bash
git add provider/yunpian
git commit -m "feat(yunpian): add template SMS provider"
```

---

### Task 9: Documentation, CI, and Live Integration Gates

**Files:**
- Modify: `README.md`
- Create: `example_test.go`
- Create: `.github/workflows/ci.yml`
- Create: `docs/integration-testing.md`
- Create: `internal/integrationtest/integrationtest.go`
- Create: `provider/tencent/integration_test.go`
- Create: `provider/aliyun/integration_test.go`
- Create: `provider/ucloud/integration_test.go`
- Create: `provider/qiniu/integration_test.go`
- Create: `provider/yunpian/integration_test.go`

**Interfaces:**
- Consumes: all public constructors and `sms.Sender` implementations from Tasks 1-8.
- Produces: compile-checked user guidance, two-version CI, and credential-gated proof for every advertised Provider.

- [ ] **Step 1: Add compile-checked README examples**

Document installation and one complete Tencent example using `ParseRecipient`, `tencent.New`, `context.WithTimeout`, ordered named params, `Submission`, and `errors.Is(err, sms.ErrUnknownOutcome)`. Add concise construction tables for the other four Providers and list the non-goals verbatim from the design. Put the runnable core example in `ExampleSender` so `go test ./...` compiles it.

```go
package sms_test

import (
    "context"
    "errors"
    "log"
    "time"

    sms "github.com/feymanlee/go-sms"
    "github.com/feymanlee/go-sms/provider/tencent"
)

func ExampleSender() {
    provider, err := tencent.New(tencent.Config{
        SecretID: "example-secret-id", SecretKey: "example-secret-key",
        SMSAppID: "1400000000", Region: "ap-guangzhou",
        DefaultSignatureRef: "Example",
    })
    if err != nil { log.Print(err); return }
    recipient, err := sms.ParseRecipient("+8613812345678")
    if err != nil { log.Print(err); return }
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    submission, err := provider.Send(ctx, sms.Request{
        Recipient: recipient,
        Message: sms.TemplateMessage{
            TemplateID: "123456",
            Params: []sms.TemplateParam{{Name: "code", Value: "654321"}},
        },
    })
    if errors.Is(err, sms.ErrUnknownOutcome) { log.Print("send outcome is unknown"); return }
    if err != nil { log.Print(err); return }
    log.Printf("accepted by %s as %s", submission.Provider, submission.MessageID)
}
```

- [ ] **Step 2: Add the CI workflow**

```yaml
name: ci
on:
  push:
  pull_request:
jobs:
  test:
    strategy:
      matrix:
        go: ["1.25.x", "1.26.x"]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
          cache: true
      - run: go test ./...
      - if: matrix.go == '1.26.x'
        run: go test -race ./...
```

- [ ] **Step 3: Add the integration test helper**

```go
//go:build integration

package integrationtest

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    sms "github.com/feymanlee/go-sms"
)

func Env(t *testing.T, names ...string) map[string]string {
    t.Helper()
    values := make(map[string]string, len(names))
    missing := make([]string, 0)
    for _, name := range names {
        value := os.Getenv(name)
        if value == "" { missing = append(missing, name); continue }
        values[name] = value
    }
    if len(missing) > 0 { t.Skipf("integration variables required: %s", strings.Join(missing, ", ")) }
    return values
}

func Send(t *testing.T, sender sms.Sender, templateID, signatureRef string) {
    t.Helper()
    common := Env(t, "GO_SMS_TEST_RECIPIENT", "GO_SMS_TEST_PARAM_NAME", "GO_SMS_TEST_PARAM_VALUE")
    recipient, err := sms.ParseRecipient(common["GO_SMS_TEST_RECIPIENT"])
    if err != nil { t.Fatal(err) }
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    submission, err := sender.Send(ctx, sms.Request{
        Recipient: recipient,
        SignatureRef: signatureRef,
        Message: sms.TemplateMessage{TemplateID: templateID, Params: []sms.TemplateParam{{
            Name: common["GO_SMS_TEST_PARAM_NAME"], Value: common["GO_SMS_TEST_PARAM_VALUE"],
        }}},
    })
    if err != nil { t.Fatal(err) }
    t.Logf("date=%s provider=%s message_id=%s request_id=%s", time.Now().UTC().Format(time.DateOnly), submission.Provider, submission.MessageID, submission.RequestID)
}
```

- [ ] **Step 4: Add Tencent live integration test**

```go
//go:build integration

package tencent

import (
    "testing"

    "github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
    v := integrationtest.Env(t, "TENCENT_SECRET_ID", "TENCENT_SECRET_KEY", "TENCENT_SMS_APP_ID", "TENCENT_REGION", "TENCENT_TEMPLATE_ID", "TENCENT_SIGNATURE_REF")
    provider, err := New(Config{SecretID: v["TENCENT_SECRET_ID"], SecretKey: v["TENCENT_SECRET_KEY"], SMSAppID: v["TENCENT_SMS_APP_ID"], Region: v["TENCENT_REGION"]})
    if err != nil { t.Fatal(err) }
    integrationtest.Send(t, provider, v["TENCENT_TEMPLATE_ID"], v["TENCENT_SIGNATURE_REF"])
}
```

- [ ] **Step 5: Add Alibaba live integration test**

```go
//go:build integration

package aliyun

import (
    "testing"

    "github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
    v := integrationtest.Env(t, "ALIYUN_ACCESS_KEY_ID", "ALIYUN_ACCESS_KEY_SECRET", "ALIYUN_REGION", "ALIYUN_TEMPLATE_ID", "ALIYUN_SIGNATURE_REF")
    provider, err := New(Config{AccessKeyID: v["ALIYUN_ACCESS_KEY_ID"], AccessKeySecret: v["ALIYUN_ACCESS_KEY_SECRET"], Region: v["ALIYUN_REGION"]})
    if err != nil { t.Fatal(err) }
    integrationtest.Send(t, provider, v["ALIYUN_TEMPLATE_ID"], v["ALIYUN_SIGNATURE_REF"])
}
```

- [ ] **Step 6: Add UCloud live integration test**

```go
//go:build integration

package ucloud

import (
    "testing"

    "github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
    v := integrationtest.Env(t, "UCLOUD_PUBLIC_KEY", "UCLOUD_PRIVATE_KEY", "UCLOUD_PROJECT_ID", "UCLOUD_REGION", "UCLOUD_TEMPLATE_ID", "UCLOUD_SIGNATURE_REF")
    provider, err := New(Config{PublicKey: v["UCLOUD_PUBLIC_KEY"], PrivateKey: v["UCLOUD_PRIVATE_KEY"], ProjectID: v["UCLOUD_PROJECT_ID"], Region: v["UCLOUD_REGION"]})
    if err != nil { t.Fatal(err) }
    integrationtest.Send(t, provider, v["UCLOUD_TEMPLATE_ID"], v["UCLOUD_SIGNATURE_REF"])
}
```

- [ ] **Step 7: Add Qiniu live integration test**

```go
//go:build integration

package qiniu

import (
    "testing"

    "github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
    v := integrationtest.Env(t, "QINIU_ACCESS_KEY", "QINIU_SECRET_KEY", "QINIU_TEMPLATE_ID", "QINIU_SIGNATURE_REF")
    provider, err := New(Config{AccessKey: v["QINIU_ACCESS_KEY"], SecretKey: v["QINIU_SECRET_KEY"]})
    if err != nil { t.Fatal(err) }
    integrationtest.Send(t, provider, v["QINIU_TEMPLATE_ID"], v["QINIU_SIGNATURE_REF"])
}
```

- [ ] **Step 8: Add Yunpian live integration test**

```go
//go:build integration

package yunpian

import (
    "testing"

    "github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
    v := integrationtest.Env(t, "YUNPIAN_API_KEY", "YUNPIAN_TEMPLATE_ID")
    provider, err := New(Config{APIKey: v["YUNPIAN_API_KEY"]})
    if err != nil { t.Fatal(err) }
    integrationtest.Send(t, provider, v["YUNPIAN_TEMPLATE_ID"], "")
}
```

- [ ] **Step 9: Document live verification**

Write `docs/integration-testing.md` with this operational contract:

```markdown
# Live Provider Verification

Live tests are excluded from ordinary CI. Export the common Recipient and one template parameter plus the credentials, template ID, and SignatureRef for every Provider listed in the implementation plan, then run:

`go test -tags=integration ./provider/... -run TestIntegrationSend -count=1 -v`

Each test submits exactly one SMS. Store the UTC date and redacted MessageID/RequestID in release notes. Never commit credentials, Recipient values, template parameter values, shell history exports, or test output containing those values.
```

- [ ] **Step 10: Run the complete local verification suite**

Run:

```bash
go mod tidy
gofmt -w $(find . -name '*.go' -type f)
go vet ./...
go test ./...
go test -race ./...
git diff --check
```

Expected: every command exits 0; integration tests are excluded without the build tag; `git status --short` contains only intended source, test, documentation, workflow, and module files.

- [ ] **Step 11: Commit documentation and CI**

```bash
git add README.md example_test.go .github/workflows/ci.yml docs/integration-testing.md internal/integrationtest provider/*/integration_test.go go.mod go.sum
git commit -m "docs: add usage and provider verification workflow"
```

---

## Final Release Check

Before tagging the first release, run the full Task 9 verification commands on Go 1.25 and 1.26, then run the integration command with credentials for all five Providers. Confirm each Provider produced one `Submission`, record the UTC date and redacted RequestID/MessageID in release notes, and confirm no credentials or complete phone numbers appear in `git grep`, test output, or the final diff.
