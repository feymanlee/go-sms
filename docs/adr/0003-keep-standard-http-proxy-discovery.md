# Keep standard HTTP proxy discovery

Default Provider HTTP clients retain Go's standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` discovery so deployments can use conventional enterprise proxies without custom library configuration. The prohibition on environment-based configuration applies to credentials and Provider business settings; callers that need a different or fully deterministic transport policy can inject their own `http.Client`.
