package tencent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

type fakeClient struct {
	calls    int
	req      *tc.SendSmsRequest
	response *tc.SendSmsResponse
	err      error
}

func (f *fakeClient) SendSmsWithContext(_ context.Context, req *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
	f.calls++
	f.req = req
	return f.response, f.err
}

type apiClientFunc func(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error)

func (f apiClientFunc) SendSmsWithContext(ctx context.Context, req *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
	return f(ctx, req)
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
			TemplateID: "1234",
			Params: []sms.TemplateParam{
				{Name: "code", Value: "123456"},
				{Name: "minutes", Value: "5"},
			},
		},
		SignatureRef: "Example",
	}
}

func response(code, message, serialNo string, fee uint64, requestID string) *tc.SendSmsResponse {
	return &tc.SendSmsResponse{Response: &tc.SendSmsResponseParams{
		RequestId: tccommon.StringPtr(requestID),
		SendStatusSet: []*tc.SendStatus{
			{
				Code:     tccommon.StringPtr(code),
				Message:  tccommon.StringPtr(message),
				SerialNo: tccommon.StringPtr(serialNo),
				Fee:      tccommon.Uint64Ptr(fee),
			},
		},
	}}
}

func TestSendMapsRequestAndReturnsSubmission(t *testing.T) {
	fake := &fakeClient{response: response("Ok", "send success", "serial-1", 1, "request-1")}
	provider := newTestProvider(t, fake)
	provider.defaultSignature = "Default"

	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
	if got := strings.Join(tccommon.StringValues(fake.req.PhoneNumberSet), ","); got != "+8613812345678" {
		t.Fatal("PhoneNumberSet did not preserve the E.164 recipient")
	}
	if got := strings.Join(tccommon.StringValues(fake.req.TemplateParamSet), ","); got != "123456,5" {
		t.Fatalf("TemplateParamSet = %q", got)
	}
	if got := stringValue(fake.req.SmsSdkAppId); got != "1400000000" {
		t.Fatalf("SmsSdkAppId = %q", got)
	}
	if got := stringValue(fake.req.TemplateId); got != "1234" {
		t.Fatalf("TemplateId = %q", got)
	}
	if got := stringValue(fake.req.SignName); got != "Example" {
		t.Fatalf("SignName = %q", got)
	}
	if submission.Provider != "tencent" || submission.MessageID != "serial-1" || submission.RequestID != "request-1" {
		t.Fatalf("submission = %#v", submission)
	}
	if got := submission.Metadata["fee"]; got != "1" {
		t.Fatalf("fee = %q", got)
	}
}

func TestSendUsesDefaultSignature(t *testing.T) {
	fake := &fakeClient{response: response("Ok", "send success", "serial-1", 1, "request-1")}
	provider := newTestProvider(t, fake)
	provider.defaultSignature = "Default"
	req := testRequest(t)
	req.SignatureRef = ""

	if _, err := provider.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := stringValue(fake.req.SignName); got != "Default" {
		t.Fatalf("SignName = %q", got)
	}
}

func TestSendRejectsNonOKStatus(t *testing.T) {
	fake := &fakeClient{response: response("FailedOperation.TemplateIncorrectOrUnapproved", "bad template", "", 0, "request-2")}
	provider := newTestProvider(t, fake)

	_, err := provider.Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Rejected)
	if details := got.Details(); details.Code != "FailedOperation.TemplateIncorrectOrUnapproved" || details.RequestID != "request-2" {
		t.Fatalf("details = %#v", details)
	}
}

