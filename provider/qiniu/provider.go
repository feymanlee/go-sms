package qiniu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

const defaultEndpoint = "https://sms.qiniuapi.com/v1/message"

type Provider struct {
	client           *http.Client
	endpoint         string
	accessKey        string
	secretKey        string
	defaultSignature string
	failures         failure.Factory
}

var _ sms.Sender = (*Provider)(nil)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.AccessKey) == "" {
		return nil, errors.New("qiniu: AccessKey is required")
	}
	if strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("qiniu: SecretKey is required")
	}
	failures, err := failure.NewFactory("qiniu")
	if err != nil {
		return nil, err
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
		failures:         failures,
	}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signatureRef, err := providerutil.Prepare(ctx, req, p.defaultSignature, true)
	if err != nil {
		return sms.Submission{}, err
	}
	mobile, err := providerutil.ChinaNational(req.Recipient.String())
	if err != nil {
		return sms.Submission{}, errors.New("qiniu: only supports +86 recipients")
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
		return sms.Submission{}, errors.New("qiniu: cannot encode request")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return sms.Submission{}, errors.New("qiniu: cannot create request")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", authorization(httpRequest, p.accessKey, p.secretKey, payload))

	response, err := p.client.Do(httpRequest)
	if err != nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))
	}
	if response == nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	defer response.Body.Close()

	requestID := response.Header.Get("X-Reqid")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return sms.Submission{}, p.failures.Decision(httpErrorCategory(response.StatusCode), failure.Diagnostic{
			Code: strconv.Itoa(response.StatusCode), RequestID: requestID,
		})
	}

	var responseBody struct {
		JobID string `json:"job_id"`
	}
	if err := decodeResponse(response.Body, &responseBody); err != nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{RequestID: requestID}, ctx.Err())
	}
	if responseBody.JobID == "" {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{RequestID: requestID}, ctx.Err())
	}
	return sms.Submission{Provider: "qiniu", MessageID: responseBody.JobID, RequestID: requestID}, nil
}

func decodeResponse(body io.Reader, value any) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("qiniu: unexpected additional JSON value")
		}
		return err
	}
	return nil
}
