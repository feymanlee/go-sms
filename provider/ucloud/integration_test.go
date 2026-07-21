//go:build integration

package ucloud

import (
	"testing"

	"github.com/feymanlee/go-sms/internal/integrationtest"
)

func TestIntegrationSend(t *testing.T) {
	v := integrationtest.Env(t, "UCLOUD_PUBLIC_KEY", "UCLOUD_PRIVATE_KEY", "UCLOUD_PROJECT_ID", "UCLOUD_REGION", "UCLOUD_TEMPLATE_ID", "UCLOUD_SIGNATURE_REF")
	provider, err := New(Config{PublicKey: v["UCLOUD_PUBLIC_KEY"], PrivateKey: v["UCLOUD_PRIVATE_KEY"], ProjectID: v["UCLOUD_PROJECT_ID"], Region: v["UCLOUD_REGION"]})
	if err != nil {
		t.Fatal(err)
	}
	integrationtest.Send(t, provider, v["UCLOUD_TEMPLATE_ID"], v["UCLOUD_SIGNATURE_REF"])
}
