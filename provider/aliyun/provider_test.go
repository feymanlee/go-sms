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
	"github.com/feymanlee/go-sms/failure"
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

func testProvider(t *testing.T, client apiClient) *Provider {
	t.Helper()
	failures, err := failure.NewFactory("aliyun")
	if err != nil {
		t.Fatal(err)
	}
	autoretry := false
	maxAttempts := 1
	return &Provider{
		client:           client,
		runtime:          &dara.RuntimeOptions{Autoretry: &autoretry, MaxAttempts: &maxAttempts},
		defaultSignature: "Default",
		failures:         failures,
	}
}

func TestSendMapsRequestAndReturnsSubmission(t *testing.T) {
	fake := &fakeClient{response: response("OK", "OK", "biz-1", "request-1")}
	provider := testProvider(t, fake)
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

	if _, err := testProvider(t, fake).Send(context.Background(), req); err != nil {
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

	_, err = testProvider(t, fake).Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if _, ok := failure.From(err); ok || fake.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendMapsBusinessLimitControlToRateLimited(t *testing.T) {
	fake := &fakeClient{response: response("isv.BUSINESS_LIMIT_CONTROL", "frequency limit", "", "request-2")}

	_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.RateLimited)
	if fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	if details := got.Details(); details.Code != "isv.BUSINESS_LIMIT_CONTROL" || details.RequestID != "request-2" {
		t.Fatalf("details = %#v", details)
	}
}

func TestSendReturnsUnknownOutcomeWhenOKResponseLacksBizID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeClient{
		response:     response("OK", "OK", "", "request-missing-biz"),
		beforeReturn: cancel,
	}

	_, err := testProvider(t, fake).Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	if details := got.Details(); details.Code != "OK" || details.RequestID != "request-missing-biz" {
		t.Fatalf("details = %#v", details)
	}
	if !errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendClassifiesBodyCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		category failure.Category
	}{
		{name: "day limit", code: "isv.DAY_LIMIT_CONTROL", category: failure.RateLimited},
		{name: "access key", code: "InvalidAccessKeyId.NotFound", category: failure.Authentication},
		{name: "signature", code: "SignatureDoesNotMatch", category: failure.Authentication},
		{name: "service unavailable", code: "ServiceUnavailable", category: failure.Unavailable},
		{name: "system error", code: "isp.SYSTEM_ERROR", category: failure.Unavailable},
		{name: "other business rejection", code: "isv.TEMPLATE_ILLEGAL", category: failure.Rejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{response: response(tt.code, "failed", "", "request-body")}
			_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, tt.category)
			if fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
			if details := got.Details(); details.Code != tt.code || details.RequestID != "request-body" {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendClassifiesSDKError(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		status   int
		category failure.Category
	}{
		{name: "HTTP 429 wins over authentication code", code: "InvalidAccessKeyId.NotFound", status: http.StatusTooManyRequests, category: failure.RateLimited},
		{name: "HTTP 401 wins over throttling code", code: "isv.DAY_LIMIT_CONTROL", status: http.StatusUnauthorized, category: failure.Authentication},
		{name: "HTTP 403 wins over unavailable code", code: "ServiceUnavailable", status: http.StatusForbidden, category: failure.Authentication},
		{name: "HTTP 5xx wins over throttling code", code: "isv.BUSINESS_LIMIT_CONTROL", status: http.StatusBadGateway, category: failure.Unavailable},
		{name: "native authentication", code: "SignatureDoesNotMatch", status: http.StatusOK, category: failure.Authentication},
		{name: "native throttling", code: "isv.DAY_LIMIT_CONTROL", status: http.StatusOK, category: failure.RateLimited},
		{name: "native unavailable", code: "InternalError.ServerBusy", status: http.StatusOK, category: failure.Unavailable},
		{name: "other HTTP 4xx", code: "ClientError", status: http.StatusBadRequest, category: failure.Rejected},
		{name: "no decision without status or known code", code: "ClientError", status: 0, category: failure.UnknownOutcome},
		{name: "no decision for successful status and unknown code", code: "ClientError", status: http.StatusOK, category: failure.UnknownOutcome},
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
			_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, tt.category)
			if fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
			var sdkError *tea.SDKError
			if errors.Is(err, native) || errors.As(err, &sdkError) {
				t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
			}
			if details := got.Details(); details.Code != tt.code || details.RequestID != "request-sdk" {
				t.Fatalf("details = %#v", details)
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

	_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Authentication)
	if fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	var sdkError *dara.SDKError
	if errors.Is(err, native) || errors.As(err, &sdkError) {
		t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
	}
	if details := got.Details(); details.Code != "SignatureDoesNotMatch" {
		t.Fatalf("details = %#v", details)
	}
}

func TestSendReturnsUnknownOutcomeForUndecidableSDKErrorWithContextEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	native := tea.NewSDKError(map[string]interface{}{
		"code":       "ClientError",
		"statusCode": http.StatusOK,
		"message":    "request failed",
		"data":       map[string]interface{}{"RequestId": "request-sdk"},
	})
	fake := &fakeClient{err: native, beforeReturn: cancel}

	_, err := testProvider(t, fake).Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	if !errors.Is(err, context.Canceled) || errors.Is(err, native) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	var sdkError *tea.SDKError
	if errors.As(err, &sdkError) {
		t.Fatalf("SDK error leaked through public error chain: %#v", sdkError)
	}
	if details := got.Details(); details.Code != "ClientError" || details.RequestID != "request-sdk" {
		t.Fatalf("details = %#v", details)
	}
}

