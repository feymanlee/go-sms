package sms

import "strings"

func (r Request) Validate() error {
	if !r.Recipient.valid() {
		return invalidRequest("recipient must be E.164")
	}
	if strings.TrimSpace(r.Message.TemplateID) == "" {
		return invalidRequest("template ID is required")
	}
	seen := make(map[string]struct{}, len(r.Message.Params))
	for _, param := range r.Message.Params {
		if strings.TrimSpace(param.Name) == "" {
			return invalidRequest("template parameter name is required")
		}
		if _, exists := seen[param.Name]; exists {
			return invalidRequest("template parameter names must be unique")
		}
		seen[param.Name] = struct{}{}
	}
	return nil
}

func invalidRequest(message string) error {
	return &SendError{Kind: KindInvalidRequest, Message: message}
}
