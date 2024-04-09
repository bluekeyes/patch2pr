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

// NewTokenClient returns an [http.Client] that sets the bearer token in the
// Authorization header of all requests.
func NewTokenClient(token string) *http.Client {
	return &http.Client{
		Transport: &tokenRoundTripper{
			base:  http.DefaultTransport,
			token: token,
		},
	}
}
