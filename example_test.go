package sms_test

import (
	"context"
	"errors"
	"log"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/provider/tencent"
)

func ExampleSender() {
	provider, err := tencent.New(tencent.Config{
		SecretID:            "example-secret-id",
		SecretKey:           "example-secret-key",
		SMSAppID:            "1400000000",
		Region:              "ap-guangzhou",
		DefaultSignatureRef: "Example",
	})
	if err != nil {
		log.Print(err)
		return
	}
	recipient, err := sms.ParseRecipient("+8613812345678")
	if err != nil {
		log.Print(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	submission, err := provider.Send(ctx, sms.Request{
		Recipient: recipient,
		Message: sms.TemplateMessage{
			TemplateID: "123456",
			Params: []sms.TemplateParam{
				{Name: "code", Value: "654321"},
			},
		},
	})
	if errors.Is(err, sms.ErrUnknownOutcome) {
		log.Print("send outcome is unknown")
		return
	}
	if err != nil {
		log.Print(err)
		return
	}
	log.Printf("accepted by %s as %s", submission.Provider, submission.MessageID)
}
