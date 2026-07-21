package yunpian

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	sms "github.com/feymanlee/go-sms"
)

func TestSendPostsTemplateFormAndReturnsSubmission(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v2/sms/tpl_single_send.json" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		const want = "apikey=api-key&mobile=13812345678&tpl_id=template-1&tpl_value=%23code%23%3D123456%26%23minutes%23%3D5"
		if got := string(body); got != want {
			t.Errorf("form = %q, want %q", got, want)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if got := form.Get("tpl_value"); got != "#code#=123456&#minutes#=5" {
			t.Errorf("tpl_value = %q", got)
		}
		if got := form.Get("signature"); got != "" {
			t.Errorf("signature = %q", got)
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"发送成功","count":1,"fee":0.05,"unit":"RMB","mobile":"13812345678","sid":1234567890}`)
	}))
	defer server.Close()

	submission, err := testProvider(t, server.Client(), server.URL+"/v2/sms/tpl_single_send.json").Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if submission.Provider != "yunpian" || submission.MessageID != "1234567890" || submission.RequestID != "" {
		t.Fatalf("submission = %#v", submission)
	}
	if got, want := submission.Metadata, map[string]string{"count": "1", "fee": "0.05", "unit": "RMB"}; !sameMetadata(got, want) {
		t.Fatalf("Metadata = %#v, want %#v", got, want)
	}
}

func TestSendPreservesLargeNumericSID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"sid":9007199254740993}`)
	}))
	defer server.Close()

	submission, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
	if err != nil || submission.MessageID != "9007199254740993" {
		t.Fatalf("submission = %#v, err = %v", submission, err)
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		_, _ = io.WriteString(w, `{"code":0,"sid":1}`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrRejected) || targetCalls.Load() != 0 {
		t.Fatalf("error = %v, target calls = %d", err, targetCalls.Load())
	}
}

func TestSendRejectsEmptyTemplateParameterWithoutHTTPAttempt(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected request")
	})}
	req := testRequest(t)
	req.Message.Params[1].Value = ""

	_, err := testProvider(t, client, "https://example.invalid").Send(context.Background(), req)
	if !errors.Is(err, sms.ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendDoesNotCallTransportForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected request")
	})}

	_, err := testProvider(t, client, "https://example.invalid").Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendClassifiesHTTPResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: sms.ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, want: sms.ErrAuthentication},
		{name: "rate limited", status: http.StatusTooManyRequests, want: sms.ErrRateLimited},
		{name: "unavailable", status: http.StatusBadGateway, want: sms.ErrUnavailable},
		{name: "rejected", status: http.StatusBadRequest, want: sms.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"code":42,"msg":"untrusted"}`)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, tt.want) || !errors.As(err, &detail) || detail.Code != strconv.Itoa(tt.status) || detail.Message != non2xxMessage {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendClassifiesYunpianRejectionWithoutExposingRemoteText(t *testing.T) {
	const remote = `request +8613812345678 and +12025550123, Authorization: Bearer private-token, body={"mobile":"13812345678"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":42,"msg":`+quoteJSON(remote)+`,"detail":`+quoteJSON(remote)+`}`)
	}))
	defer server.Close()

	_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrRejected) || !errors.As(err, &detail) || detail.Code != "42" || detail.Message != providerRejectionMessage {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
	for _, secret := range []string{"+8613812345678", "+12025550123", "private-token", `{"mobile":"13812345678"}`} {
		if strings.Contains(err.Error(), secret) || strings.Contains(detail.Message, secret) {
			t.Fatalf("remote text leaked %q: error=%q message=%q", secret, err, detail.Message)
		}
	}
}

func TestSendRejectsMalformedOrIncompleteJSON(t *testing.T) {
	for _, body := range []string{
		"not JSON for +8613812345678",
		`{"code":0}`,
		`{"sid":1}`,
		`{"code":0,"sid":1} trailing garbage`,
		`{"code":0,"sid":1}{"code":0,"sid":2}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendReturnsPrivateUnknownOutcomeAfterTransportError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := &secretTransportError{recipient: "+8613812345678", credential: "Bearer private-token"}
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		cancel()
		return nil, cause
	})}

	_, err := testProvider(t, client, "https://example.invalid").Send(ctx, testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &detail) || calls.Load() != 1 {
		t.Fatalf("error = %v, detail = %#v, calls = %d", err, detail, calls.Load())
	}
	if strings.Contains(err.Error(), cause.recipient) || strings.Contains(err.Error(), cause.credential) {
		t.Fatalf("transport error leaked: %q", err)
	}
	var recovered *secretTransportError
	if errors.As(err, &recovered) {
		t.Fatalf("raw transport error leaked through chain: %#v", recovered)
	}
}

func TestNewValidatesAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New returned nil error")
	}
}

func testProvider(t *testing.T, client *http.Client, endpoint string) *Provider {
	t.Helper()
	provider, err := New(Config{APIKey: "api-key"}, WithHTTPClient(client), WithEndpoint(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testRequest(t *testing.T) sms.Request {
	t.Helper()
	recipient, err := sms.ParseRecipient("+8613812345678")
	if err != nil {
		t.Fatal(err)
	}
	return sms.Request{
		Recipient: recipient,
		Message: sms.TemplateMessage{TemplateID: "template-1", Params: []sms.TemplateParam{
			{Name: "code", Value: "123456"}, {Name: "minutes", Value: "5"},
		}},
		SignatureRef: "must-not-be-sent",
	}
}

func sameMetadata(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type secretTransportError struct {
	recipient  string
	credential string
}

func (e *secretTransportError) Error() string {
	return "request failed for " + e.recipient + " with " + e.credential
}
