package qiniu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"testing"
)

func TestAuthorizationUsesQiniuCanonicalRequest(t *testing.T) {
	body := []byte(`{"signature_id":"sig-1"}`)
	req, err := http.NewRequest(http.MethodPost, "https://sms.qiniuapi.com/v1/message", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	const canonical = "POST /v1/message\nHost: sms.qiniuapi.com\nContent-Type: application/json\n\n{\"signature_id\":\"sig-1\"}"
	mac := hmac.New(sha1.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(canonical))
	want := "Qiniu access-key:" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if want != "Qiniu access-key:G60at940HrcM9TDxo_b2z-oEUAg=" {
		t.Fatalf("independent authorization = %q", want)
	}
	if got := authorization(req, "access-key", "secret-key", body); got != want {
		t.Fatalf("authorization = %q, want %q", got, want)
	}
}
