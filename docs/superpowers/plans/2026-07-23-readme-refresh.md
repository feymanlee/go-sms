# Bilingual README Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish matching English and Simplified Chinese README files that accurately document the `v0.1.0` prerelease, five built-in Providers, safe Failure handling, and version-one guarantees.

**Architecture:** `README.md` remains the canonical English document and `README_zh.md` becomes its complete Simplified Chinese mirror. Both files share identical badges, links, tables, and code blocks; only headings and explanatory prose differ. The refresh changes documentation only and preserves the compile-checked Tencent example.

**Tech Stack:** GitHub-flavored Markdown, Go 1.25+, existing `example_test.go`, shell-based parity checks.

## Global Constraints

- `README.md` is canonical English; `README_zh.md` is a complete Simplified Chinese mirror.
- Both files have exactly seven level-two sections in this order: installation, quick start, supported Providers, Send Failure handling, behavioral guarantees, non-goals, live Provider verification.
- Both files use the same language links, CI badge, Go Reference badge, Release badge, table shape, code blocks, URLs, `v0.1.0`, Go 1.25 requirement, category names, and prerelease status.
- The prerelease note says Go 1.25 and Go 1.26 CI passed and credential-gated live SMS verification for all five Providers is not complete.
- Do not imply production readiness or claim live Provider verification passed.
- The Provider signature policies are Tencent required, Alibaba required, UCloud optional, Qiniu required, and Yunpian not used.
- The quick-start Go block is byte-for-byte identical in both files and remains equivalent to `example_test.go`.
- Do not change production Go files, Provider behavior, dependencies, tags, or releases. Do not send live SMS messages.

## File Map

- Modify: `README.md` — canonical English public documentation.
- Create: `README_zh.md` — complete Simplified Chinese mirror.
- Verify: `example_test.go` — compile-checked source for the shared quick-start example; no edits.
- Consume: `docs/superpowers/specs/2026-07-23-readme-refresh-design.md` — approved design and synchronization contract.

---

### Task 1: Publish the Mirrored README Pair

**Files:**
- Modify: `README.md`
- Create: `README_zh.md`
- Test: shell parity checks and existing Go tests

**Interfaces:**
- Consumes: public constructors, `sms.ParseRecipient`, `Sender.Send`, `failure.From`, `Failure.Category`, `Failure.Details`, and `Failure.UnknownOutcome` already compiled by `example_test.go`.
- Produces: two public documentation entry points with the same technical contract and language-specific prose.

- [ ] **Step 1: Run the documentation gate and verify RED**

Run:

```bash
test -f README_zh.md && \
rg -F '[![CI](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml/badge.svg)]' README.md README_zh.md && \
rg -F 'go get github.com/feymanlee/go-sms@v0.1.0' README.md README_zh.md
```

Expected: FAIL at `test -f README_zh.md` because the Chinese mirror does not exist.

- [ ] **Step 2: Replace `README.md` with the exact English document**

Use this complete content:

````markdown
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
````

- [ ] **Step 3: Create `README_zh.md` with the exact Chinese mirror**

Use this complete content:

````markdown
# go-sms

[English](README.md) | [简体中文](README_zh.md)

