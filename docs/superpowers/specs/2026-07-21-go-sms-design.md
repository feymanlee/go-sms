# Go SMS 多 Provider 发送库设计

## 目标

`github.com/feymanlee/go-sms` 是一个可复用的 Go 库，为腾讯云 SMS、UCloud、七牛云、阿里云和云片提供统一的模板短信发送契约。首版只抽象五家的共同能力：调用方显式选择一个 Provider，向一个 E.164 收件人提交一条模板短信，并获得已受理凭证或结构化失败信息。

首版不是短信路由器。它不选择 Provider，不自动重试，不跨 Provider 降级，也不把供应商的模板配置变成业务模板注册表。

## 设计原则

- 公共契约只表达五家均可支持的能力。
- 一次 `Send` 调用等于一次且仅一次外部发送尝试。
- Provider 原生类型不得泄漏到根包的公共 API。
- 取消、截止时间和结果不确定性必须显式表达。
- Provider 业务配置和凭证由宿主应用显式注入；库不从环境变量、文件或全局状态解析这些值。Go 标准的 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY` 传输代理发现仍然可用。
- Provider 实例构造后不可变，并可被多个 goroutine 并发使用。

## Module 与包结构

项目使用一个 Go module，最低版本为 Go 1.25：

```text
github.com/feymanlee/go-sms
├── package sms
├── failure
└── provider
    ├── aliyun
    ├── qiniu
    ├── tencent
    ├── ucloud
    └── yunpian
