package qiniu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

const defaultEndpoint = "https://sms.qiniuapi.com/v1/message"

type Provider struct {
	client           *http.Client
	endpoint         string
	accessKey        string
	secretKey        string
	defaultSignature string
}

var _ sms.Sender = (*Provider)(nil)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.AccessKey) == "" {
		return nil, errors.New("qiniu: AccessKey is required")
	}
	if strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("qiniu: SecretKey is required")
	}

	settings := options{endpoint: defaultEndpoint}
	for _, option := range opts {
		if option != nil {
			option(&settings)
		}
	}
	if settings.client == nil {
		settings.client = providerutil.NewHTTPClient()
	}
	if strings.TrimSpace(settings.endpoint) == "" {
		settings.endpoint = defaultEndpoint
	}
	client := *settings.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Provider{
		client:           &client,
		endpoint:         settings.endpoint,
		accessKey:        config.AccessKey,
		secretKey:        config.SecretKey,
		defaultSignature: config.DefaultSignatureRef,
	}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signatureRef, err := providerutil.Prepare(ctx, "qiniu", req, p.defaultSignature, true)
	if err != nil {
		return sms.Submission{}, err
	}
	mobile, err := providerutil.ChinaNational(req.Recipient.String())
	if err != nil {
		return sms.Submission{}, &sms.SendError{
			Kind:     sms.KindInvalidRequest,
			Provider: "qiniu",
			Message:  providerutil.Sanitize(err.Error(), req.Recipient),
			Cause:    err,
		}
	}

	body := struct {
		SignatureID string            `json:"signature_id"`
		TemplateID  string            `json:"template_id"`
		Mobiles     []string          `json:"mobiles"`
		Parameters  map[string]string `json:"parameters"`
	}{
		SignatureID: signatureRef,
		TemplateID:  req.Message.TemplateID,
		Mobiles:     []string{mobile},
		Parameters:  make(map[string]string, len(req.Message.Params)),
	}
	for _, param := range req.Message.Params {
		body.Parameters[param.Name] = param.Value
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return sms.Submission{}, internalError("cannot encode request", "")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return sms.Submission{}, internalError("cannot create request", "")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", authorization(httpRequest, p.accessKey, p.secretKey, payload))

	response, err := p.client.Do(httpRequest)
	if err != nil {
		cause := err
		if contextErr := ctx.Err(); contextErr != nil && !errors.Is(err, contextErr) {
			cause = errors.Join(err, contextErr)
		}
		return sms.Submission{}, providerutil.UnknownOutcome("qiniu", req.Recipient, cause)
	}
	if response == nil {
		return sms.Submission{}, internalError("malformed response", "")
	}
	defer response.Body.Close()

	requestID := response.Header.Get("X-Reqid")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var responseBody struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
			return sms.Submission{}, providerError(response.StatusCode, "", requestID, req.Recipient)
		}
		return sms.Submission{}, providerError(response.StatusCode, responseBody.Error, requestID, req.Recipient)
	}

	var responseBody struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
		return sms.Submission{}, internalError("cannot decode response", requestID)
	}
	if responseBody.JobID == "" {
		return sms.Submission{}, internalError("malformed response", requestID)
	}
	return sms.Submission{Provider: "qiniu", MessageID: responseBody.JobID, RequestID: requestID}, nil
}
