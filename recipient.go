package sms

import (
	"errors"
	"regexp"
)

var e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{0,14}$`)

type Recipient struct {
	e164 string
}

func ParseRecipient(value string) (Recipient, error) {
	if !e164Pattern.MatchString(value) {
		return Recipient{}, errors.New("sms: invalid E.164 recipient")
	}
	return Recipient{e164: value}, nil
}

func (r Recipient) String() string { return r.e164 }

func (r Recipient) valid() bool { return e164Pattern.MatchString(r.e164) }