```

根包定义请求、结果和 `Sender` 接口；`failure` 包定义跨 Provider 的安全 Send Failure 契约。每个 Provider 子包包含强类型配置、构造函数、参数转换、底层客户端和错误映射。五个子包与根包使用同一版本发布。未导入的 Provider 包不会进入调用方二进制，但五家 SDK 依赖仍会出现在 module 依赖图中。

## 公共 API

核心契约如下：

```go
type Sender interface {
    Send(ctx context.Context, req Request) (Submission, error)
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

`ParseRecipient` 从字符串创建不可变的 `Recipient`。有效输入必须是严格 E.164 语法：`+` 后是 1 到 15 位数字，第一位不能为零。零值 Recipient 在发送前视为无效。语法校验不判断号码是否真实，也不承诺所选 Provider 支持对应国家或地区。

`TemplateID` 是调用方已经解析出的 Provider 原生模板标识。`TemplateParam` 同时保留名称和顺序：命名型 Provider 使用 `Name`，位置型 Provider 保持切片顺序。名称必须非空且不可重复，值允许为空，无参数模板允许空切片。云片适配器负责把裸名称（例如 `code`）编码成其 `#code#` 形式。

`SignatureRef` 是 Provider 原生签名引用，而不是统一的签名 ID。腾讯、阿里和 UCloud 分别使用签名内容或名称，七牛使用签名 ID，云片模板发送不使用该字段。空值表示使用 Provider 构造配置中的默认值；非空发送级值覆盖默认值。

`Submission` 仅表示 Provider 明确接受了请求，不表示短信已经到达 Recipient。`MessageID` 和 `RequestID` 在 Provider 不返回对应信息时允许为空。`Metadata` 只保存有诊断或业务价值、但无稳定跨 Provider 含义的字符串字段，不保存官方 SDK 对象或完整原始响应。

## Provider 构造

每个 Provider 使用自己的强类型配置，构造时校验固定必填项：

| Provider | 固定配置 |
|---|---|
| 腾讯云 | Secret ID、Secret Key、SMS SDK App ID、Region、可选默认 SignatureRef |
| 阿里云 | AccessKey ID、AccessKey Secret、Region、可选默认 SignatureRef |
| UCloud | Public Key、Private Key、Project ID、Region、可选默认 SignatureRef |
| 七牛云 | Access Key、Secret Key、可选默认 SignatureRef（签名 ID） |
| 云片 | API Key |

每个子包公开 `New(Config, ...Option) (*Provider, error)`。构造选项允许注入 `http.Client` 和自定义 API endpoint，供企业代理、私有网络和测试使用。未注入时使用总超时 10 秒的独立客户端；调用方可用更短的 Context deadline，也可通过注入客户端改变默认值。库不修改 `http.DefaultClient`。凭证来源、轮换和多账号选择由宿主应用负责，多账号通过多个 Provider 实例表示。

默认客户端遵循 Go 的标准代理环境约定。这属于 HTTP 传输策略，不是 Provider 凭证或业务配置；需要完全确定性或不同代理策略的宿主应用可以注入自己的 `http.Client`。

Provider 构造失败用于固定配置错误；发送级输入错误通过 `Send` 返回 `InvalidRequest`。

## 发送流程

1. 检查 Context、Recipient、模板 ID、参数名称和重复项。nil Context 返回 `InvalidRequest`；已取消或已超时的 Context 直接返回 `ctx.Err()`，且不创建外部请求。
2. 用发送级 `SignatureRef` 覆盖 Provider 默认值，并校验该 Provider 的签名要求。
3. 把 E.164 Recipient 转换为目标 API 要求的格式。不支持该国家或地区时返回 `InvalidRequest`，不切换发送 API 或 Provider。
4. 将参数按目标 API 编码。位置型 API 只读取切片顺序，命名型 API 按名称生成对象或表单值。
5. 使用能接受 `context.Context` 且已关闭重试的官方 SDK 方法，或执行一次直接 HTTP 请求。
6. 只有 Provider 明确返回受理状态时才构造 `Submission`；HTTP 200 中的业务拒绝仍转换为 `failure.Failure`。

如果 Context 在请求开始后取消、截止时间到期或连接中断，且无法证明 Provider 未受理，则返回 `UnknownOutcome`。适配器不得为了等待底层 SDK 而启动无法取消的后台 goroutine。

## 五家映射

| Provider | 底层实现 | Recipient 与请求字段 | 参数映射 | Submission |
|---|---|---|---|---|
| 腾讯云 | 官方 `sms/v20210111` SDK | E.164 原样进入 `PhoneNumberSet`；配置提供 `SmsSdkAppId`；`SignatureRef` 进入 `SignName` | 按顺序进入 `TemplateParamSet` | `SerialNo` 为 MessageID，`RequestId` 为 RequestID；计费条数等进入 Metadata |
| 阿里云 | 官方 `dysmsapi-20170525` SDK | 中国大陆号码去除 `+86` 后进入 `PhoneNumbers`；`SignatureRef` 进入 `SignName` | 按名称编码为 `TemplateParam` JSON | `BizId` 为 MessageID，`RequestId` 为 RequestID |
| UCloud | 直接 HTTP | E.164 转为 UCloud 区号格式；`SignatureRef` 进入 `SigContent` | 按顺序进入 `TemplateParams` | `SessionNo` 为 MessageID |
| 七牛云 | 直接 HTTP | 号码按七牛 API 要求转换；`SignatureRef` 进入 `signature_id` | 按名称构造 `parameters` | `job_id` 为 MessageID，HTTP 请求标识为 RequestID |
| 云片 | 直接 HTTP `tpl_single_send` | E.164 按云片规则进入 `mobile`；SignatureRef 不参与模板发送 | 名称和值进行表单编码后构造 `tpl_value` | 响应 `sid` 为 MessageID，其余稳定标量可进入 Metadata |

阿里云首版的 `SendSms` 映射只支持中国大陆 `+86` Recipient；其他国家或地区返回 `InvalidRequest`。其余 Provider 的国家与地区支持范围以所用发送端点为准，并通过契约测试固定转换行为。

腾讯和新版阿里官方 SDK 提供 Context 发送方法，并允许禁用重试。UCloud 官方 SDK 的发送请求默认启用重试且发送方法不接收 Context；七牛和云片当前官方 SDK 的发送方法也不接收 Context。因此后三家直接调用 HTTP API，以满足可取消且只尝试一次的公共契约。直接 HTTP 实现可参考官方 SDK 的鉴权算法，但不复制其公共类型。

## 错误模型

`failure` 包定义 Send Failure。它只描述请求已经跨过 Provider seam 后的不成功或不确定 Send Attempt；普通 `error` 返回仍是 `Sender.Send` 的完整错误契约。调用方通过 `failure.From(err)` 识别 Failure，而不是通过类别 sentinel、`SendError` 或 Provider 原生错误类型。

```go
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
}

func From(error) (Failure, bool)

type Factory struct { /* unexported fields */ }
func NewFactory(provider string) (Factory, error)
func (Factory) Decision(Category, Diagnostic) Failure
func (Factory) Unknown(Diagnostic, error) Failure
```

稳定类别严格只有 `authentication`、`rate_limited`、`rejected`、`unavailable` 和 `unknown_outcome`。前四项只用于 Provider 给出明确决定时：分别表示凭证或权限失败、限流、业务规则拒绝和明确暂时不可用。`UnknownOutcome` 表示请求调用后没有明确 Provider 决定，包括超时、取消、连接中断、无法解析的响应或适配器无法可靠分类的结果；它没有 Failure 级 `internal` 类别。

公共校验、已完成的 Context、Provider 特有的 preflight 拒绝、请求编码和请求构造错误都发生在 Provider seam 之前，保持普通错误且 `failure.From` 返回 false。Provider 的明确决定优先于同时发生的 Context 取消；调用开始后，任何没有明确决定的结果都必须是 `UnknownOutcome`。

每个 Provider 在构造时以稳定 Provider 名称创建一个 `failure.Factory`，并在本地把原生状态或错误码映射到 `Decision`。`Decision` 的无效类别安全降级为 `UnknownOutcome`，工厂方法不 panic 且不返回第二个错误。Provider 名称、Code 和 RequestID 都必须是有界 ASCII token：无效的可选 Code 或 RequestID 被省略。Failure 的 `Error()` 只包含规范化 Provider 和类别；它不公开 Message、Cause、`Unwrap`、原生错误身份、Provider 原生类型、原始响应或任意事实包。

只有 `UnknownOutcome` 在记录了相应 Context 事实时才匹配 `errors.Is(err, context.Canceled)` 或 `errors.Is(err, context.DeadlineExceeded)`；Failure 不保留其他 native cause identity。`Failure` 是 sealed interface，内置和第三方 Provider 只能经由公开的 Provider-bound `Factory` 构造它。错误模型不提供 `Retryable` 布尔值；是否重试由调用方结合业务幂等性决定。

## 安全与可观测性

库不主动记录日志、发送指标或启动后台任务。应用可以用装饰器包装 `Sender` 实现自己的监控、审计和路由。

Failure 的 `Error()`、Details 和 Metadata 不包含凭证、完整请求体、完整手机号、模板参数、Provider 原生错误消息或 native cause。Details 只保存通过 token 校验的 Provider、Code 和 RequestID；测试失败输出同样使用脱敏值。

## 测试与发布验收

普通 CI 在 Go 1.25 和 1.26 上执行：

- 根包与 failure 单元测试：E.164 解析、零值、请求校验、SignatureRef 优先级、Failure 分类、诊断校验和 Context 匹配
- Provider 契约测试：一次调用只产生一次外部请求、Context 传播、字段映射、成功响应和错误映射
- HTTP Provider 测试：使用 `httptest.Server` 验证方法、路径、鉴权头、编码和响应解析
- SDK Provider 测试：在官方客户端外定义最小内部接口，使用 fake 验证调用，不在公共 API 暴露 SDK 类型
- 并发测试：`go test -race ./...` 验证共享 Provider 实例
- 编译示例：README 和 Example 中的标准调用持续通过编译

真实发送测试使用 `integration` build tag，凭证只从测试进程环境显式读取，默认不运行。首个正式版本发布前，五家各完成至少一次真实模板短信发送，并记录测试日期、Provider 和脱敏请求标识；真实号码、模板参数和凭证不得进入仓库或 CI 日志。

## 非目标

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

## 参考实现与事实来源

- [overtrue/easy-sms](https://github.com/overtrue/easy-sms)：统一消息、Provider（Gateway）与策略分层的参考；本项目首版有意不实现其路由与降级层。
- [TencentCloud/tencentcloud-sdk-go SMS v20210111](https://github.com/TencentCloud/tencentcloud-sdk-go/tree/master/tencentcloud/sms/v20210111)：E.164、`TemplateParamSet`、`SendSmsWithContext`、`SerialNo` 与 `RequestId`。
- [Alibaba Cloud Dysmsapi Go SDK](https://github.com/alibabacloud-go/dysmsapi-20170525)：命名参数 JSON、`SendSmsWithContext`、`BizId` 与 `RequestId`。
- [UCloud Go SDK USMS](https://github.com/ucloud/ucloud-sdk-go/tree/master/services/usms)：位置参数、区号格式、`SigContent`、`SessionNo` 以及默认重试行为。
- [Qiniu Go SDK SMS](https://github.com/qiniu/go-sdk/tree/master/sms)：`signature_id`、命名参数和 `job_id`。
- [Yunpian Go SDK SMS](https://github.com/yunpian/yunpian-go-sdk/blob/master/sdk/sms.go)：`tpl_single_send`、`tpl_id` 与 `tpl_value` 编码。

## 相关决策

- [ADR 0001](../../adr/0001-prefer-official-provider-sdks.md)：优先官方 SDK，必要时单家使用直接 HTTP。
- [ADR 0002](../../adr/0002-use-a-single-go-module.md)：核心与五个内置 Provider 使用单 Go module。
- [ADR 0003](../../adr/0003-keep-standard-http-proxy-discovery.md)：默认客户端保留 Go 标准 HTTP 代理发现。
- [ADR 0004](../../adr/0004-normalize-send-failures.md)：以 sealed failure module 统一 Send Failure。
