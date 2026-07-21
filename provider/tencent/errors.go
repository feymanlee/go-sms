package tencent

import (
	"context"
	"errors"
	"strings"

	"github.com/feymanlee/go-sms/failure"
	tcerr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func classifyError(ctx context.Context, failures failure.Factory, err error) error {
	var native *tcerr.TencentCloudSDKError
	if errors.As(err, &native) {
		diagnostic := failure.Diagnostic{Code: native.Code, RequestID: native.RequestId}
		if native.Code != "ClientError.NetworkError" {
			if category, ok := classifyCode(native.Code); ok {
				return failures.Decision(category, diagnostic)
			}
		}
		return failures.Unknown(diagnostic, errors.Join(err, ctx.Err()))
	}
	return failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))
}

func classifyCode(code string) (failure.Category, bool) {
	switch {
	case strings.HasPrefix(code, "AuthFailure."), strings.HasPrefix(code, "InvalidCredential"), strings.HasPrefix(code, "UnauthorizedOperation."):
		return failure.Authentication, true
	case strings.HasPrefix(code, "RequestLimitExceeded"), strings.HasPrefix(code, "LimitExceeded."):
		return failure.RateLimited, true
	case strings.HasPrefix(code, "InternalError"), strings.HasPrefix(code, "ResourceUnavailable."):
		return failure.Unavailable, true
	default:
		return "", false
	}
}

func classifyStatusCode(code string) failure.Category {
	if category, ok := classifyCode(code); ok {
		return category
	}
	return failure.Rejected
}
