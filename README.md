# go-sms

`go-sms` provides one common Go contract for sending a Provider-native
template SMS through Tencent Cloud, Alibaba Cloud, UCloud, Qiniu Cloud, or
Yunpian. The caller explicitly chooses a Provider for each send attempt.

## Installation

```sh
go get github.com/feymanlee/go-sms
```

The module requires Go 1.25 or later.

## Sending with Tencent Cloud

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	sms "github.com/feymanlee/go-sms"
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
	if errors.Is(err, sms.ErrUnknownOutcome) {
		log.Print("send outcome is unknown")
		return
	}
	if err != nil {
		log.Print(err)
		return
	}

	log.Printf("accepted by %s as %s", submission.Provider, submission.MessageID)
}
```

`Submission` proves only that the selected Provider accepted the send attempt;
it is not a delivery receipt. Template parameters retain both their names and
slice order. A request-level `SignatureRef` overrides the configured default.

The same example is compile-checked in `example_test.go`. It has no expected
output declaration, so ordinary `go test` runs compile it without sending an
SMS.

## Provider construction

All constructors return `(*Provider, error)` and accept optional
`WithHTTPClient` and `WithEndpoint` options. The other Providers use these
exact `Config` fields:

| Provider | Package | Construction fields |
|---|---|---|
| Alibaba Cloud | `provider/aliyun` | `AccessKeyID`, `AccessKeySecret`, `Region`, optional `DefaultSignatureRef` |
| UCloud | `provider/ucloud` | `PublicKey`, `PrivateKey`, `ProjectID`, `Region`, optional `DefaultSignatureRef` |
| Qiniu Cloud | `provider/qiniu` | `AccessKey`, `SecretKey`, optional `DefaultSignatureRef` |
| Yunpian | `provider/yunpian` | `APIKey` |

Credentials and Provider business settings are explicit constructor inputs.
The library does not load them from environment variables or configuration
files. Default clients retain Go's standard `HTTP_PROXY`, `HTTPS_PROXY`, and
`NO_PROXY` discovery; inject an `http.Client` to use a different transport
policy.

## Error handling

Use `errors.Is` with `ErrInvalidRequest`, `ErrAuthentication`,
`ErrRateLimited`, `ErrRejected`, `ErrUnavailable`, `ErrUnknownOutcome`, and
`ErrInternal`. Use `errors.As` with `*sms.SendError` when Provider code,
request ID, or other structured diagnostic fields are needed. An
`ErrUnknownOutcome` means the caller must not assume that the SMS was not
accepted.

## Non-goals

首版不包含：

- Provider 自动选择、路由策略或中央 Manager
- 同一 Provider 自动重试、跨 Provider 降级或故障转移
- 多收件人、批量或个性化批量发送
- 原始文本短信
- 逻辑模板或逻辑签名注册表
- 送达回执查询、回调处理或最终送达保证
- 异步队列、后台 worker、日志或指标
- 自动读取环境变量或配置文件
- Provider 专属的单次发送扩展选项
- 成功响应中的官方 SDK 原始对象

See [Live Provider Verification](docs/integration-testing.md) for the
credential-gated release checks.
