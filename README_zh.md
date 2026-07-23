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
