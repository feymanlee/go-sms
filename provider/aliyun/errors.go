package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/feymanlee/go-sms/failure"
)

func classifyBodyCode(code string) failure.Category {
	if category, ok := classifyKnownCode(code); ok {
		return category
	}
	return failure.Rejected
}

func classifyError(ctx context.Context, failures failure.Factory, err error) error {
	code, requestID, status, sdkError := sdkErrorDetails(err)
	if sdkError {
		diagnostic := failure.Diagnostic{Code: code, RequestID: requestID}
		if category, ok := classifySDKDecision(status, code); ok {
			return failures.Decision(category, diagnostic)
		}
		return failures.Unknown(diagnostic, errors.Join(err, ctx.Err()))
	}
	return failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))
}

func classifySDKDecision(status int, code string) (failure.Category, bool) {
	switch {
	case status == http.StatusTooManyRequests:
		return failure.RateLimited, true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure.Authentication, true
	case status >= 500 && status <= 599:
		return failure.Unavailable, true
	}
	if category, ok := classifyKnownCode(code); ok {
		return category, true
	}
	if status >= 400 && status <= 499 {
		return failure.Rejected, true
	}
	return "", false
}

func classifyKnownCode(code string) (failure.Category, bool) {
	switch code {
	case "InvalidAccessKeyId.NotFound", "SignatureDoesNotMatch":
		return failure.Authentication, true
	case "isv.BUSINESS_LIMIT_CONTROL", "isv.DAY_LIMIT_CONTROL":
		return failure.RateLimited, true
	}
	if isUnavailableCode(code) {
		return failure.Unavailable, true
	}
	return "", false
}

func sdkErrorDetails(err error) (code, requestID string, status int, ok bool) {
	var teaError *tea.SDKError
	if errors.As(err, &teaError) {
		return tea.StringValue(teaError.Code), requestIDFromData(tea.StringValue(teaError.Data)), tea.IntValue(teaError.StatusCode), true
	}
	var daraError *dara.SDKError
	if errors.As(err, &daraError) {
		return dara.StringValue(daraError.Code), requestIDFromData(dara.StringValue(daraError.Data)), dara.IntValue(daraError.StatusCode), true
	}
	return "", "", 0, false
}

func requestIDFromData(data string) string {
	var body struct {
		RequestID      string `json:"RequestId"`
		RequestIDLower string `json:"requestId"`
	}
	if json.Unmarshal([]byte(data), &body) != nil {
		return ""
	}
	if body.RequestID != "" {
		return body.RequestID
	}
	return body.RequestIDLower
}

func isUnavailableCode(code string) bool {
	return code == "ServiceUnavailable" || strings.HasPrefix(code, "ServiceUnavailable.") ||
		code == "InternalError" || strings.HasPrefix(code, "InternalError.") || code == "isp.SYSTEM_ERROR"
}
