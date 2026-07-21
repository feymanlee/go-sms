package failure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Category string

const (
	Authentication Category = "authentication"
	RateLimited    Category = "rate_limited"
	Rejected       Category = "rejected"
	Unavailable    Category = "unavailable"
	UnknownOutcome Category = "unknown_outcome"
)

type Details struct{ Provider, Code, RequestID string }
type Diagnostic struct{ Code, RequestID string }

type Failure interface {
	error
	Category() Category
	Details() Details
	UnknownOutcome() bool
	isFailure()
}

type normalized struct {
	category Category
	details  Details
	canceled bool
	deadline bool
}

func (f *normalized) Error() string {
	return fmt.Sprintf("sms: %s %s", f.details.Provider, strings.ReplaceAll(string(f.category), "_", " "))
}

func (f *normalized) Category() Category   { return f.category }
func (f *normalized) Details() Details     { return f.details }
func (f *normalized) UnknownOutcome() bool { return f.category == UnknownOutcome }
func (*normalized) isFailure()             {}
func (f *normalized) Is(target error) bool {
	return target == context.Canceled && f.canceled || target == context.DeadlineExceeded && f.deadline
}

func From(err error) (Failure, bool) {
	if err == nil {
		return nil, false
	}
	var got Failure
	if !errors.As(err, &got) {
		return nil, false
	}
	return got, true
}

type Factory struct{ provider string }

var (
	providerToken  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	codeToken      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	requestIDToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=-]{0,255}$`)
)

func NewFactory(provider string) (Factory, error) {
	if !providerToken.MatchString(provider) {
		return Factory{}, fmt.Errorf("failure: invalid Provider identifier")
	}
	return Factory{provider: provider}, nil
}

func (f Factory) Decision(category Category, diagnostic Diagnostic) Failure {
	if !isDecision(category) {
		return f.Unknown(diagnostic, nil)
	}
	return f.new(category, diagnostic, nil)
}

func (f Factory) Unknown(diagnostic Diagnostic, contextEvidence error) Failure {
	return f.new(UnknownOutcome, diagnostic, contextEvidence)
}

func (f Factory) new(category Category, diagnostic Diagnostic, contextEvidence error) Failure {
	provider := f.provider
	if !providerToken.MatchString(provider) {
		provider = "unknown"
	}
	details := Details{Provider: provider}
	if codeToken.MatchString(diagnostic.Code) {
		details.Code = diagnostic.Code
	}
	if requestIDToken.MatchString(diagnostic.RequestID) {
		details.RequestID = diagnostic.RequestID
	}
	return &normalized{
		category: category,
		details:  details,
		canceled: errors.Is(contextEvidence, context.Canceled),
		deadline: errors.Is(contextEvidence, context.DeadlineExceeded),
	}
}

func isDecision(category Category) bool {
	return category == Authentication || category == RateLimited || category == Rejected || category == Unavailable
}
