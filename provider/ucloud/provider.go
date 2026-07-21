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
	"github.com/feymanlee/go-sms/failure"
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
	failures         failure.Factory
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
	failures, err := failure.NewFactory("ucloud")
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
	settings.client = providerutil.NoRedirectClient(settings.client)
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
		failures:         failures,
	}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	signatureRef, err := providerutil.Prepare(ctx, req, p.defaultSignature, false)
	if err != nil {
		return sms.Submission{}, err
	}
	phoneNumber, err := providerutil.UCloudNumber(req.Recipient.String())
	if err != nil {
		return sms.Submission{}, errors.New("ucloud: cannot convert recipient")
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
		return sms.Submission{}, errors.New("ucloud: cannot create request")
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := p.client.Do(httpRequest)
	if err != nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, errors.Join(err, ctx.Err()))
	}
	if response == nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return sms.Submission{}, p.failures.Decision(httpErrorCategory(response.StatusCode), failure.Diagnostic{
			Code: strconv.Itoa(response.StatusCode),
		})
	}

	var body struct {
		RetCode   *int   `json:"RetCode"`
		Message   string `json:"Message"`
		SessionNo string `json:"SessionNo"`
	}
	if err := decodeResponse(response.Body, &body); err != nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	if body.RetCode == nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	if *body.RetCode != 0 {
		return sms.Submission{}, p.failures.Decision(failure.Rejected, failure.Diagnostic{
			Code: strconv.Itoa(*body.RetCode),
		})
	}
	if body.SessionNo == "" {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}

	return sms.Submission{Provider: "ucloud", MessageID: body.SessionNo}, nil
}

func decodeResponse(body io.Reader, value any) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("ucloud: unexpected additional JSON value")
		}
		return err
	}
	return nil
}
