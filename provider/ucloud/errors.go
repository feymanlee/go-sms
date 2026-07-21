package ucloud

import (
	"github.com/feymanlee/go-sms/failure"
)

func httpErrorCategory(status int) failure.Category {
	switch {
	case status == 401 || status == 403:
		return failure.Authentication
	case status == 429:
		return failure.RateLimited
	case status >= 500 && status <= 599:
		return failure.Unavailable
	default:
		return failure.Rejected
	}
}
