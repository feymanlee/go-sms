package aliyun

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ali "github.com/alibabacloud-go/dysmsapi-20170525/v5/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	sms "github.com/feymanlee/go-sms"
)

type fakeClient struct {
	calls        int
	ctx          context.Context
	req          *ali.SendSmsRequest
	runtime      *dara.RuntimeOptions
	response     *ali.SendSmsResponse
	err          error
	beforeReturn func()
}

func (f *fakeClient) SendSmsWithContext(ctx context.Context, req *ali.SendSmsRequest, runtime *dara.RuntimeOptions) (*ali.SendSmsResponse, error) {
	f.calls++
	f.ctx = ctx
	f.req = req
	f.runtime = runtime
	if f.beforeReturn != nil {
		f.beforeReturn()
	}
	return f.response, f.err
}

func testRequest(t *testing.T) sms.Request {
	t.Helper()
	recipient, err := sms.ParseRecipient("+8613812345678")
	if err != nil {
		t.Fatal(err)
	}
	return sms.Request{
		Recipient: recipient,
		Message: sms.TemplateMessage{
			TemplateID: "SMS_1234",
			Params: []sms.TemplateParam{
				{Name: "code", Value: "123456"},
				{Name: "minutes", Value: "5"},
			},
		},
		SignatureRef: "Example",
	}
}

func response(code, message, bizID, requestID string) *ali.SendSmsResponse {
	return &ali.SendSmsResponse{Body: &ali.SendSmsResponseBody{
		Code:      tea.String(code),
		Message:   tea.String(message),
		BizId:     tea.String(bizID),
		RequestId: tea.String(requestID),
	}}
}

func testProvider(fake *fakeClient) *Provider {
	autoretry := false
	maxAttempts := 1
	return &Provider{
		client:           fake,
		runtime:          &dara.RuntimeOptions{Autoretry: &autoretry, MaxAttempts: &maxAttempts},
		defaultSignature: "Default",
	}
}

