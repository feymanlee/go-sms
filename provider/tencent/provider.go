package tencent

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

type apiClient interface {
	SendSmsWithContext(context.Context, *tc.SendSmsRequest) (*tc.SendSmsResponse, error)
}

type Provider struct {
	client           apiClient
	appID            string
	defaultSignature string
}

var _ sms.Sender = (*Provider)(nil)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.SecretID) == "" {
		return nil, errors.New("tencent: SecretID is required")
	}
	if strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("tencent: SecretKey is required")
	}
	if strings.TrimSpace(config.SMSAppID) == "" {
		return nil, errors.New("tencent: SMSAppID is required")
	}
	if strings.TrimSpace(config.Region) == "" {
		return nil, errors.New("tencent: Region is required")
	}
	if tccommon.DefaultHttpClient != nil {
		return nil, errors.New("tencent: tccommon.DefaultHttpClient must be nil; use WithHTTPClient for provider-specific HTTP configuration")
	}

	settings := options{}
	for _, option := range opts {
		if option != nil {
			option(&settings)
		}
	}
	client := settings.client
	if client == nil {
		client = providerutil.NewHTTPClient()
	}

	clientProfile := profile.NewClientProfile()
	clientProfile.NetworkFailureMaxRetries = 0
	clientProfile.RateLimitExceededMaxRetries = 0
	clientProfile.HttpProfile.ReqTimeout = requestTimeout(client)
	if settings.endpoint != "" {
		clientProfile.HttpProfile.Endpoint = settings.endpoint
	}

	credential := tccommon.NewCredential(config.SecretID, config.SecretKey)
	sdkClient, err := tc.NewClient(credential, config.Region, clientProfile)
	if err != nil {
		return nil, err
	}
	sdkClient.WithHttpTransport(client.Transport)
	return &Provider{
		client:           sdkClient,
		appID:            config.SMSAppID,
		defaultSignature: config.DefaultSignatureRef,
	}, nil
}

func requestTimeout(client *http.Client) int {
	if client.Timeout == 0 {
		return 10
	}
	seconds := int(math.Ceil(client.Timeout.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signature, err := providerutil.Prepare(ctx, "tencent", req, p.defaultSignature, true)
	if err != nil {
		return sms.Submission{}, err
	}

	params := make([]*string, len(req.Message.Params))
	for i, param := range req.Message.Params {
		value := param.Value
		params[i] = &value
	}
	request := tc.NewSendSmsRequest()
	request.PhoneNumberSet = []*string{tccommon.StringPtr(req.Recipient.String())}
	request.SmsSdkAppId = tccommon.StringPtr(p.appID)
	request.TemplateId = tccommon.StringPtr(req.Message.TemplateID)
	request.SignName = tccommon.StringPtr(signature)
	request.TemplateParamSet = params

	response, err := p.client.SendSmsWithContext(ctx, request)
	if err != nil {
		return sms.Submission{}, classifyError(ctx, err, req.Recipient)
	}
	if response == nil || response.Response == nil || len(response.Response.SendStatusSet) != 1 || response.Response.SendStatusSet[0] == nil || stringValue(response.Response.SendStatusSet[0].Code) == "" {
		return sms.Submission{}, internalError("malformed response", "", nil)
	}

	status := response.Response.SendStatusSet[0]
	requestID := stringValue(response.Response.RequestId)
	code := stringValue(status.Code)
	if code != "Ok" {
		return sms.Submission{}, &sms.SendError{
			Kind:      classifyStatusCode(code),
			Provider:  "tencent",
			Code:      code,
			Message:   providerutil.Sanitize(stringValue(status.Message), req.Recipient),
			RequestID: requestID,
		}
	}
	var metadata map[string]string
	if status.Fee != nil {
		metadata = map[string]string{"fee": strconv.FormatUint(*status.Fee, 10)}
	}

	return sms.Submission{
		Provider:  "tencent",
		MessageID: stringValue(status.SerialNo),
		RequestID: requestID,
		Metadata:  metadata,
	}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
