package ucloud

import "net/http"

type Config struct {
	PublicKey           string
	PrivateKey          string
	ProjectID           string
	Region              string
	DefaultSignatureRef string
}

type options struct {
	client   *http.Client
	endpoint string
}

type Option func(*options)

func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.client = client }
}

func WithEndpoint(endpoint string) Option {
	return func(o *options) { o.endpoint = endpoint }
}