func TestSendMapsRequestAndReturnsSubmission(t *testing.T) {
	fake := &fakeClient{response: response("OK", "OK", "biz-1", "request-1")}
	provider := testProvider(fake)
	ctx := context.Background()

	submission, err := provider.Send(ctx, testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
	if fake.ctx != ctx {
		t.Fatal("SendSmsWithContext did not receive the caller context")
	}
	if got := tea.StringValue(fake.req.PhoneNumbers); got != "13812345678" {
		t.Fatalf("PhoneNumbers = %q", got)
	}
	if got := tea.StringValue(fake.req.SignName); got != "Example" {
		t.Fatalf("SignName = %q", got)
	}
	if got := tea.StringValue(fake.req.TemplateCode); got != "SMS_1234" {
		t.Fatalf("TemplateCode = %q", got)
	}
	if got := tea.StringValue(fake.req.TemplateParam); got != `{"code":"123456","minutes":"5"}` {
		t.Fatalf("TemplateParam = %q", got)
	}
	if fake.runtime == nil || fake.runtime.Autoretry == nil || *fake.runtime.Autoretry {
		t.Fatalf("Autoretry = %#v, want false", fake.runtime)
	}
	if fake.runtime.MaxAttempts == nil || *fake.runtime.MaxAttempts != 1 {
		t.Fatalf("MaxAttempts = %#v, want 1", fake.runtime.MaxAttempts)
	}
	if submission.Provider != "aliyun" || submission.MessageID != "biz-1" || submission.RequestID != "request-1" {
		t.Fatalf("submission = %#v", submission)
	}
}

func TestSendUsesDefaultSignature(t *testing.T) {
	fake := &fakeClient{response: response("OK", "OK", "biz-1", "request-1")}
	req := testRequest(t)
	req.SignatureRef = ""

	if _, err := testProvider(fake).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || tea.StringValue(fake.req.SignName) != "Default" {
		t.Fatalf("calls = %d, SignName = %q", fake.calls, tea.StringValue(fake.req.SignName))
	}
}

func TestSendRejectsNonChinaRecipientBeforeCallingClient(t *testing.T) {
	fake := &fakeClient{}
	req := testRequest(t)
	recipient, err := sms.ParseRecipient("+12025550123")
	if err != nil {
		t.Fatal(err)
	}
	req.Recipient = recipient

	_, err = testProvider(fake).Send(context.Background(), req)
	if !errors.Is(err, sms.ErrInvalidRequest) || fake.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendMapsBusinessLimitControlToRateLimited(t *testing.T) {
	fake := &fakeClient{response: response("isv.BUSINESS_LIMIT_CONTROL", "frequency limit", "", "request-2")}

	_, err := testProvider(fake).Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrRateLimited) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	var detail *sms.SendError
	if !errors.As(err, &detail) || detail.Code != "isv.BUSINESS_LIMIT_CONTROL" || detail.RequestID != "request-2" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestSendClassifiesBodyCodes(t *testing.T) {
	tests := []struct {
		name string
		code string
		want error
	}{
		{name: "day limit", code: "isv.DAY_LIMIT_CONTROL", want: sms.ErrRateLimited},
		{name: "access key", code: "InvalidAccessKeyId.NotFound", want: sms.ErrAuthentication},
		{name: "signature", code: "SignatureDoesNotMatch", want: sms.ErrAuthentication},
		{name: "service unavailable", code: "ServiceUnavailable", want: sms.ErrUnavailable},
		{name: "system error", code: "isp.SYSTEM_ERROR", want: sms.ErrUnavailable},
		{name: "other business rejection", code: "isv.TEMPLATE_ILLEGAL", want: sms.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{response: response(tt.code, "failed", "", "request-body")}
			_, err := testProvider(fake).Send(context.Background(), testRequest(t))
			if !errors.Is(err, tt.want) || fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
		})
	}
}

func TestSendClassifiesSDKError(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		status int
		want   error
	}{
		{name: "unauthorized", code: "Unauthorized", status: http.StatusUnauthorized, want: sms.ErrAuthentication},
		{name: "forbidden", code: "Forbidden", status: http.StatusForbidden, want: sms.ErrAuthentication},
		{name: "access key", code: "InvalidAccessKeyId.NotFound", status: http.StatusBadRequest, want: sms.ErrAuthentication},
		{name: "rate limited", code: "Throttling.User", status: http.StatusTooManyRequests, want: sms.ErrRateLimited},
		{name: "server error", code: "ServerError", status: http.StatusBadGateway, want: sms.ErrUnavailable},
		{name: "service unavailable", code: "ServiceUnavailable", status: http.StatusBadRequest, want: sms.ErrUnavailable},
		{name: "unknown SDK error", code: "ClientError", status: http.StatusBadRequest, want: sms.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			native := tea.NewSDKError(map[string]interface{}{
				"code":       tt.code,
				"statusCode": tt.status,
				"message":    "failed for +8613812345678 and 13812345678",
				"data":       map[string]interface{}{"RequestId": "request-sdk"},
			})
			fake := &fakeClient{err: native}
			_, err := testProvider(fake).Send(context.Background(), testRequest(t))
			if !errors.Is(err, tt.want) || !errors.Is(err, native) || fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
			var sdkError *tea.SDKError
			if errors.As(err, &sdkError) {
				t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
			}
			var detail *sms.SendError
			if !errors.As(err, &detail) || detail.Code != tt.code || detail.Message != "aliyun SDK request failed" || detail.RequestID != "request-sdk" {
				t.Fatalf("detail = %#v", detail)
			}
		})
	}
}

func TestSendClassifiesDaraSDKError(t *testing.T) {
	native := dara.NewSDKError(map[string]interface{}{
		"code":       "SignatureDoesNotMatch",
		"statusCode": http.StatusForbidden,
		"message":    "signature mismatch",
	})
	fake := &fakeClient{err: native}

	_, err := testProvider(fake).Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrAuthentication) || !errors.Is(err, native) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	var sdkError *dara.SDKError
	if errors.As(err, &sdkError) {
		t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
	}
}

func TestSendPreservesContextCancellationWithoutSDKErrorCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	native := tea.NewSDKError(map[string]interface{}{
		"code":       "ClientError",
		"statusCode": http.StatusBadRequest,
		"message":    "request failed",
		"data":       map[string]interface{}{"RequestId": "request-sdk"},
	})
	fake := &fakeClient{err: native, beforeReturn: cancel}

	_, err := testProvider(fake).Send(ctx, testRequest(t))
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, context.Canceled) || !errors.Is(err, native) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	var sdkError *tea.SDKError
	if errors.As(err, &sdkError) {
		t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
	}
}

