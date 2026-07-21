//go:build integration

package integrationtest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
)

func Env(t *testing.T, names ...string) map[string]string {
	t.Helper()
	values := make(map[string]string, len(names))
	missing := make([]string, 0)
	for _, name := range names {
		value := os.Getenv(name)
		if value == "" {
			missing = append(missing, name)
			continue
		}
		values[name] = value
	}
	if len(missing) > 0 {
		t.Skipf("integration variables required: %s", strings.Join(missing, ", "))
	}
	return values
}

func Send(t *testing.T, sender sms.Sender, templateID, signatureRef string) {
	t.Helper()
	common := Env(t, "GO_SMS_TEST_RECIPIENT", "GO_SMS_TEST_PARAM_NAME", "GO_SMS_TEST_PARAM_VALUE")
	recipient, err := sms.ParseRecipient(common["GO_SMS_TEST_RECIPIENT"])
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	submission, err := sender.Send(ctx, sms.Request{
		Recipient:    recipient,
		SignatureRef: signatureRef,
		Message: sms.TemplateMessage{
			TemplateID: templateID,
			Params: []sms.TemplateParam{
				{
					Name:  common["GO_SMS_TEST_PARAM_NAME"],
					Value: common["GO_SMS_TEST_PARAM_VALUE"],
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf(
		"date=%s provider=%s message_id=%s request_id=%s",
		time.Now().UTC().Format(time.DateOnly),
		submission.Provider,
		submission.MessageID,
		submission.RequestID,
	)
}
