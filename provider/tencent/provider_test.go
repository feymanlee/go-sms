package tencent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
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
	provider := &Provider{client: fake, appID: "1400000000", defaultSignature: "Default"}

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
	provider := &Provider{client: fake, appID: "1400000000", defaultSignature: "Default"}
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
	provider := &Provider{client: fake, appID: "1400000000"}

	_, err := provider.Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrRejected) {
		t.Fatalf("error = %v", err)
	}
	var detail *sms.SendError
	if !errors.As(err, &detail) || detail.Code != "FailedOperation.TemplateIncorrectOrUnapproved" || detail.RequestID != "request-2" || detail.Message != "bad template" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestSendClassifiesKnownStatusCode(t *testing.T) {
	fake := &fakeClient{response: response("LimitExceeded.PhoneNumberThirtySecondLimit", "too frequent", "", 0, "request-2")}
	provider := &Provider{client: fake, appID: "1400000000"}

	_, err := provider.Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrRateLimited) {
		t.Fatalf("error = %v", err)
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
		{name: "URL", err: &url.Error{Op: "Post", URL: "https://sms.tencentcloudapi.com", Err: errors.New("connection reset")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{err: tt.err}
			provider := &Provider{client: fake, appID: "1400000000"}
			_, err := provider.Send(context.Background(), testRequest(t))
			if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, tt.err) {
				t.Fatalf("error = %v", err)
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
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	var detail *sms.SendError
	var native *tcerr.TencentCloudSDKError
	if !errors.As(err, &detail) || detail.Code != "ClientError.NetworkError" {
		t.Fatalf("detail = %#v", detail)
	}
	if errors.As(err, &native) {
		t.Fatalf("raw SDK error leaked through error chain: %#v", native)
	}
	if !errors.Is(err, context.Canceled) || errors.Unwrap(detail.Cause) != nil {
		t.Fatalf("cause = %v", detail.Cause)
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
	if !errors.Is(err, sms.ErrUnknownOutcome) {
		t.Fatalf("error = %v", err)
	}
	var detail *sms.SendError
	var native *tcerr.TencentCloudSDKError
	if !errors.As(err, &detail) || detail.Code != "ClientError.NetworkError" {
		t.Fatalf("detail = %#v", detail)
	}
	if errors.As(err, &native) || errors.Unwrap(detail.Cause) != nil {
		t.Fatalf("native error leaked: %#v", native)
	}
}

func TestSendPreservesSDKNetworkErrorDetails(t *testing.T) {
	native := tcerr.NewTencentCloudSDKError(
		"ClientError.NetworkError",
		"failed for +8613812345678, +12025550123, Authorization: Bearer secret-token, {\"message\":\"private response body\"}",
		"request-network",
	)
	provider := &Provider{client: &fakeClient{err: native}, appID: "1400000000"}

	_, err := provider.Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.As(err, &detail) {
		t.Fatalf("error = %v", err)
	}
	if detail.Kind != sms.KindUnknownOutcome || detail.Code != "ClientError.NetworkError" || detail.RequestID != "request-network" {
		t.Fatalf("detail = %#v", detail)
	}
	for _, secret := range []string{"+8613812345678", "+12025550123", "Authorization: Bearer secret-token", `{\"message\":\"private response body\"}`} {
		if strings.Contains(err.Error(), secret) || strings.Contains(detail.Message, secret) || strings.Contains(errors.Unwrap(err).Error(), secret) {
			t.Fatalf("public error text leaked %q: error=%q message=%q cause=%q", secret, err, detail.Message, errors.Unwrap(err))
		}
	}
	if !errors.Is(err, native) {
		t.Fatalf("error does not preserve native identity: %v", err)
	}
	var recovered *tcerr.TencentCloudSDKError
	if errors.As(err, &recovered) {
		t.Fatalf("raw SDK error leaked through error chain: %#v", recovered)
	}
	if detail.Cause == native || errors.Unwrap(detail.Cause) != nil {
		t.Fatalf("cause = %#v", detail.Cause)
	}
}

func TestSendClassifiesNativeErrors(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{code: "AuthFailure.SecretIdNotFound", want: sms.ErrAuthentication},
		{code: "InvalidCredential", want: sms.ErrAuthentication},
		{code: "InvalidCredential.Expired", want: sms.ErrAuthentication},
		{code: "UnauthorizedOperation.RequestPermissionDeny", want: sms.ErrAuthentication},
		{code: "RequestLimitExceeded", want: sms.ErrRateLimited},
		{code: "RequestLimitExceeded.Global", want: sms.ErrRateLimited},
		{code: "LimitExceeded.PhoneNumberDailyLimit", want: sms.ErrRateLimited},
		{code: "InternalError", want: sms.ErrUnavailable},
		{code: "InternalErrorUnexpected", want: sms.ErrUnavailable},
		{code: "ResourceUnavailable.ServiceBusy", want: sms.ErrUnavailable},
		{code: "UnsupportedOperation", want: sms.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			native := tcerr.NewTencentCloudSDKError(tt.code, "failed for +8613812345678", "request-3")
			fake := &fakeClient{err: native}
			provider := &Provider{client: fake, appID: "1400000000"}

			_, err := provider.Send(context.Background(), testRequest(t))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			var detail *sms.SendError
			if !errors.As(err, &detail) || detail.Code != tt.code || detail.RequestID != "request-3" || strings.Contains(detail.Message, "13812345678") || !errors.Is(err, native) {
				t.Fatalf("detail = %#v", detail)
			}
		})
	}
}

func TestSendSanitizesRejectedStatus(t *testing.T) {
	fake := &fakeClient{response: response("FailedOperation", "failed for +8613812345678", "", 0, "request-4")}
	provider := &Provider{client: fake, appID: "1400000000"}

	_, err := provider.Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestSendRejectsMalformedResponses(t *testing.T) {
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
			provider := &Provider{client: &fakeClient{response: tt.response}, appID: "1400000000"}
			_, err := provider.Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || detail.RequestID != tt.requestID {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendOmitsFeeMetadataWhenFeeIsMissing(t *testing.T) {
	accepted := response("Ok", "send success", "serial-1", 0, "request-1")
	accepted.Response.SendStatusSet[0].Fee = nil
	provider := &Provider{client: &fakeClient{response: accepted}, appID: "1400000000"}

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
	provider := &Provider{client: fake, appID: "1400000000"}
	req := testRequest(t)
	req.SignatureRef = ""

	_, err := provider.Send(context.Background(), req)
	if !errors.Is(err, sms.ErrInvalidRequest) || fake.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, fake.calls)
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
