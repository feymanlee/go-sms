package sms

import "context"

type Sender interface {
	Send(context.Context, Request) (Submission, error)
}

type Request struct {
	Recipient    Recipient
	Message      TemplateMessage
	SignatureRef string
}

type TemplateMessage struct {
	TemplateID string
	Params     []TemplateParam
}

type TemplateParam struct {
	Name  string
	Value string
}

type Submission struct {
	Provider  string
	MessageID string
	RequestID string
	Metadata  map[string]string
}