func TestSendReturnsUnknownOutcomeWhenOkStatusLacksSerialNo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	provider := newTestProvider(t, apiClientFunc(func(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
		calls.Add(1)
		cancel()
		return response("Ok", "send success", "", 1, "request-missing-serial"), nil
	}))

	_, err := provider.Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	if details := got.Details(); details.Code != "Ok" || details.RequestID != "request-missing-serial" {
		t.Fatalf("details = %#v", details)
	}
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendReturnsUnknownOutcomeWhenOkStatusLacksSerialNoWithoutContextError(t *testing.T) {
	fake := &fakeClient{response: response("Ok", "send success", "", 1, "request-missing-serial-background")}

	_, err := newTestProvider(t, fake).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	if details := got.Details(); details.Code != "Ok" || details.RequestID != "request-missing-serial-background" {
		t.Fatalf("details = %#v", details)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || fake.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendReturnsAcceptedSubmissionWhenContextIsCanceledDuringInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	provider := newTestProvider(t, apiClientFunc(func(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
		calls.Add(1)
		cancel()
		return response("Ok", "send success", "serial-accepted", 1, "request-accepted"), nil
	}))

	submission, err := provider.Send(ctx, testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled || calls.Load() != 1 {
		t.Fatalf("Context error = %v, calls = %d", ctx.Err(), calls.Load())
	}
	if submission.Provider != "tencent" || submission.MessageID != "serial-accepted" || submission.RequestID != "request-accepted" {
		t.Fatalf("submission = %#v", submission)
	}
}

func TestSendClassifiesKnownStatusCode(t *testing.T) {
	tests := []struct {
		code     string
		category failure.Category
	}{
		{code: "AuthFailure.SecretIdNotFound", category: failure.Authentication},
		{code: "RequestLimitExceeded", category: failure.RateLimited},
		{code: "InternalError", category: failure.Unavailable},
		{code: "FailedOperation.TemplateIncorrectOrUnapproved", category: failure.Rejected},
		{code: "UnsupportedOperation", category: failure.Rejected},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			provider := newTestProvider(t, &fakeClient{response: response(tt.code, "untrusted", "", 0, "request-2")})

			_, err := provider.Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, tt.category)
			if details := got.Details(); details.Code != tt.code || details.RequestID != "request-2" {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendClassifiesTransportErrorsAsUnknownOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "URL", err: &url.Error{Op: "Post", URL: "https://sms.tencentcloudapi.com", Err: errors.New("connection reset")}},
		{name: "ordinary", err: errors.New("SDK failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{err: tt.err}
			provider := newTestProvider(t, fake)
			_, err := provider.Send(context.Background(), testRequest(t))
			requireFailure(t, err, failure.UnknownOutcome)
			isContext := tt.err == context.Canceled || tt.err == context.DeadlineExceeded
			if isContext != errors.Is(err, tt.err) {
				t.Fatalf("context match for %v = %t", tt.err, errors.Is(err, tt.err))
			}
		})
	}
}

func TestSDKTransportCancellationIsUnknownOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	provider, err := New(
		Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou"},
		WithHTTPClient(client),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	var native *tcerr.TencentCloudSDKError
	if details := got.Details(); details.Provider != "tencent" || details.Code != "ClientError.NetworkError" {
		t.Fatalf("details = %#v", details)
	}
	if !errors.Is(err, context.Canceled) || errors.As(err, &native) {
		t.Fatalf("raw SDK error leaked through error chain: %#v", native)
	}
}

func TestSDKNetworkErrorIsUnknownOutcome(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})}
	provider, err := New(
		Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou"},
		WithHTTPClient(client),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	var native *tcerr.TencentCloudSDKError
	if details := got.Details(); details.Provider != "tencent" || details.Code != "ClientError.NetworkError" {
		t.Fatalf("details = %#v", details)
	}
	if errors.As(err, &native) {
		t.Fatalf("native error leaked: %#v", native)
	}
}

