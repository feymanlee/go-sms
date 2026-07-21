package sms

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	KindInvalidRequest ErrorKind = "invalid_request"
	KindAuthentication ErrorKind = "authentication"
	KindRateLimited    ErrorKind = "rate_limited"
	KindRejected       ErrorKind = "rejected"
	KindUnavailable    ErrorKind = "unavailable"
	KindUnknownOutcome ErrorKind = "unknown_outcome"
	KindInternal       ErrorKind = "internal"
)

var (
	ErrInvalidRequest = errors.New("sms: invalid request")
	ErrAuthentication = errors.New("sms: authentication failed")
	ErrRateLimited    = errors.New("sms: rate limited")
	ErrRejected       = errors.New("sms: request rejected")
	ErrUnavailable    = errors.New("sms: provider unavailable")
	ErrUnknownOutcome = errors.New("sms: outcome unknown")
	ErrInternal       = errors.New("sms: internal error")
)

type SendError struct {
	Kind      ErrorKind
	Provider  string
	Code      string
	Message   string
	RequestID string
	Cause     error
}

func (e *SendError) Error() string {
	label := map[ErrorKind]string{
		KindInvalidRequest: "invalid request",
		KindAuthentication: "authentication failed",
		KindRateLimited:    "rate limited",
		KindRejected:       "rejected",
		KindUnavailable:    "unavailable",
		KindUnknownOutcome: "unknown outcome",
		KindInternal:       "internal error",
	}[e.Kind]
	prefix := "sms: "
	if e.Provider != "" {
		prefix += e.Provider + " "
	}
	if e.Code != "" {
		return fmt.Sprintf("%s%s (code=%s)", prefix, label, e.Code)
	}
	return prefix + label
}

func (e *SendError) Unwrap() error { return e.Cause }

func (e *SendError) Is(target error) bool {
	sentinels := map[ErrorKind]error{
		KindInvalidRequest: ErrInvalidRequest,
		KindAuthentication: ErrAuthentication,
		KindRateLimited:    ErrRateLimited,
		KindRejected:       ErrRejected,
		KindUnavailable:    ErrUnavailable,
		KindUnknownOutcome: ErrUnknownOutcome,
		KindInternal:       ErrInternal,
	}
	return target == sentinels[e.Kind]
}
