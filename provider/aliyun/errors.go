package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

const (
	providerErrorMessage = "aliyun provider request failed"
	sdkErrorMessage      = "aliyun SDK request failed"
)

func classifyBodyCode(code string) sms.ErrorKind {
	switch code {
	case "isv.BUSINESS_LIMIT_CONTROL", "isv.DAY_LIMIT_CONTROL":
		return sms.KindRateLimited
	case "InvalidAccessKeyId.NotFound", "SignatureDoesNotMatch":
		return sms.KindAuthentication
	}
	if isUnavailableCode(code) {
		return sms.KindUnavailable
	}
	return sms.KindRejected
}

func classifyError(ctx context.Context, err error, recipient sms.Recipient) error {
	code, requestID, status, sdkError := sdkErrorDetails(err)
	if sdkError {
		kind := sms.KindInternal
		switch {
		case status == http.StatusTooManyRequests:
			kind = sms.KindRateLimited
		case status == http.StatusUnauthorized, status == http.StatusForbidden,
			code == "InvalidAccessKeyId.NotFound", code == "SignatureDoesNotMatch":
			kind = sms.KindAuthentication
		case status >= http.StatusInternalServerError, isUnavailableCode(code):
			kind = sms.KindUnavailable
		}
		cause := err
		if contextErr := ctx.Err(); contextErr != nil {
			kind = sms.KindUnknownOutcome
			cause = errors.Join(err, contextErr)
		}
		return &sms.SendError{
			Kind:      kind,
			Provider:  "aliyun",
			Code:      code,
			Message:   sdkErrorMessage,
			RequestID: requestID,
			Cause:     providerutil.OpaqueCause(cause),
		}
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isNetworkError(err) {
		return providerutil.UnknownOutcome("aliyun", recipient, err)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return providerutil.UnknownOutcome("aliyun", recipient, errors.Join(err, contextErr))
	}

	return internalError(sdkErrorMessage, "", err)
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

func isNetworkError(err error) bool {
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	var urlError *url.Error
	return errors.As(err, &urlError)
}

func internalError(message, requestID string, cause error) error {
	return &sms.SendError{
		Kind:      sms.KindInternal,
		Provider:  "aliyun",
		Message:   message,
		RequestID: requestID,
		Cause:     providerutil.OpaqueCause(cause),
	}
}