func TestSendClassifiesTransportErrorsAsUnknownOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "network", err: &net.DNSError{Err: "timeout", IsTimeout: true}},
		{name: "URL", err: &url.Error{Op: "Post", URL: "https://dysmsapi.aliyuncs.com", Err: errors.New("connection reset")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{err: tt.err}
			_, err := testProvider(fake).Send(context.Background(), testRequest(t))
			if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, tt.err) || fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
		})
	}
}

func TestSendPreservesContextCancellationWhenSDKReturnsOpaqueError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	native := errors.New("SDK request aborted")
	fake := &fakeClient{err: native}

	_, err := testProvider(fake).Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || errors.Is(err, sms.ErrUnknownOutcome) || fake.calls != 0 {
		t.Fatalf("preflight error = %v, calls = %d", err, fake.calls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	fake = &fakeClient{err: native, beforeReturn: cancel}
	_, err = testProvider(fake).Send(ctx, testRequest(t))
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, context.Canceled) || !errors.Is(err, native) || fake.calls != 1 {
		t.Fatalf("post-invocation error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendDoesNotExposeUntrustedProviderMessages(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	fake := &fakeClient{response: response("isv.TEMPLATE_ILLEGAL", remoteMessage, "", "request-adversarial")}

	_, err := testProvider(fake).Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrRejected) || !errors.As(err, &detail) {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
	if detail.Code != "isv.TEMPLATE_ILLEGAL" || detail.RequestID != "request-adversarial" || detail.Message != "aliyun provider request failed" {
		t.Fatalf("detail = %#v", detail)
	}
	assertNoSensitiveText(t, secrets, err.Error(), detail.Message)
}

func TestSendKeepsSDKCauseOpaqueAndMatchable(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	native := tea.NewSDKError(map[string]interface{}{
		"code":       "InvalidAccessKeyId.NotFound",
		"statusCode": http.StatusForbidden,
		"message":    remoteMessage,
		"data":       map[string]interface{}{"RequestId": "request-native"},
	})

	_, err := testProvider(&fakeClient{err: native}).Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrAuthentication) || !errors.Is(err, native) || !errors.As(err, &detail) {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
	if detail.Code != "InvalidAccessKeyId.NotFound" || detail.RequestID != "request-native" || detail.Message != "aliyun SDK request failed" {
		t.Fatalf("detail = %#v", detail)
	}
	var recovered *tea.SDKError
	if errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	if detail.Cause == nil || errors.Unwrap(detail.Cause) != nil {
		t.Fatalf("cause = %#v", detail.Cause)
	}
	assertNoSensitiveText(t, secrets, err.Error(), detail.Message, detail.Cause.Error(), errors.Unwrap(err).Error())
}

func TestSendKeepsUnmatchedSDKCauseOpaqueAndMatchable(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	native := &sensitiveSDKError{message: remoteMessage}

	_, err := testProvider(&fakeClient{err: native}).Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrInternal) || !errors.Is(err, native) || !errors.As(err, &detail) {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
	if detail.Message != "aliyun SDK request failed" {
		t.Fatalf("detail = %#v", detail)
	}
	var recovered *sensitiveSDKError
	if errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	if detail.Cause == nil || errors.Unwrap(detail.Cause) != nil {
		t.Fatalf("cause = %#v", detail.Cause)
	}
	assertNoSensitiveText(t, secrets, err.Error(), detail.Message, detail.Cause.Error(), errors.Unwrap(err).Error())
}

func TestSendRejectsMalformedResponse(t *testing.T) {
	tests := []struct {
		name      string
		response  *ali.SendSmsResponse
		requestID string
	}{
		{name: "nil response"},
		{name: "nil body", response: &ali.SendSmsResponse{}},
		{name: "nil code", response: &ali.SendSmsResponse{Body: &ali.SendSmsResponseBody{RequestId: tea.String("request-3")}}, requestID: "request-3"},
		{name: "empty code", response: response("", "", "", "request-4"), requestID: "request-4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{response: tt.response}
			_, err := testProvider(fake).Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || detail.RequestID != tt.requestID || fake.calls != 1 {
				t.Fatalf("error = %v, detail = %#v, calls = %d", err, detail, fake.calls)
			}
		})
	}
}

type captureClient struct {
	mu       sync.Mutex
	requests []*ali.SendSmsRequest
}

func (c *captureClient) SendSmsWithContext(_ context.Context, req *ali.SendSmsRequest, _ *dara.RuntimeOptions) (*ali.SendSmsResponse, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	return response("OK", "OK", "biz", "request"), nil
}

func TestConcurrentSendsBuildFreshRequests(t *testing.T) {
	client := &captureClient{}
	autoretry := false
	maxAttempts := 1
	provider := &Provider{
		client:           client,
		runtime:          &dara.RuntimeOptions{Autoretry: &autoretry, MaxAttempts: &maxAttempts},
		defaultSignature: "Default",
	}
	requests := []sms.Request{testRequest(t), testRequest(t)}
	requests[0].Message.Params[0].Value = "first"
	requests[1].Message.Params[0].Value = "second"

	errorsCh := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := provider.Send(context.Background(), request)
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(client.requests) != 2 || client.requests[0] == client.requests[1] {
		t.Fatalf("requests = %#v", client.requests)
	}
	got := map[string]bool{
		tea.StringValue(client.requests[0].TemplateParam): true,
		tea.StringValue(client.requests[1].TemplateParam): true,
	}
	if !got[`{"code":"first","minutes":"5"}`] || !got[`{"code":"second","minutes":"5"}`] {
		t.Fatalf("TemplateParam values = %#v", got)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	valid := Config{AccessKeyID: "id", AccessKeySecret: "secret", Region: "cn-hangzhou"}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "access key ID", mutate: func(c *Config) { c.AccessKeyID = " " }},
		{name: "access key secret", mutate: func(c *Config) { c.AccessKeySecret = "" }},
		{name: "region", mutate: func(c *Config) { c.Region = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			if _, err := New(config); err == nil {
				t.Fatal("New returned nil error")
			}
		})
	}
}

