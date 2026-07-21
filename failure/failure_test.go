package failure_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/feymanlee/go-sms/failure"
)

func TestDecisionProducesSafeImmutableFailure(t *testing.T) {
	factory, err := failure.NewFactory("aliyun")
	if err != nil {
		t.Fatal(err)
	}

	got := factory.Decision(failure.RateLimited, failure.Diagnostic{
		Code: "isv.BUSINESS_LIMIT_CONTROL", RequestID: "request-1",
	})
	if got.Category() != failure.RateLimited || got.UnknownOutcome() {
		t.Fatalf("category=%q unknown=%v", got.Category(), got.UnknownOutcome())
	}
	if details := got.Details(); details != (failure.Details{
		Provider: "aliyun", Code: "isv.BUSINESS_LIMIT_CONTROL", RequestID: "request-1",
	}) {
		t.Fatalf("details=%#v", details)
	}
	if got.Error() != "sms: aliyun rate limited" {
		t.Fatalf("Error=%q", got.Error())
	}
	if errors.Unwrap(got) != nil {
		t.Fatalf("Unwrap=%v", errors.Unwrap(got))
	}
}

func TestFromFindsWrappedFailure(t *testing.T) {
	factory, _ := failure.NewFactory("qiniu")
	original := factory.Decision(failure.Rejected, failure.Diagnostic{Code: "400"})
	got, ok := failure.From(fmt.Errorf("send welcome: %w", original))
	if !ok || got != original {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
	if got, ok := failure.From(errors.New("ordinary")); ok || got != nil {
		t.Fatalf("ordinary got=%v ok=%v", got, ok)
	}
}

func TestUnknownRecordsOnlyContextSentinels(t *testing.T) {
	factory, _ := failure.NewFactory("tencent")
	native := errors.New("secret native error")
	got := factory.Unknown(failure.Diagnostic{RequestID: "request-2"}, errors.Join(native, context.DeadlineExceeded))
	if got.Category() != failure.UnknownOutcome || !got.UnknownOutcome() {
		t.Fatalf("category=%q unknown=%v", got.Category(), got.UnknownOutcome())
	}
	if !errors.Is(got, context.DeadlineExceeded) || errors.Is(got, native) {
		t.Fatalf("context/native matching: %v", got)
	}
	if got.Error() != "sms: tencent unknown outcome" {
		t.Fatalf("Error=%q", got.Error())
	}
}

func TestFactoryValidationAndSafeDegradation(t *testing.T) {
	invalidProviders := []string{"", "Aliyun", " aliyun", "aliyun.example", "provider-name-that-is-far-too-long"}
	for _, provider := range invalidProviders {
		if _, err := failure.NewFactory(provider); err == nil {
			t.Fatalf("NewFactory(%q) succeeded", provider)
		}
	}

	factory, _ := failure.NewFactory("ucloud")
	got := factory.Decision(failure.Category("unsupported"), failure.Diagnostic{
		Code: "bad code with spaces", RequestID: "line\nbreak",
	})
	if got.Category() != failure.UnknownOutcome {
		t.Fatalf("category=%q", got.Category())
	}
	if got.Details().Code != "" || got.Details().RequestID != "" {
		t.Fatalf("details=%#v", got.Details())
	}
}

func TestDecisionCategories(t *testing.T) {
	tests := []struct {
		category failure.Category
	}{
		{failure.Authentication},
		{failure.RateLimited},
		{failure.Rejected},
		{failure.Unavailable},
	}

	factory, _ := failure.NewFactory("yunpian")
	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			got := factory.Decision(tt.category, failure.Diagnostic{})
			if got.Category() != tt.category || got.UnknownOutcome() {
				t.Fatalf("category=%q unknown=%v", got.Category(), got.UnknownOutcome())
			}
			if errors.Is(got, context.Canceled) || errors.Is(got, context.DeadlineExceeded) {
				t.Fatalf("decision retained context evidence: %v", got)
			}
		})
	}
}

func TestUnknownContextEvidence(t *testing.T) {
	native := &nativeEvidence{}
	tests := []struct {
		name     string
		evidence error
		canceled bool
		deadline bool
	}{
		{"canceled", context.Canceled, true, false},
		{"deadline", context.DeadlineExceeded, false, true},
		{"joined", errors.Join(context.Canceled, context.DeadlineExceeded), true, true},
		{"native", native, false, false},
	}

	factory, _ := failure.NewFactory("tencent")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := factory.Unknown(failure.Diagnostic{}, tt.evidence)
			if errors.Is(got, context.Canceled) != tt.canceled || errors.Is(got, context.DeadlineExceeded) != tt.deadline {
				t.Fatalf("canceled=%v deadline=%v", errors.Is(got, context.Canceled), errors.Is(got, context.DeadlineExceeded))
			}
			if errors.Is(got, native) {
				t.Fatal("native evidence is recoverable with errors.Is")
			}
			var recovered *nativeEvidence
			if errors.As(got, &recovered) {
				t.Fatal("native evidence is recoverable with errors.As")
			}
		})
	}
}

func TestZeroFactory(t *testing.T) {
	var factory failure.Factory
	got := factory.Decision(failure.Authentication, failure.Diagnostic{})
	if got.Category() != failure.Authentication || got.Details().Provider != "unknown" {
		t.Fatalf("category=%q details=%#v", got.Category(), got.Details())
	}
}

func TestDiagnosticValidation(t *testing.T) {
	tests := []struct {
		name       string
		diagnostic failure.Diagnostic
		code       string
		requestID  string
	}{
		{"empty", failure.Diagnostic{}, "", ""},
		{"minimum", failure.Diagnostic{Code: "a", RequestID: "b"}, "a", "b"},
		{"maximum", failure.Diagnostic{Code: strings.Repeat("a", 128), RequestID: strings.Repeat("b", 256)}, strings.Repeat("a", 128), strings.Repeat("b", 256)},
		{"too long", failure.Diagnostic{Code: strings.Repeat("a", 129), RequestID: strings.Repeat("b", 257)}, "", ""},
		{"spaces", failure.Diagnostic{Code: "bad code", RequestID: "bad request"}, "", ""},
		{"newline", failure.Diagnostic{Code: "bad\ncode", RequestID: "bad\nrequest"}, "", ""},
		{"control", failure.Diagnostic{Code: "bad\x00code", RequestID: "bad\x1frequest"}, "", ""},
		{"unicode", failure.Diagnostic{Code: "cafe\u00e9", RequestID: "\u8bf7\u6c42"}, "", ""},
	}

	factory, _ := failure.NewFactory("qiniu")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := factory.Decision(failure.Rejected, tt.diagnostic).Details()
			if got.Code != tt.code || got.RequestID != tt.requestID {
				t.Fatalf("details=%#v want code=%q requestID=%q", got, tt.code, tt.requestID)
			}
		})
	}
}

type nativeEvidence struct{}

func (*nativeEvidence) Error() string { return "secret native error" }
