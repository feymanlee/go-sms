package sms

import (
	"context"
	"errors"
	"testing"
)

func TestSendErrorMatchesKindAndCause(t *testing.T) {
	err := &SendError{Kind: KindUnknownOutcome, Provider: "fake", Code: "timeout", Cause: context.DeadlineExceeded}
	if !errors.Is(err, ErrUnknownOutcome) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("matching failed: %v", err)
	}
	var detail *SendError
	if !errors.As(err, &detail) || detail.Provider != "fake" {
		t.Fatalf("detail = %#v", detail)
	}
	if got := err.Error(); got != "sms: fake unknown outcome (code=timeout)" {
		t.Fatalf("Error() = %q", got)
	}
}
