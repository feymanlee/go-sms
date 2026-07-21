package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	ali "github.com/alibabacloud-go/dysmsapi-20170525/v5/client"
	"github.com/alibabacloud-go/tea/dara"
	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

type apiClient interface {
	SendSmsWithContext(context.Context, *ali.SendSmsRequest, *dara.RuntimeOptions) (*ali.SendSmsResponse, error)
}

const sdkUserAgent = "go-sms"

type Provider struct {
	client           apiClient
	runtime          *dara.RuntimeOptions
	defaultSignature string
	failures         failure.Factory
}

type daraHTTPClient struct {
	client *http.Client
}

func (c daraHTTPClient) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
	return c.client.Do(req)
}

var (
	_ sms.Sender      = (*Provider)(nil)
	_ dara.HttpClient = daraHTTPClient{}
)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.AccessKeyID) == "" {
		return nil, errors.New("aliyun: AccessKeyID is required")
	}
	if strings.TrimSpace(config.AccessKeySecret) == "" {
		return nil, errors.New("aliyun: AccessKeySecret is required")
	}
	if strings.TrimSpace(config.Region) == "" {
		return nil, errors.New("aliyun: Region is required")
	}
	failures, err := failure.NewFactory("aliyun")
	if err != nil {
		return nil, err
	}

	settings := options{}
	for _, option := range opts {
		if option != nil {
			option(&settings)
		}
	}
	httpClient := settings.client
	if httpClient == nil {
		httpClient = providerutil.NewHTTPClient()
	}
	httpClient = providerutil.NoRedirectClient(httpClient)

	sdkConfig := &openapiutil.Config{
		AccessKeyId:     dara.String(config.AccessKeyID),
		AccessKeySecret: dara.String(config.AccessKeySecret),
		RegionId:        dara.String(config.Region),
		UserAgent:       dara.String(sdkUserAgent),
		HttpClient:      daraHTTPClient{client: httpClient},
	}
	if settings.endpoint != "" {
		sdkConfig.Endpoint = dara.String(settings.endpoint)
	}
	sdkClient, err := ali.NewClient(sdkConfig)
	if err != nil {
		return nil, err
	}
	autoretry := false
	maxAttempts := 1
	return &Provider{
		client: sdkClient,
		runtime: &dara.RuntimeOptions{
			Autoretry:   &autoretry,
			MaxAttempts: &maxAttempts,
		},
		defaultSignature: config.DefaultSignatureRef,
		failures:         failures,
	}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signature, err := providerutil.Prepare(ctx, req, p.defaultSignature, true)
	if err != nil {
		return sms.Submission{}, err
	}
	phoneNumber, err := providerutil.ChinaNational(req.Recipient.String())
	if err != nil {
		return sms.Submission{}, errors.New("aliyun: only supports +86 recipients")
	}

	params := make(map[string]string, len(req.Message.Params))
	for _, param := range req.Message.Params {
		params[param.Name] = param.Value
	}
	templateParam, err := json.Marshal(params)
	if err != nil {
		return sms.Submission{}, errors.New("aliyun: cannot encode template parameters")
	}
	request := &ali.SendSmsRequest{
		PhoneNumbers:  dara.String(phoneNumber),
		SignName:      dara.String(signature),
		TemplateCode:  dara.String(req.Message.TemplateID),
		TemplateParam: dara.String(string(templateParam)),
	}

	response, err := p.client.SendSmsWithContext(ctx, request, p.runtime)
	if err != nil {
		return sms.Submission{}, classifyError(ctx, p.failures, err)
	}
	requestID := ""
	if response != nil && response.Body != nil {
		requestID = dara.StringValue(response.Body.RequestId)
	}
	if response == nil || response.Body == nil || dara.StringValue(response.Body.Code) == "" {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{RequestID: requestID}, ctx.Err())
	}

	code := dara.StringValue(response.Body.Code)
	if code != "OK" {
		return sms.Submission{}, p.failures.Decision(classifyBodyCode(code), failure.Diagnostic{Code: code, RequestID: requestID})
	}
	return sms.Submission{
		Provider:  "aliyun",
		MessageID: dara.StringValue(response.Body.BizId),
		RequestID: requestID,
	}, nil
}
