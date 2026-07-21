package providerutil

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
)

func request(t *testing.T, number string) sms.Request {
	t.Helper()
	recipient, err := sms.ParseRecipient(number)
	if err != nil {
		t.Fatal(err)
	}
	return sms.Request{Recipient: recipient, Message: sms.TemplateMessage{TemplateID: "tpl"}}
}

func TestPrepare(t *testing.T) {
	req := request(t, "+8613812345678")
	signature, err := Prepare(context.Background(), req, "default-signature", true)
	if err != nil || signature != "default-signature" {
		t.Fatalf("signature=%q err=%v", signature, err)
	}
	req.SignatureRef = "request-signature"
	signature, err = Prepare(context.Background(), req, "default-signature", true)
	if err != nil || signature != "request-signature" {
		t.Fatalf("signature=%q err=%v", signature, err)
	}
	if _, err := Prepare(context.Background(), request(t, "+8613812345678"), "", true); err == nil {
		t.Fatal("missing signature succeeded")
	} else if _, ok := failure.From(err); ok {
		t.Fatalf("missing signature returned Failure: %v", err)
	}
}

func TestPrepareReturnsOrdinaryErrorsBeforeInvocation(t *testing.T) {
	invalid := request(t, "+8613812345678")
	invalid.Message.TemplateID = ""
	tests := []struct {
		name string
		ctx  context.Context
		req  sms.Request
	}{
		{name: "nil context", req: request(t, "+8613812345678")},
		{name: "invalid request", ctx: context.Background(), req: invalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Prepare(tt.ctx, tt.req, "", false)
			if err == nil {
				t.Fatal("Prepare succeeded")
			}
			if _, ok := failure.From(err); ok {
				t.Fatalf("Prepare returned Failure: %v", err)
			}
		})
	}
}

func TestPrepareDoesNotStartWithDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Prepare(ctx, request(t, "+8613812345678"), "", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, ok := failure.From(err); ok {
		t.Fatalf("done Context returned Failure: %v", err)
	}
}

func TestPhoneFormats(t *testing.T) {
	if got, _ := ChinaNational("+8613812345678"); got != "13812345678" {
		t.Fatalf("ChinaNational=%q", got)
	}
	if got, _ := UCloudNumber("+60123456789"); got != "(60)123456789" {
		t.Fatalf("UCloudNumber=%q", got)
	}
}

func TestNewHTTPClientTimeout(t *testing.T) {
	if NewHTTPClient().Timeout != 10*time.Second {
		t.Fatalf("timeout=%s", NewHTTPClient().Timeout)
	}
}

func TestNoRedirectTransportDoesNotMutateBaseResponse(t *testing.T) {
	body := http.NoBody
	baseResponse := &http.Response{
		StatusCode: http.StatusTemporaryRedirect,
		Header: http.Header{
			"Location": []string{"https://redirect.example.test"},
			"X-Base":   []string{"retained"},
		},
		Body: body,
	}
	transport := NoRedirectTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return baseResponse, nil
	}))
	req, err := http.NewRequest(http.MethodPost, "https://source.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if response == baseResponse {
		t.Fatal("response aliases base response")
	}
	if got := response.Header.Get("Location"); got != "" {
		t.Fatalf("wrapped Location = %q, want empty", got)
	}
	if got := response.Header.Get("X-Base"); got != "retained" {
		t.Fatalf("wrapped X-Base = %q, want retained", got)
	}
	if response.Body != body {
		t.Fatal("wrapped response body identity changed")
	}
	if got := baseResponse.Header.Get("Location"); got != "https://redirect.example.test" {
		t.Fatalf("base Location = %q, want retained redirect", got)
	}
	if got := baseResponse.Header.Get("X-Base"); got != "retained" {
		t.Fatalf("base X-Base = %q, want retained", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
