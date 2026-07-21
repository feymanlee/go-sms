//go:build integration

package qiniu

import (
	"testing"

	"github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
	v := integrationtest.Env(t, "GO_SMS_TEST_RECIPIENT", "GO_SMS_TEST_PARAM_NAME", "GO_SMS_TEST_PARAM_VALUE", "QINIU_ACCESS_KEY", "QINIU_SECRET_KEY", "QINIU_TEMPLATE_ID", "QINIU_SIGNATURE_REF")
	provider, err := New(Config{AccessKey: v["QINIU_ACCESS_KEY"], SecretKey: v["QINIU_SECRET_KEY"]})
	if err != nil {
		t.Fatal(err)
	}
	integrationtest.Send(t, provider, v, v["QINIU_TEMPLATE_ID"], v["QINIU_SIGNATURE_REF"])
}