func TestNewDisablesSDKRetries(t *testing.T) {
	provider, err := New(Config{AccessKeyID: "id", AccessKeySecret: "secret", Region: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.runtime == nil || provider.runtime.Autoretry == nil || *provider.runtime.Autoretry {
		t.Fatalf("runtime = %#v", provider.runtime)
	}
	if provider.runtime.MaxAttempts == nil || *provider.runtime.MaxAttempts != 1 {
		t.Fatalf("MaxAttempts = %#v", provider.runtime.MaxAttempts)
	}
}

func TestNewDoesNotReadAlibabaUserAgentEnvironment(t *testing.T) {
	t.Setenv("ALIBABA_CLOUD_USER_AGENT", "environment-user-agent")

	provider, err := New(Config{AccessKeyID: "id", AccessKeySecret: "secret", Region: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	client, ok := provider.client.(*ali.Client)
	if !ok {
		t.Fatalf("client = %T, want *client.Client", provider.client)
	}
	if got := tea.StringValue(client.UserAgent); got != "go-sms" {
		t.Fatalf("UserAgent = %q, want %q", got, "go-sms")
	}
}

func TestDaraHTTPClientDelegatesToInjectedClient(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://example.com/sms" {
			t.Fatalf("URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(strings.NewReader("accepted")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	request, err := http.NewRequest(http.MethodPost, "https://example.com/sms", nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := (daraHTTPClient{client: &http.Client{Transport: transport}}).Call(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", got.StatusCode)
	}
}

func TestSDKDoesNotFollowRedirectsOrMutateCallerClient(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		_, _ = io.WriteString(w, `{"Code":"OK","Message":"OK","BizId":"biz-redirect","RequestId":"request-redirect"}`)
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	callerClient := source.Client()
	callerClient.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	provider, err := New(
		Config{AccessKeyID: "id", AccessKeySecret: "secret", Region: "cn-hangzhou"},
		WithHTTPClient(callerClient),
		WithEndpoint(strings.TrimPrefix(source.URL, "https://")),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = provider.Send(context.Background(), testRequest(t))
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
	if callerClient.CheckRedirect == nil {
		t.Fatal("caller client CheckRedirect was mutated")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type sensitiveSDKError struct {
	message string
}

func (e *sensitiveSDKError) Error() string { return e.message }

func adversarialRemoteMessage(req sms.Request, credential string) (string, []string) {
	secrets := []string{
		"+12025550123",
		url.QueryEscape(req.Recipient.String()),
		credential,
		req.Message.Params[0].Value,
		`{"PhoneNumbers":"+12025550123","TemplateParam":{"code":"123456"}}`,
	}
	return strings.Join(secrets, " | "), secrets
}

func assertNoSensitiveText(t *testing.T, secrets []string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("public error text leaked %q: %q", secret, text)
			}
		}
	}
}
