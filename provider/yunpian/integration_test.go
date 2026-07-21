//go:build integration

package yunpian

import (
	"testing"

	"github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
	v := integrationtest.Env(t, "GO_SMS_TEST_RECIPIENT", "GO_SMS_TEST_PARAM_NAME", "GO_SMS_TEST_PARAM_VALUE", "YUNPIAN_API_KEY", "YUNPIAN_TEMPLATE_ID")
	provider, err := New(Config{APIKey: v["YUNPIAN_API_KEY"]})
	if err != nil {
		t.Fatal(err)
	}
	integrationtest.Send(t, provider, v, v["YUNPIAN_TEMPLATE_ID"], "")
}
