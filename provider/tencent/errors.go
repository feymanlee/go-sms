package tencent

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
	tcerr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func classifyError(ctx context.Context, err error, recipient sms.Recipient) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isNetworkError(err) {
		return providerutil.UnknownOutcome("tencent", recipient, err)
	}

	var native *tcerr.TencentCloudSDKError
	if errors.As(err, &native) {
		if native.Code == "ClientError.NetworkError" {
			cause := err
			if contextErr := ctx.Err(); contextErr != nil {
				cause = errors.Join(err, contextErr)
			}
			return providerutil.UnknownOutcome("tencent", recipient, cause)
		}
		return &sms.SendError{
			Kind:      classifyCode(native.Code),
			Provider:  "tencent",
			Code:      native.Code,
			Message:   providerutil.Sanitize(native.Message, recipient),
			RequestID: native.RequestId,
			Cause:     err,
		}
	}

	return internalError(providerutil.Sanitize(err.Error(), recipient), "", err)
}

func classifyCode(code string) sms.ErrorKind {
	switch {
	case strings.HasPrefix(code, "AuthFailure."), strings.HasPrefix(code, "InvalidCredential"), strings.HasPrefix(code, "UnauthorizedOperation."):
		return sms.KindAuthentication
	case strings.HasPrefix(code, "RequestLimitExceeded"), strings.HasPrefix(code, "LimitExceeded."):
		return sms.KindRateLimited
	case strings.HasPrefix(code, "InternalError"), strings.HasPrefix(code, "ResourceUnavailable."):
		return sms.KindUnavailable
	default:
		return sms.KindInternal
	}
}

func classifyStatusCode(code string) sms.ErrorKind {
	kind := classifyCode(code)
	if kind == sms.KindInternal {
		return sms.KindRejected
	}
	return kind
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
		Provider:  "tencent",
		Message:   message,
		RequestID: requestID,
		Cause:     cause,
	}
}
