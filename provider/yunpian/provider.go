package yunpian

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

const defaultEndpoint = "https://sms.yunpian.com/v2/sms/tpl_single_send.json"

type Provider struct {
	client   *http.Client
	endpoint string
	apiKey   string
	failures failure.Factory
}

var _ sms.Sender = (*Provider)(nil)

func New(config Config, opts ...Option) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("yunpian: APIKey is required")
	}
	failures, err := failure.NewFactory("yunpian")
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

	return &Provider{client: &client, endpoint: settings.endpoint, apiKey: config.APIKey, failures: failures}, nil
}

func (p *Provider) Send(ctx context.Context, req sms.Request) (sms.Submission, error) {
	if _, err := providerutil.Prepare(ctx, req, "", false); err != nil {
		return sms.Submission{}, err
	}
	for _, param := range req.Message.Params {
		if strings.TrimSpace(param.Value) == "" {
			return sms.Submission{}, errors.New("yunpian: template parameter value is required")
		}
	}

	form := url.Values{
		"apikey":    {p.apiKey},
		"mobile":    {req.Recipient.String()},
		"tpl_id":    {req.Message.TemplateID},
		"tpl_value": {templateValue(req.Message.Params)},
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return sms.Submission{}, errors.New("yunpian: cannot create request")
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

	var body yunpianResponse
	if err := decodeResponse(response.Body, &body); err != nil {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	if body.Code == "" {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}
	if body.Code != "0" {
		return sms.Submission{}, p.failures.Decision(failure.Rejected, failure.Diagnostic{
			Code: body.Code.String(),
		})
	}
	if body.SID == "" {
		return sms.Submission{}, p.failures.Unknown(failure.Diagnostic{}, ctx.Err())
	}

	metadata := map[string]string{}
	if body.Count != "" {
		metadata["count"] = body.Count.String()
	}
	if body.Fee != "" {
		metadata["fee"] = body.Fee.String()
	}
	if body.Unit != "" {
		metadata["unit"] = body.Unit
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return sms.Submission{Provider: "yunpian", MessageID: body.SID.String(), Metadata: metadata}, nil
}

func templateValue(params []sms.TemplateParam) string {
	pairs := make([]string, len(params))
	for i, param := range params {
		pairs[i] = "#" + param.Name + "#=" + param.Value
	}
	return strings.Join(pairs, "&")
}

type yunpianResponse struct {
	Code  json.Number `json:"code"`
	Count json.Number `json:"count"`
	Fee   json.Number `json:"fee"`
	Unit  string      `json:"unit"`
	SID   json.Number `json:"sid"`
}

func decodeResponse(body io.Reader, value any) error {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("yunpian: unexpected additional JSON value")
		}
		return err
	}
	return nil
}
