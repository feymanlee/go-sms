//go:build integration

package tencent

import (
	"testing"

	"github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
	v := integrationtest.Env(t, "GO_SMS_TEST_RECIPIENT", "GO_SMS_TEST_PARAM_NAME", "GO_SMS_TEST_PARAM_VALUE", "TENCENT_SECRET_ID", "TENCENT_SECRET_KEY", "TENCENT_SMS_APP_ID", "TENCENT_REGION", "TENCENT_TEMPLATE_ID", "TENCENT_SIGNATURE_REF")
	provider, err := New(Config{SecretID: v["TENCENT_SECRET_ID"], SecretKey: v["TENCENT_SECRET_KEY"], SMSAppID: v["TENCENT_SMS_APP_ID"], Region: v["TENCENT_REGION"]})
	if err != nil {
		t.Fatal(err)
	}
	integrationtest.Send(t, provider, v, v["TENCENT_TEMPLATE_ID"], v["TENCENT_SIGNATURE_REF"])
}
