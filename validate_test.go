package sms

import (
	"errors"
	"testing"
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
	}{
		{"zero recipient", func(r *Request) { r.Recipient = Recipient{} }},
		{"blank template", func(r *Request) { r.Message.TemplateID = " " }},
		{"blank parameter name", func(r *Request) { r.Message.Params[0].Name = " " }},
		{"duplicate parameter", func(r *Request) { r.Message.Params = append(r.Message.Params, TemplateParam{Name: "code"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest(t)
			tt.mutate(&req)
			err := req.Validate()
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v", err)
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