[![CI](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml/badge.svg)](https://github.com/feymanlee/go-sms/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/feymanlee/go-sms.svg)](https://pkg.go.dev/github.com/feymanlee/go-sms)
[![Release](https://img.shields.io/github/v/release/feymanlee/go-sms?include_prereleases&sort=semver)](https://github.com/feymanlee/go-sms/releases)

`go-sms` 为腾讯云、阿里云、UCloud、七牛云和云片的 Provider 原生模板短信提供统一的 Go 发送契约。调用方为每次 Send Attempt 显式选择一个 Provider。

> [!WARNING]
> `v0.1.0` 是预发布版本。项目已通过 Go 1.25 和 Go 1.26 CI（包括 race 测试），但五家 Provider 的凭据受控真实短信验收尚未全部完成。在生产环境使用前，请在获批环境中验证每个已配置的 Provider。

## 安装

```sh
go get github.com/feymanlee/go-sms@v0.1.0
```

该模块要求 Go 1.25 或更高版本。

## 快速上手

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

`Submission` 只表示所选 Provider 已接受本次 Send Attempt，不是送达回执。模板参数同时保留名称和切片顺序。请求级 `SignatureRef` 会覆盖配置中的默认值。

同一示例会在 [`example_test.go`](example_test.go) 中参与编译检查。示例没有预期输出声明，因此普通测试只会编译它，不会发送短信。

## 支持的 Provider

| Provider | 导入路径 | 必填构造字段 | Signature Reference |
|---|---|---|---|
| 腾讯云 | `github.com/feymanlee/go-sms/provider/tencent` | `SecretID`, `SecretKey`, `SMSAppID`, `Region` | 必填 |
| 阿里云 | `github.com/feymanlee/go-sms/provider/aliyun` | `AccessKeyID`, `AccessKeySecret`, `Region` | 必填 |
| UCloud | `github.com/feymanlee/go-sms/provider/ucloud` | `PublicKey`, `PrivateKey`, `ProjectID`, `Region` | 可选 |
| 七牛云 | `github.com/feymanlee/go-sms/provider/qiniu` | `AccessKey`, `SecretKey` | 必填 |
| 云片 | `github.com/feymanlee/go-sms/provider/yunpian` | `APIKey` | 不使用 |

所有构造函数都返回 `(*Provider, error)`，并接受 `WithHTTPClient` 和 `WithEndpoint` 选项。腾讯云和阿里云将自定义 endpoint 交给官方 SDK，因此该值是不含 URL scheme 和路径的主机名。UCloud、七牛云和云片使用直接 HTTP，请提供绝对 `http://` 或 `https://` URL；七牛云和云片的 endpoint 还需包含发送 API 路径。

凭据和 Provider 业务设置都是显式构造参数。库不会读取环境变量或配置文件。默认客户端保留 Go 标准代理发现；需要不同传输策略时可注入 `http.Client`。

## Send Failure 处理

Provider 调用前的错误都是普通 error，包括验证、已结束的 Context、Provider 特定 preflight 拒绝、编码和请求构造失败。它们不满足 `failure.From`。

调用后，明确的 Provider 决策使用 `authentication`、`rate_limited`、`rejected` 或 `unavailable`。不确定结果使用 `unknown_outcome`。通过 `failure.From(err)`、`Category()`、`Details()` 和 `UnknownOutcome()` 检查 Send Failure。

Failure 只暴露安全的结构化诊断。`Details()` 可以包含 Provider、Code 和 RequestID，但不会暴露原生错误身份、Provider 响应文本、Recipient、模板值或请求体。当类别为 `unknown_outcome` 时，不要假设 Provider 已拒绝请求；应根据应用的幂等策略先对账再重试。

## 行为保证

- 每次 Send Attempt 只面向一个由调用方显式选择的 Provider。
- 库不会自动重试、跟随重定向、路由或故障转移。
- 在仓库 race 测试覆盖下，Provider 实例可安全并发使用。
- 默认 HTTP 客户端使用有界超时和 Go 标准代理发现。
- `Submission` 证明 Provider 已接受请求，不代表短信最终送达。

## 非目标

版本一不包含：

- Provider 自动选择、路由策略或中央 Manager
- 自动重试、跨 Provider 降级或故障转移
- 多 Recipient、批量发送或个性化批量发送
- 原始文本短信
- 逻辑模板或签名注册表
- 送达回执查询、回调处理或最终送达保证
- 异步队列、后台 worker、日志或指标
- 自动读取环境变量或配置文件
- Provider 特定的单次发送扩展选项
- 成功结果中的官方 SDK 响应对象

## 真实 Provider 验证

真实测试由凭据控制，并从普通 CI 中排除。此预发布版本尚未完成全部五家 Provider 的验证。获批测试流程和敏感数据规则见[真实 Provider 验证](docs/integration-testing.md)。
````

- [ ] **Step 4: Run deterministic mirror checks and verify GREEN**

Run:

```bash
test -f README.md
test -f README_zh.md
test "$(rg -c '^## ' README.md)" -eq 7
test "$(rg -c '^## ' README_zh.md)" -eq 7
diff -u <(rg '^## ' README.md) <(printf '%s\n' '## Installation' '## Quick start' '## Supported Providers' '## Send Failure handling' '## Behavioral guarantees' '## Non-goals' '## Live Provider verification')
diff -u <(rg '^## ' README_zh.md) <(printf '%s\n' '## 安装' '## 快速上手' '## 支持的 Provider' '## Send Failure 处理' '## 行为保证' '## 非目标' '## 真实 Provider 验证')
diff -u <(awk '/^```/{print; inside=!inside; next} inside' README.md) <(awk '/^```/{print; inside=!inside; next} inside' README_zh.md)
test "$(rg -c '^\| (Tencent Cloud|Alibaba Cloud|UCloud|Qiniu Cloud|Yunpian) ' README.md)" -eq 5
test "$(rg -c '^\| (腾讯云|阿里云|UCloud|七牛云|云片) ' README_zh.md)" -eq 5
```

Expected: every command exits 0 and both `diff` commands produce no output.

- [ ] **Step 5: Check local links and public facts**

Run:

```bash
for file in README.md README_zh.md; do
  for target in $(rg -o '\]\(([^)#]+\.md)\)' "$file" -r '$1'); do
    test -f "$target" || exit 1
  done
done
rg -F 'go get github.com/feymanlee/go-sms@v0.1.0' README.md README_zh.md
rg -F 'Go 1.25' README.md README_zh.md
rg -F 'unknown_outcome' README.md README_zh.md
```

Expected: all commands exit 0; each `rg` prints one matching line from each README.

- [ ] **Step 6: Compile and run the repository tests**

Run:

```bash
go test -count=1 .
go test -count=1 ./...
git diff --check
git status --short
```

Expected: both Go commands pass, `git diff --check` has no output, and status lists only `README.md`, `README_zh.md`, and this plan if it has not already been committed.

- [ ] **Step 7: Review the diff for sensitive or unsupported claims**

Run:

```bash
git diff -- README.md README_zh.md
rg -n 'production.ready|live verification.*(passed|complete)|AKIA[0-9A-Z]{16}|BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY' README.md README_zh.md || true
```

Expected: the diff contains only the approved documentation; the search produces no output.

- [ ] **Step 8: Commit the mirrored documentation**

```bash
git add README.md README_zh.md docs/superpowers/plans/2026-07-23-readme-refresh.md
git commit -m "docs(readme): add bilingual release guide"
```

Expected: one documentation-only commit containing the synchronized README pair and this plan.

---

## Self-Review Checklist

- [x] Every approved design requirement appears in Task 1.
- [x] The English and Chinese target documents have matching section counts, tables, code fences, URLs, versions, categories, and status claims.
- [x] The three badge image and target URLs are exact and verifiable.
- [x] The Provider fields and Signature Reference policies match the current adapters.
- [x] No placeholder, roadmap commitment, production change, new release, or live send is included.
- [x] Every verification command is executable from the repository root.
