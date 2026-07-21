package providerutil

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
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
	signature, err := Prepare(context.Background(), "fake", req, "default-signature", true)
	if err != nil || signature != "default-signature" {
		t.Fatalf("signature=%q err=%v", signature, err)
	}
	req.SignatureRef = "request-signature"
	signature, err = Prepare(context.Background(), "fake", req, "default-signature", true)
	if err != nil || signature != "request-signature" {
		t.Fatalf("signature=%q err=%v", signature, err)
	}
	if _, err := Prepare(context.Background(), "fake", request(t, "+8613812345678"), "", true); !errors.Is(err, sms.ErrInvalidRequest) {
		t.Fatalf("missing signature error=%v", err)
	}
}

func TestPrepareDoesNotStartWithDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Prepare(ctx, "fake", request(t, "+8613812345678"), "", false)
	if !errors.Is(err, context.Canceled) || errors.Is(err, sms.ErrUnknownOutcome) {
		t.Fatalf("error=%v", err)
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

func TestUnknownOutcomeSanitizesUnwrappedCauseWithoutLosingIsMatchability(t *testing.T) {
	req := request(t, "+8613812345678")
	raw := &secretTransportError{recipient: req.Recipient.String()}
	err := UnknownOutcome("fake", req.Recipient, raw)

	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, raw) {
		t.Fatalf("error does not preserve sentinel or raw identity: %v", err)
	}
	if strings.Contains(err.Error(), raw.recipient) {
		t.Fatalf("Error leaked recipient: %q", err)
	}
	var recovered *secretTransportError
	if errors.As(err, &recovered) {
		t.Fatalf("raw transport error leaked through error chain: %#v", recovered)
	}
	unwrap := errors.Unwrap(err)
	if unwrap == nil || unwrap == raw || unwrap.Error() != "transport failed for [recipient]" || errors.Unwrap(unwrap) != nil || strings.Contains(unwrap.Error(), raw.recipient) {
		t.Fatalf("unwrap = %#v", unwrap)
	}
}

type secretTransportError struct {
	recipient string
}

func (e *secretTransportError) Error() string {
	return "transport failed for " + e.recipient
}
