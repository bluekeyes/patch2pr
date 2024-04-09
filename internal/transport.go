package internal

import "net/http"

type tokenRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (trt *tokenRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	rCopy := r.Clone(r.Context())
	rCopy.Header.Set("Authorization", "Bearer "+trt.token)
	return trt.base.RoundTrip(rCopy)
}

func NewTokenClient(token string) *http.Client {
	return &http.Client{
		Transport: &tokenRoundTripper{
			base:  http.DefaultTransport,
			token: token,
		},
	}
}
