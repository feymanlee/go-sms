package ucloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/internal/providerutil"
)

const defaultEndpoint = "https://api.ucloud.cn"

type Provider struct {
	client           *http.Client
	endpoint         string
	publicKey        string
	privateKey       string
	projectID        string
	region           string
	defaultSignature string
}

var _ sms.Sender = (*Provider)(nil)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.PublicKey) == "" {
		return nil, errors.New("ucloud: PublicKey is required")
	}
	if strings.TrimSpace(config.PrivateKey) == "" {
		return nil, errors.New("ucloud: PrivateKey is required")
	}
	if strings.TrimSpace(config.ProjectID) == "" {
		return nil, errors.New("ucloud: ProjectID is required")
	}
	if strings.TrimSpace(config.Region) == "" {
		return nil, errors.New("ucloud: Region is required")
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

	return &Provider{
		client:           settings.client,
		endpoint:         strings.TrimRight(settings.endpoint, "/"),
		publicKey:        config.PublicKey,
		privateKey:       config.PrivateKey,
		projectID:        config.ProjectID,
		region:           config.Region,
		defaultSignature: config.DefaultSignatureRef,
	}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signatureRef, err := providerutil.Prepare(ctx, "ucloud", req, p.defaultSignature, false)
	if err != nil {
		return sms.Submission{}, err
	}
	phoneNumber, err := providerutil.UCloudNumber(req.Recipient.String())
	if err != nil {
		return sms.Submission{}, &sms.SendError{
			Kind:     sms.KindInvalidRequest,
			Provider: "ucloud",
			Message:  providerutil.Sanitize(err.Error(), req.Recipient),
			Cause:    err,
		}
	}

	values := map[string]string{
		"Action":         "SendUSMSMessage",
		"PhoneNumbers.0": phoneNumber,
		"TemplateId":     req.Message.TemplateID,
		"SigContent":     signatureRef,
		"ProjectId":      p.projectID,
		"Region":         p.region,
		"PublicKey":      p.publicKey,
	}
	for i, param := range req.Message.Params {
		values["TemplateParams."+strconv.Itoa(i)] = param.Value
	}
	values["Signature"] = sign(values, p.privateKey)

	form := make(url.Values, len(values))
	for key, value := range values {
		form.Set(key, value)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return sms.Submission{}, internalError("cannot create request", err)
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := p.client.Do(httpRequest)
	if err != nil {
		cause := err
		if contextErr := ctx.Err(); contextErr != nil && !errors.Is(err, contextErr) {
			cause = errors.Join(err, contextErr)
		}
		return sms.Submission{}, providerutil.UnknownOutcome("ucloud", req.Recipient, cause)
	}
	if response == nil {
		return sms.Submission{}, internalError("malformed response", nil)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return sms.Submission{}, &sms.SendError{Kind: httpErrorKind(response.StatusCode), Provider: "ucloud"}
	}

	var body struct {
		RetCode   int    `json:"RetCode"`
		Message   string `json:"Message"`
		SessionNo string `json:"SessionNo"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return sms.Submission{}, internalError("cannot decode response", nil)
	}
	if body.RetCode != 0 {
		return sms.Submission{}, providerRejection(body.RetCode, body.Message, req.Recipient)
	}
	if body.SessionNo == "" {
		return sms.Submission{}, internalError("malformed response", nil)
	}

	return sms.Submission{Provider: "ucloud", MessageID: body.SessionNo}, nil
}
