package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	laiclient "github.com/rossigee/localai-client/client"
)

// RawClient makes authenticated JSON calls to LocalAI endpoints that the
// localai-client library does not expose (project-layer skills and moving an
// agent into a project). It uses the same transport as the typed client: TLS
// verification is never skipped and the configured root CA is honoured.
type RawClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewRawClient builds a RawClient from the ProviderConfig-derived config.
func NewRawClient(cfg laiclient.Config) (*RawClient, error) {
	h, err := NewLocalAIHTTPClient(context.Background(), cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build raw LocalAI HTTP client")
	}
	return &RawClient{BaseURL: strings.TrimRight(cfg.BaseURL, "/"), APIKey: cfg.APIKey, HTTP: h}, nil
}

// Do sends a JSON request and decodes a JSON response when out is non-nil.
func (r *RawClient) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return errors.Wrap(err, "cannot encode request body")
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.APIKey)
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return errors.Wrap(readErr, "cannot read response")
	}
	if closeErr != nil {
		return errors.Wrap(closeErr, "cannot close response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &laiclient.APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.Wrap(err, "cannot decode response")
		}
	}
	return nil
}
