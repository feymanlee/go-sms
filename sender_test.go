package sms

import (
	"context"
	"testing"
)

type senderFunc func(context.Context, Request) (Submission, error)

func (f senderFunc) Send(ctx context.Context, req Request) (Submission, error) {
	return f(ctx, req)
}

func TestSenderContract(t *testing.T) {
	var _ Sender = senderFunc(func(context.Context, Request) (Submission, error) {
		return Submission{Provider: "fake", MessageID: "message-1"}, nil
	})
}