func TestSendDefinitiveSDKDecisionWinsOverCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	native := tea.NewSDKError(map[string]interface{}{
		"code":       "InvalidAccessKeyId.NotFound",
		"statusCode": http.StatusTooManyRequests,
		"message":    "request failed",
		"data":       map[string]interface{}{"RequestId": "request-rate"},
	})
	fake := &fakeClient{err: native, beforeReturn: cancel}

	_, err := testProvider(t, fake).Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.RateLimited)
	if errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	if details := got.Details(); details.Code != "InvalidAccessKeyId.NotFound" || details.RequestID != "request-rate" {
		t.Fatalf("details = %#v", details)
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
		{name: "ordinary", err: errors.New("SDK request failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{err: tt.err}
			_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, failure.UnknownOutcome)
			isContext := tt.err == context.Canceled || tt.err == context.DeadlineExceeded
			if isContext != errors.Is(err, tt.err) || fake.calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, fake.calls)
			}
			if details := got.Details(); details.Code != "" || details.RequestID != "" {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendPreservesContextCancellationWhenSDKReturnsOpaqueError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	native := errors.New("SDK request aborted")
	fake := &fakeClient{err: native}

	_, err := testProvider(t, fake).Send(ctx, testRequest(t))
	_, isFailure := failure.From(err)
	if !errors.Is(err, context.Canceled) || isFailure || fake.calls != 0 {
		t.Fatalf("preflight error = %v, calls = %d", err, fake.calls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	fake = &fakeClient{err: native, beforeReturn: cancel}
	_, err = testProvider(t, fake).Send(ctx, testRequest(t))
	requireFailure(t, err, failure.UnknownOutcome)
	if !errors.Is(err, context.Canceled) || errors.Is(err, native) || fake.calls != 1 {
		t.Fatalf("post-invocation error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendDoesNotExposeUntrustedProviderMessages(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	fake := &fakeClient{response: response("isv.TEMPLATE_ILLEGAL", remoteMessage, "", "request-adversarial")}

	_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Rejected)
	if details := got.Details(); details.Code != "isv.TEMPLATE_ILLEGAL" || details.RequestID != "request-adversarial" {
		t.Fatalf("details = %#v", details)
	}
	assertNoSensitiveText(t, secrets, err.Error())
}

func TestSendDoesNotExposeNativeSDKError(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	native := tea.NewSDKError(map[string]interface{}{
		"code":       "InvalidAccessKeyId.NotFound",
		"statusCode": http.StatusForbidden,
		"message":    remoteMessage,
		"data":       map[string]interface{}{"RequestId": "request-native"},
	})

	_, err := testProvider(t, &fakeClient{err: native}).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Authentication)
	if details := got.Details(); details.Code != "InvalidAccessKeyId.NotFound" || details.RequestID != "request-native" {
		t.Fatalf("details = %#v", details)
	}
	var recovered *tea.SDKError
	if errors.Is(err, native) || errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	assertNoSensitiveText(t, secrets, err.Error())
}

func TestSendDoesNotExposeUnmatchedSDKError(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "access-secret")
	native := &sensitiveSDKError{message: remoteMessage}

	_, err := testProvider(t, &fakeClient{err: native}).Send(context.Background(), testRequest(t))
	requireFailure(t, err, failure.UnknownOutcome)
	var recovered *sensitiveSDKError
	if errors.Is(err, native) || errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	assertNoSensitiveText(t, secrets, err.Error())
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
			_, err := testProvider(t, fake).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, failure.UnknownOutcome)
			if details := got.Details(); details.RequestID != tt.requestID || fake.calls != 1 {
				t.Fatalf("error = %v, details = %#v, calls = %d", err, details, fake.calls)
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
	provider := testProvider(t, client)
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
			} else if _, ok := failure.From(err); ok {
				t.Fatalf("constructor validation returned Failure: %v", err)
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

func requireFailure(t *testing.T, err error, category failure.Category) failure.Failure {
	t.Helper()
	got, ok := failure.From(err)
	if !ok {
		t.Fatalf("error is not a Failure: %v", err)
	}
	if got.Category() != category {
		t.Fatalf("category=%q want=%q", got.Category(), category)
	}
	return got
}

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
