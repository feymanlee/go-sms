package sms

import (
	"testing"

	"github.com/feymanlee/go-sms/failure"
)

func validRequest(t *testing.T) Request {
	t.Helper()
	recipient, err := ParseRecipient("+8613812345678")
	if err != nil {
		t.Fatal(err)
	}
	return Request{Recipient: recipient, Message: TemplateMessage{
		TemplateID: "template-1",
		Params:     []TemplateParam{{Name: "code", Value: "123456"}},
	}}
}

func TestRequestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
		want   string
	}{
		{"zero recipient", func(r *Request) { r.Recipient = Recipient{} }, "sms: recipient must be E.164"},
		{"blank template", func(r *Request) { r.Message.TemplateID = " " }, "sms: template ID is required"},
		{"blank parameter name", func(r *Request) { r.Message.Params[0].Name = " " }, "sms: template parameter name is required"},
		{"duplicate parameter", func(r *Request) { r.Message.Params = append(r.Message.Params, TemplateParam{Name: "code"}) }, "sms: template parameter names must be unique"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest(t)
			tt.mutate(&req)
			err := req.Validate()
			if err == nil {
				t.Fatal("Validate succeeded")
			}
			if _, ok := failure.From(err); ok {
				t.Fatalf("validation returned Failure: %v", err)
			}
			if got := err.Error(); got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequestValidateAllowsNoParamsAndEmptyValues(t *testing.T) {
	req := validRequest(t)
	req.Message.Params = nil
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	req.Message.Params = []TemplateParam{{Name: "optional", Value: ""}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}
