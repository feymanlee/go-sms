package sms

import (
	"context"
	"errors"
	"testing"
)

type countingCause struct {
	isCalls int
}

func (e *countingCause) Error() string { return "counting cause" }

func (e *countingCause) Is(error) bool {
	e.isCalls++
	return false
}

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

func TestSendErrorCauseIsCalledOnceForNonMatchingTarget(t *testing.T) {
	cause := &countingCause{}
	err := &SendError{Kind: KindUnavailable, Cause: cause}

	if errors.Is(err, errors.New("nonmatching")) {
		t.Fatal("errors.Is unexpectedly matched")
	}
	if cause.isCalls != 1 {
		t.Fatalf("cause Is calls = %d, want 1", cause.isCalls)
	}
}
