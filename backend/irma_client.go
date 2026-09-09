package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/privacybydesign/irmago/irma"
	"github.com/privacybydesign/irmago/irma/server"
)

// IrmaClient talks to the requestor API of the IRMA server. The issuer uses it
// to run the identity disclosure session itself, so that the disclosed
// attributes can be compared with the extracts before anything is issued.
type IrmaClient interface {
	// StartSession posts a signed session request JWT and returns the session
	// package (session pointer for the app, requestor token for us).
	StartSession(ctx context.Context, signedJwt string) (*server.SessionPackage, error)
	// GetSessionResult fetches the result of a session by requestor token.
	GetSessionResult(ctx context.Context, token irma.RequestorToken) (*server.SessionResult, error)
}

// HttpIrmaClient is the production IrmaClient.
type HttpIrmaClient struct {
	baseURL string
	client  *http.Client
}

func NewHttpIrmaClient(baseURL string, timeout time.Duration) *HttpIrmaClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &HttpIrmaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *HttpIrmaClient) StartSession(ctx context.Context, signedJwt string) (*server.SessionPackage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/session", strings.NewReader(signedJwt))
	if err != nil {
		return nil, fmt.Errorf("failed to create session request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var pkg server.SessionPackage
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("failed to decode session package: %w", err)
	}
	if pkg.SessionPtr == nil || pkg.Token == "" {
		return nil, fmt.Errorf("incomplete session package from irma server: %s", bytes.TrimSpace(body))
	}
	return &pkg, nil
}

func (c *HttpIrmaClient) GetSessionResult(ctx context.Context, token irma.RequestorToken) (*server.SessionResult, error) {
	url := fmt.Sprintf("%s/session/%s/result", c.baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create result request: %w", err)
	}
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var result server.SessionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode session result: %w", err)
	}
	return &result, nil
}

func (c *HttpIrmaClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("irma server request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read irma server response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("irma server returned status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return body, nil
}