func TestSDKDoesNotFollowRedirectsOrMutateCallerClient(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		_, _ = io.WriteString(w, `{"Response":{"SendStatusSet":[{"Code":"Ok","Message":"ok","SerialNo":"serial-redirect","Fee":1}],"RequestId":"request-redirect"}}`)
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	callerClient := source.Client()
	originalRedirect := func(*http.Request, []*http.Request) error { return nil }
	callerClient.CheckRedirect = originalRedirect
	provider, err := New(
		Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou"},
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

func TestSendPreservesSDKNetworkErrorDetails(t *testing.T) {
	native := tcerr.NewTencentCloudSDKError(
		"ClientError.NetworkError",
		"failed for +8613812345678, +12025550123, Authorization: Bearer secret-token, {\"message\":\"private response body\"}",
		"request-network",
	)
	provider := newTestProvider(t, &fakeClient{err: native})

	_, err := provider.Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.UnknownOutcome)
	if details := got.Details(); details.Code != "ClientError.NetworkError" || details.RequestID != "request-network" {
		t.Fatalf("details = %#v", details)
	}
	for _, secret := range []string{"+8613812345678", "+12025550123", "Authorization: Bearer secret-token", `{\"message\":\"private response body\"}`} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("public error text leaked %q: error=%q", secret, err)
		}
	}
	var recovered *tcerr.TencentCloudSDKError
	if errors.Is(err, native) || errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through error chain: %#v", recovered)
	}
}

func TestSendDoesNotExposeUntrustedProviderMessages(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "secret-key")
	provider := newTestProvider(t, &fakeClient{response: response(
		"FailedOperation.TemplateIncorrectOrUnapproved",
		remoteMessage,
		"",
		0,
		"request-adversarial",
	)})

	_, err := provider.Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Rejected)
	if details := got.Details(); details.Code != "FailedOperation.TemplateIncorrectOrUnapproved" || details.RequestID != "request-adversarial" {
		t.Fatalf("details = %#v", details)
	}
	assertNoSensitiveText(t, secrets, err.Error())
}

func TestSendDoesNotExposeNativeSDKError(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "secret-key")
	native := tcerr.NewTencentCloudSDKError("AuthFailure.SecretIdNotFound", remoteMessage, "request-native")
	provider := newTestProvider(t, &fakeClient{err: native})

	_, err := provider.Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Authentication)
	if details := got.Details(); details.Code != "AuthFailure.SecretIdNotFound" || details.RequestID != "request-native" {
		t.Fatalf("details = %#v", details)
	}
	var recovered *tcerr.TencentCloudSDKError
	if errors.Is(err, native) || errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	assertNoSensitiveText(t, secrets, err.Error())
}

func TestSendDoesNotExposeUnmatchedSDKError(t *testing.T) {
	remoteMessage, secrets := adversarialRemoteMessage(testRequest(t), "secret-key")
	native := &sensitiveSDKError{message: remoteMessage}
	provider := newTestProvider(t, &fakeClient{err: native})

	_, err := provider.Send(context.Background(), testRequest(t))
	requireFailure(t, err, failure.UnknownOutcome)
	var recovered *sensitiveSDKError
	if errors.Is(err, native) || errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through errors.As: %#v", recovered)
	}
	assertNoSensitiveText(t, secrets, err.Error())
}

func TestSendClassifiesNativeErrors(t *testing.T) {
	tests := []struct {
		code     string
		category failure.Category
	}{
		{code: "AuthFailure.SecretIdNotFound", category: failure.Authentication},
		{code: "InvalidCredential", category: failure.Authentication},
		{code: "InvalidCredential.Expired", category: failure.Authentication},
		{code: "UnauthorizedOperation.RequestPermissionDeny", category: failure.Authentication},
		{code: "RequestLimitExceeded", category: failure.RateLimited},
		{code: "RequestLimitExceeded.Global", category: failure.RateLimited},
		{code: "LimitExceeded.PhoneNumberDailyLimit", category: failure.RateLimited},
		{code: "InternalError", category: failure.Unavailable},
		{code: "InternalErrorUnexpected", category: failure.Unavailable},
		{code: "ResourceUnavailable.ServiceBusy", category: failure.Unavailable},
		{code: "ClientError.NetworkError", category: failure.UnknownOutcome},
		{code: "UnsupportedOperation", category: failure.UnknownOutcome},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			native := tcerr.NewTencentCloudSDKError(tt.code, "failed for +8613812345678", "request-3")
			fake := &fakeClient{err: native}
			provider := newTestProvider(t, fake)

			_, err := provider.Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, tt.category)
			if details := got.Details(); details.Code != tt.code || details.RequestID != "request-3" {
				t.Fatalf("details = %#v", details)
			}
			var recovered *tcerr.TencentCloudSDKError
			if errors.Is(err, native) || errors.As(err, &recovered) {
				t.Fatalf("native error leaked: %#v", recovered)
			}
		})
	}
}

func TestKnownSDKDecisionWinsOverCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	native := tcerr.NewTencentCloudSDKError("AuthFailure.SecretIdNotFound", "untrusted", "request-canceled")
	provider := newTestProvider(t, apiClientFunc(func(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error) {
		cancel()
		return nil, native
	}))

	_, err := provider.Send(ctx, testRequest(t))
	got := requireFailure(t, err, failure.Authentication)
	if details := got.Details(); details.Code != "AuthFailure.SecretIdNotFound" || details.RequestID != "request-canceled" {
		t.Fatalf("details = %#v", details)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("decision matched canceled Context: %v", err)
	}
}

func TestSendSanitizesRejectedStatus(t *testing.T) {
	fake := &fakeClient{response: response("FailedOperation", "failed for +8613812345678", "", 0, "request-4")}
	provider := newTestProvider(t, fake)

	_, err := provider.Send(context.Background(), testRequest(t))
	requireFailure(t, err, failure.Rejected)
	assertNoSensitiveText(t, []string{"13812345678"}, err.Error())
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
		`{"PhoneNumberSet":["+12025550123"],"TemplateParamSet":["123456"]}`,
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

func TestSendReturnsUnknownOutcomeForMalformedResponses(t *testing.T) {
	tests := []struct {
		name      string
		response  *tc.SendSmsResponse
		requestID string
	}{
		{name: "nil response"},
		{name: "nil body", response: &tc.SendSmsResponse{}},
		{name: "no statuses", response: &tc.SendSmsResponse{Response: &tc.SendSmsResponseParams{}}},
		{name: "multiple statuses", response: &tc.SendSmsResponse{Response: &tc.SendSmsResponseParams{SendStatusSet: []*tc.SendStatus{{}, {}}}}},
		{name: "nil status", response: &tc.SendSmsResponse{Response: &tc.SendSmsResponseParams{SendStatusSet: []*tc.SendStatus{nil}}}},
		{name: "missing status code", response: &tc.SendSmsResponse{Response: &tc.SendSmsResponseParams{SendStatusSet: []*tc.SendStatus{{}}}}},
		{name: "empty status code", response: response("", "", "", 0, "request-6"), requestID: "request-6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newTestProvider(t, &fakeClient{response: tt.response})
			_, err := provider.Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, failure.UnknownOutcome)
			if details := got.Details(); details.RequestID != tt.requestID {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendOmitsFeeMetadataWhenFeeIsMissing(t *testing.T) {
	accepted := response("Ok", "send success", "serial-1", 0, "request-1")
	accepted.Response.SendStatusSet[0].Fee = nil
	provider := newTestProvider(t, &fakeClient{response: accepted})

	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := submission.Metadata["fee"]; exists {
		t.Fatalf("fee metadata = %q", submission.Metadata["fee"])
	}
}

func TestSendValidatesBeforeCallingClient(t *testing.T) {
	fake := &fakeClient{}
	provider := newTestProvider(t, fake)
	req := testRequest(t)
	req.SignatureRef = ""

	_, err := provider.Send(context.Background(), req)
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if _, ok := failure.From(err); ok || fake.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
}

func TestSendRejectsCanceledContextWithoutCallingClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeClient{}

	_, err := newTestProvider(t, fake).Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || fake.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
	}
	if _, ok := failure.From(err); ok {
		t.Fatalf("done Context returned Failure: %v", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	valid := Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou"}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "secret ID", mutate: func(c *Config) { c.SecretID = " " }},
		{name: "secret key", mutate: func(c *Config) { c.SecretKey = "" }},
		{name: "app ID", mutate: func(c *Config) { c.SMSAppID = "" }},
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

func TestNewRejectsTencentGlobalHTTPClientWithoutMutation(t *testing.T) {
	original := tccommon.DefaultHttpClient
	globalClient := &http.Client{Timeout: 37 * time.Second, Transport: http.DefaultTransport}
	tccommon.DefaultHttpClient = globalClient
	t.Cleanup(func() { tccommon.DefaultHttpClient = original })

	_, err := New(
		Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou"},
		WithHTTPClient(&http.Client{Timeout: 2 * time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unused")
		})}),
	)
	if err == nil || !strings.Contains(err.Error(), "DefaultHttpClient") || !strings.Contains(err.Error(), "WithHTTPClient") {
		t.Errorf("error = %v", err)
	}
	if _, ok := failure.From(err); ok {
		t.Fatalf("constructor validation returned Failure: %v", err)
	}
	if tccommon.DefaultHttpClient != globalClient {
		t.Error("Tencent global client pointer changed")
	}
	if globalClient.Timeout != 37*time.Second {
		t.Errorf("Tencent global client timeout = %s", globalClient.Timeout)
	}
	if globalClient.Transport != http.DefaultTransport {
		t.Error("Tencent global client transport changed")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newTestProvider(t *testing.T, client apiClient) *Provider {
	t.Helper()
	failures, err := failure.NewFactory("tencent")
	if err != nil {
		t.Fatal(err)
	}
	return &Provider{client: client, appID: "1400000000", failures: failures}
}

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

func TestNewConfiguresSDKProfile(t *testing.T) {
	var request *http.Request
	calls := 0
	httpClient := &http.Client{
		Timeout: 1500 * time.Millisecond,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			request = req
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"Response":{"Error":{"Code":"RequestLimitExceeded","Message":"slow down"},"RequestId":"request-5"}}`,
				)),
				Request: req,
			}, nil
		}),
	}
	provider, err := New(
		Config{SecretID: "id", SecretKey: "key", SMSAppID: "app", Region: "ap-guangzhou", DefaultSignatureRef: "Default"},
		WithHTTPClient(httpClient),
		WithEndpoint("sms.internal.example"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.client.(*tc.Client); !ok {
		t.Fatalf("client type = %T", provider.client)
	}

	_, _ = provider.Send(context.Background(), testRequest(t))
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if request == nil {
		t.Fatal("HTTP transport was not called")
	}
	if request.URL.Host != "sms.internal.example" {
		t.Fatalf("request host = %q", request.URL.Host)
	}
}

func TestClientProfileDisablesRetriesAndRegionBreaker(t *testing.T) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	clientProfile := newClientProfile(client, "sms.internal.example")

	if clientProfile.NetworkFailureMaxRetries != 0 {
		t.Fatalf("NetworkFailureMaxRetries = %d, want 0", clientProfile.NetworkFailureMaxRetries)
	}
	if clientProfile.RateLimitExceededMaxRetries != 0 {
		t.Fatalf("RateLimitExceededMaxRetries = %d, want 0", clientProfile.RateLimitExceededMaxRetries)
	}
	if !clientProfile.DisableRegionBreaker {
		t.Fatal("DisableRegionBreaker = false, want true")
	}
	if clientProfile.UnsafeRetryOnConnectionFailure {
		t.Fatal("UnsafeRetryOnConnectionFailure = true, want false")
	}
	if clientProfile.HttpProfile.ReqTimeout != 2 || clientProfile.HttpProfile.Endpoint != "sms.internal.example" {
		t.Fatalf("HttpProfile = %#v", clientProfile.HttpProfile)
	}
}

func TestRequestTimeout(t *testing.T) {
	tests := []struct {
		timeout time.Duration
		want    int
	}{
		{timeout: 0, want: 10},
		{timeout: time.Millisecond, want: 1},
		{timeout: 1500 * time.Millisecond, want: 2},
		{timeout: 10 * time.Second, want: 10},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.timeout), func(t *testing.T) {
			if got := requestTimeout(&http.Client{Timeout: tt.timeout}); got != tt.want {
				t.Fatalf("requestTimeout = %d, want %d", got, tt.want)
			}
		})
	}
}
