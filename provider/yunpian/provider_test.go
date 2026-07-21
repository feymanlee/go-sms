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
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
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
		const want = "apikey=api-key&mobile=%2B8613812345678&tpl_id=template-1&tpl_value=%23code%23%3D123456%26%23minutes%23%3D5"
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
		if got := form.Get("mobile"); got != "+8613812345678" {
			t.Errorf("mobile = %q", got)
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

func TestSendPreservesNonChineseE164Recipient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("mobile"); got != "+12025550123" {
			t.Errorf("mobile = %q", got)
		}
		_, _ = io.WriteString(w, `{"code":0,"sid":1}`)
	}))
	defer server.Close()

	req := testRequest(t)
	recipient, err := sms.ParseRecipient("+12025550123")
	if err != nil {
		t.Fatal(err)
	}
	req.Recipient = recipient

	if _, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), req); err != nil {
		t.Fatal(err)
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
	requireFailure(t, err, failure.Rejected)
	if targetCalls.Load() != 0 {
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
	if err == nil {
		t.Fatal("Send returned nil error")
	}
	if _, ok := failure.From(err); ok || calls.Load() != 0 {
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
	if err == nil {
		t.Fatal("Send returned nil error")
	}
	if _, ok := failure.From(err); ok || !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendClassifiesHTTPResponses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		category failure.Category
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, category: failure.Authentication},
		{name: "forbidden", status: http.StatusForbidden, category: failure.Authentication},
		{name: "rate limited", status: http.StatusTooManyRequests, category: failure.RateLimited},
		{name: "unavailable", status: http.StatusBadGateway, category: failure.Unavailable},
		{name: "rejected", status: http.StatusBadRequest, category: failure.Rejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"code":42,"msg":"untrusted"}`)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, tt.category)
			if details := got.Details(); details.Code != strconv.Itoa(tt.status) {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendClassifiesYunpianRejectionsWithoutExposingRemoteText(t *testing.T) {
	const remote = `request +8613812345678 and +12025550123, Authorization: Bearer private-token, body={"mobile":"13812345678"}`
	for _, code := range []string{"42", "9007199254740993"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"code":`+code+`,"msg":`+quoteJSON(remote)+`,"detail":`+quoteJSON(remote)+`}`)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, failure.Rejected)
			if details := got.Details(); details.Code != code {
				t.Fatalf("details = %#v", details)
			}
			for _, secret := range []string{"+8613812345678", "+12025550123", "private-token", `{"mobile":"13812345678"}`} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("remote text leaked %q: error=%q", secret, err)
				}
			}
		})
	}
}

func TestSendReturnsUnknownOutcomeForMalformedOrIncompleteJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "decode failure", body: "not JSON for +8613812345678"},
		{name: "missing SID", body: `{"code":0}`},
		{name: "missing Code", body: `{"sid":1}`},
		{name: "trailing garbage", body: `{"code":0,"sid":1} trailing garbage`},
		{name: "second document", body: `{"code":0,"sid":1}{"code":0,"sid":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			requireFailure(t, err, failure.UnknownOutcome)
		})
	}
}

func TestSendReturnsUnknownOutcomeAfterTransportError(t *testing.T) {
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
	requireFailure(t, err, failure.UnknownOutcome)
	if errors.Is(err, cause) || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
	if strings.Contains(err.Error(), cause.recipient) || strings.Contains(err.Error(), cause.credential) {
		t.Fatalf("transport error leaked: %q", err)
	}
	var recovered *secretTransportError
	if errors.As(err, &recovered) {
		t.Fatalf("raw transport error leaked through chain: %#v", recovered)
	}
}

func TestSendReturnsUnknownOutcomeForNilResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})}

	_, err := testProvider(t, client, "https://example.invalid").Send(context.Background(), testRequest(t))
	requireFailure(t, err, failure.UnknownOutcome)
}

func TestSendRecordsTransportContextEvidence(t *testing.T) {
	tests := []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		want    error
	}{
		{name: "canceled", context: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, want: context.Canceled},
		{name: "deadline exceeded", context: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 10*time.Millisecond)
		}, want: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.context()
			defer cancel()
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if tt.want == context.Canceled {
					cancel()
				} else {
					<-req.Context().Done()
				}
				return nil, errors.New("transport failure")
			})}

			_, err := testProvider(t, client, "https://example.invalid").Send(ctx, testRequest(t))
			requireFailure(t, err, failure.UnknownOutcome)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSendReturnsOrdinaryErrorWhenRequestCannotBeCreated(t *testing.T) {
	_, err := testProvider(t, http.DefaultClient, "://invalid").Send(context.Background(), testRequest(t))
	if err == nil {
		t.Fatal("Send returned nil error")
	}
	if _, ok := failure.From(err); ok {
		t.Fatalf("request construction returned Failure: %v", err)
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
