# go-sms

[English](README.md) | [简体中文](README_zh.md)

[![CI](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml/badge.svg)](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/feymanlee/go-sms.svg)](https://pkg.go.dev/github.com/feymanlee/go-sms)
[![Release](https://img.shields.io/github/v/release/feymanlee/go-sms?include_prereleases&sort=semver)](https://github.com/feymanlee/go-sms/releases)

`go-sms` provides one Go contract for sending Provider-native template SMS messages through Tencent Cloud, Alibaba Cloud, UCloud, Qiniu Cloud, or Yunpian. The caller explicitly chooses one Provider for each Send Attempt.

> [!WARNING]
> `v0.1.0` is a prerelease. It passes CI on Go 1.25 and Go 1.26, including race tests, but credential-gated live SMS verification has not yet been completed for all five Providers. Verify each configured Provider in an approved environment before production use.

## Installation

```sh
go get github.com/feymanlee/go-sms@v0.1.0
```

The module requires Go 1.25 or later.

## Quick start

```go
package main

import (
	"context"
	"log"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
	"github.com/feymanlee/go-sms/provider/tencent"
)

func main() {
	provider, err := tencent.New(tencent.Config{
		SecretID:            "example-secret-id",
		SecretKey:           "example-secret-key",
		SMSAppID:            "1400000000",
		Region:              "ap-guangzhou",
		DefaultSignatureRef: "Example",
	})
	if err != nil {
		log.Print(err)
		return
	}

	recipient, err := sms.ParseRecipient("+8613812345678")
	if err != nil {
		log.Print(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	submission, err := provider.Send(ctx, sms.Request{
		Recipient: recipient,
		Message: sms.TemplateMessage{
			TemplateID: "123456",
			Params: []sms.TemplateParam{
				{Name: "code", Value: "654321"},
			},
		},
	})
	if err != nil {
		if got, ok := failure.From(err); ok {
			details := got.Details()
			log.Printf("SMS Send Attempt failed: category=%s provider=%s code=%s request_id=%s",
				got.Category(), details.Provider, details.Code, details.RequestID)
			if got.UnknownOutcome() {
				log.Print("reconcile before retry")
			}
		} else {
			log.Print(err)
		}
		return
	}

	log.Printf("accepted by %s as %s", submission.Provider, submission.MessageID)
}
```

`Submission` is evidence that the selected Provider accepted the Send Attempt; it is not a delivery receipt. Template parameters preserve both their names and slice order. A request-level `SignatureRef` overrides the configured default.

The same example is compile-checked in [`example_test.go`](example_test.go). It has no expected output declaration, so ordinary tests compile it without sending an SMS.

## Supported Providers

| Provider | Import path | Required constructor fields | Signature Reference |
|---|---|---|---|
| Tencent Cloud | `github.com/feymanlee/go-sms/provider/tencent` | `SecretID`, `SecretKey`, `SMSAppID`, `Region` | Required |
| Alibaba Cloud | `github.com/feymanlee/go-sms/provider/aliyun` | `AccessKeyID`, `AccessKeySecret`, `Region` | Required |
| UCloud | `github.com/feymanlee/go-sms/provider/ucloud` | `PublicKey`, `PrivateKey`, `ProjectID`, `Region` | Optional |
| Qiniu Cloud | `github.com/feymanlee/go-sms/provider/qiniu` | `AccessKey`, `SecretKey` | Required |
| Yunpian | `github.com/feymanlee/go-sms/provider/yunpian` | `APIKey` | Not used |

All constructors return `(*Provider, error)` and accept `WithHTTPClient` and `WithEndpoint` options. Tencent Cloud and Alibaba Cloud pass custom endpoints to their official SDKs, so the value is a host without a URL scheme or path. UCloud, Qiniu Cloud, and Yunpian use direct HTTP and require an absolute `http://` or `https://` URL; Qiniu and Yunpian endpoints include the send API path.

Credentials and Provider settings are explicit constructor inputs. The library does not read environment variables or configuration files. Default clients retain Go standard proxy discovery; inject an `http.Client` for a different transport policy.

## Send Failure handling

Errors before Provider invocation are ordinary errors, including validation, an already-done Context, Provider-specific preflight rejection, encoding, and request construction failures. They do not satisfy `failure.From`.

After invocation, explicit Provider decisions use `authentication`, `rate_limited`, `rejected`, or `unavailable`. Indeterminate results use `unknown_outcome`. Inspect a Send Failure with `failure.From(err)`, `Category()`, `Details()`, and `UnknownOutcome()`.

A Failure exposes only safe structured diagnostics. `Details()` may contain Provider, Code, and RequestID; it never exposes native error identity, Provider response text, Recipient, template values, or request bodies. When the category is `unknown_outcome`, do not assume the Provider rejected the request. Reconcile before retrying according to the application idempotency policy.

## Behavioral guarantees

- Each Send Attempt targets exactly one explicitly selected Provider.
- The library does not automatically retry, follow redirects, route, or fail over.
- Provider instances are safe for concurrent use under the repository race tests.
- Default HTTP clients use bounded timeouts and Go standard proxy discovery.
- A `Submission` proves Provider acceptance, not final SMS delivery.

## Non-goals

Version one does not include:

- automatic Provider selection, routing policies, or a central manager
- automatic retry, cross-Provider fallback, or failover
- multiple Recipients, bulk sending, or personalized bulk sending
- raw text messages
- logical template or signature registries
- delivery receipt queries, callback handling, or final delivery guarantees
- asynchronous queues, background workers, logging, or metrics
- automatic environment-variable or configuration-file loading
- Provider-specific per-send extension options
- official SDK response objects in successful results

## Live Provider verification

Live tests are credential-gated and excluded from normal CI. Verification has not yet been completed for all five Providers for this prerelease. See [Live Provider Verification](docs/integration-testing.md) for the approved test procedure and sensitive-data rules.
