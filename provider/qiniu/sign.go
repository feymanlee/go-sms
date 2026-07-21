package qiniu

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
)

func authorization(req *http.Request, accessKey, secretKey string, body []byte) string {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	canonical := req.Method + " " + req.URL.EscapedPath()
	if req.URL.RawQuery != "" {
		canonical += "?" + req.URL.RawQuery
	}
	canonical += "\nHost: " + host
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		canonical += "\nContent-Type: " + contentType
	}
	canonical += "\n\n" + string(body)

	mac := hmac.New(sha1.New, []byte(secretKey))
	_, _ = mac.Write([]byte(canonical))
	return "Qiniu " + accessKey + ":" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
}
