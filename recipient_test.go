package sms

import "testing"

func TestParseRecipient(t *testing.T) {
	t.Parallel()
	valid := []string{"+8613812345678", "+12025550123", "+93701234567"}
	for _, input := range valid {
		recipient, err := ParseRecipient(input)
		if err != nil || recipient.String() != input {
			t.Fatalf("ParseRecipient(%q) = %q, %v", input, recipient.String(), err)
		}
	}

	invalid := []string{"", "13812345678", "+", "+0123", "+86 13812345678", "+1234567890123456"}
	for _, input := range invalid {
		if _, err := ParseRecipient(input); err == nil {
			t.Fatalf("ParseRecipient(%q) succeeded", input)
		}
	}
}
