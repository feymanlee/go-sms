package ucloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	sms "github.com/feymanlee/go-sms"
)

func TestSendPostsSignedUSMSForm(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"Action": "SendUSMSMessage", "PhoneNumbers.0": "(86)13812345678",
			"TemplateParams.0": "123456", "TemplateParams.1": "5",
			"TemplateId": "template-1", "SigContent": "Example", "ProjectId": "project-1",
			"Region": "cn-bj2", "PublicKey": "public-1",
		}
		for key, value := range want {
			if got := r.Form.Get(key); got != value {
				t.Errorf("form[%q] = %q, want %q", key, got, value)
			}
		}
		if got := r.Form.Get("Signature"); got == "" {
			t.Error("Signature is empty")
		}
		_, _ = io.WriteString(w, `{"RetCode":0,"Message":"","SessionNo":"session-1"}`)
	}))
	defer server.Close()

	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if submission.Provider != "ucloud" || submission.MessageID != "session-1" || submission.RequestID != "" {
		t.Fatalf("submission = %#v", submission)
	}
}

func TestSendUsesDefaultSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("SigContent"); got != "Default" {
			t.Errorf("SigContent = %q", got)
		}
		_, _ = io.WriteString(w, `{"RetCode":0,"SessionNo":"session-1"}`)
	}))
	defer server.Close()

	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	req := testRequest(t)
	req.SignatureRef = ""
	if _, err := provider.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestSendDoesNotFollowRedirectsOrMutateCallerClient(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		_, _ = io.WriteString(w, `{"RetCode":0,"SessionNo":"session-redirect"}`)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	callerClient := source.Client()
	callerClient.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	provider, err := New(testConfig(), WithHTTPClient(callerClient), WithEndpoint(source.URL))
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
				_, _ = io.WriteString(w, "response must not leak")
			}))
			defer server.Close()
			provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Send(context.Background(), testRequest(t))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSendClassifiesProviderRejectionAndSanitizesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"RetCode":170,"Message":"rejected +8613812345678"}`)
	}))
	defer server.Close()
	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrRejected) || !errors.As(err, &detail) || detail.Code != "170" || strings.Contains(detail.Message, "13812345678") {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
}

func TestSendDoesNotExposeUntrustedProviderMessages(t *testing.T) {
	req := testRequest(t)
	secrets := []string{
		"+12025550123",
		"%2B8613812345678",
		"private-1",
		req.Message.Params[0].Value,
		`{"PhoneNumbers.0":"(1)2025550123","TemplateParams.0":"123456"}`,
	}
	remoteMessage := strings.Join(secrets, " | ")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"RetCode":170,"Message":`+strconv.Quote(remoteMessage)+`}`)
	}))
	defer server.Close()
	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(context.Background(), req)
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrRejected) || !errors.As(err, &detail) {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
	if detail.Code != "170" || detail.Message != "ucloud provider request failed" {
		t.Fatalf("detail = %#v", detail)
	}
	for _, text := range []string{err.Error(), detail.Message} {
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("public error text leaked %q: %q", secret, text)
			}
		}
	}
}

func TestSendRejectsMalformedSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: "not JSON for +8613812345678"},
		{name: "missing RetCode", body: `{"SessionNo":"session-1"}`},
		{name: "null RetCode", body: `{"RetCode":null,"SessionNo":"session-1"}`},
		{name: "missing session", body: `{"RetCode":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendRejectsContentAfterOneJSONDocument(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "trailing garbage", body: `{"RetCode":0,"SessionNo":"session-1"} trailing`},
		{name: "second document", body: `{"RetCode":0,"SessionNo":"session-1"}{"RetCode":0,"SessionNo":"session-2"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}

			_, err = provider.Send(context.Background(), testRequest(t))
			if !errors.Is(err, sms.ErrInternal) {
				t.Fatalf("error = %v, want internal", err)
			}
		})
	}
}

func TestSendAcceptsTrailingJSONWhitespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"RetCode\":0,\"SessionNo\":\"session-1\"}\n\t ")
	}))
	defer server.Close()
	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil || submission.MessageID != "session-1" {
		t.Fatalf("submission = %#v, error = %v", submission, err)
	}
}

func TestSendReturnsUnknownOutcomeAfterTransportError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("connection reset for +8613812345678")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		cancel()
		return nil, cause
	})}
	provider, err := New(testConfig(), WithHTTPClient(client), WithEndpoint("https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(ctx, testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") || calls.Load() != 1 {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
}

func TestSendRejectsCanceledContextWithoutAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected")
	})}
	provider, err := New(testConfig(), WithHTTPClient(client), WithEndpoint("https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestNewValidatesRequiredConfig(t *testing.T) {
	valid := testConfig()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "public key", mutate: func(c *Config) { c.PublicKey = " " }},
		{name: "private key", mutate: func(c *Config) { c.PrivateKey = "" }},
		{name: "project ID", mutate: func(c *Config) { c.ProjectID = "" }},
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

func testConfig() Config {
	return Config{
		PublicKey: "public-1", PrivateKey: "private-1", ProjectID: "project-1", Region: "cn-bj2", DefaultSignatureRef: "Default",
	}
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
		SignatureRef: "Example",
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
