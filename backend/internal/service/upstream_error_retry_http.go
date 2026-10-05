package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

const upstreamErrorRetryBodyLimit = 64 << 10

// DoWithConfiguredUpstreamRetry replays only reproducible POST responses. It
// deliberately leaves transport errors and 429 to the existing failover and
// rate-limit policies.
func DoWithConfiguredUpstreamRetry(req *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if send == nil {
		return nil, errors.New("upstream retry sender is nil")
	}
	if req == nil {
		return send(req)
	}
	state := upstreamErrorRetryFromContext(req.Context())
	if state == nil || req.Method != http.MethodPost || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return send(req)
	}
	original := req.Clone(req.Context())
	resp, err := send(req)
	for {
		if err != nil || resp == nil || resp.StatusCode < 400 || resp.StatusCode > 599 || resp.StatusCode == http.StatusTooManyRequests {
			return resp, err
		}
		body := []byte(nil)
		var readErr error
		if resp.Body != nil {
			originalBody := resp.Body
			body, readErr = io.ReadAll(io.LimitReader(originalBody, upstreamErrorRetryBodyLimit+1))
			resp.Body = &upstreamRetryPrefixBody{ReadCloser: originalBody, reader: io.MultiReader(bytes.NewReader(body), &upstreamRetryReadError{err: readErr}, originalBody)}
		}
		if readErr != nil || len(body) > upstreamErrorRetryBodyLimit {
			return resp, nil
		}
		delay, claimed := state.claim(resp.StatusCode, body)
		if !claimed {
			return resp, nil
		}
		if resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				return resp, nil
			}
		}
		next := original.Clone(original.Context())
		if original.GetBody != nil {
			next.Body, err = original.GetBody()
			if err != nil {
				return resp, nil
			}
		}
		if err := state.wait(req.Context(), delay); err != nil {
			if next.Body != nil {
				_ = next.Body.Close()
			}
			return nil, err
		}
		resp, err = send(next)
	}
}

type upstreamRetryPrefixBody struct {
	io.ReadCloser
	reader io.Reader
}

func (b *upstreamRetryPrefixBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

type upstreamRetryReadError struct{ err error }

func (r *upstreamRetryReadError) Read([]byte) (int, error) {
	if r.err != nil {
		err := r.err
		r.err = nil
		return 0, err
	}
	return 0, io.EOF
}
