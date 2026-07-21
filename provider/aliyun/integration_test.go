//go:build integration

package aliyun

import (
	"testing"

	"github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
	v := integrationtest.Env(t, "ALIYUN_ACCESS_KEY_ID", "ALIYUN_ACCESS_KEY_SECRET", "ALIYUN_REGION", "ALIYUN_TEMPLATE_ID", "ALIYUN_SIGNATURE_REF")
	provider, err := New(Config{AccessKeyID: v["ALIYUN_ACCESS_KEY_ID"], AccessKeySecret: v["ALIYUN_ACCESS_KEY_SECRET"], Region: v["ALIYUN_REGION"]})
	if err != nil {
		t.Fatal(err)
	}
	integrationtest.Send(t, provider, v["ALIYUN_TEMPLATE_ID"], v["ALIYUN_SIGNATURE_REF"])
}
