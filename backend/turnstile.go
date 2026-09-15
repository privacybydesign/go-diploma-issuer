package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Cloudflare Turnstile protects the upload endpoint against automated
// submissions: every upload costs up to max_files signature verifications
// (possibly with OCSP calls) and PDF parses, so it is the one surface worth
// gating. The widget on the upload page produces a single-use token that
// travels with the upload as the multipart field "cf-turnstile-response". The
// backend redeems that token at Cloudflare's siteverify endpoint (browser ->
// this backend -> Cloudflare, never from the browser directly) before it looks
// at the PDFs. Siteverify echoes the action the widget was rendered with and
// the hostname of the page, and both are checked, so a token minted on another
// site or for another purpose is useless here.
// See https://developers.cloudflare.com/turnstile/.

const (
	// TurnstileTokenField is the multipart form field that carries the token.
	TurnstileTokenField = "cf-turnstile-response"
	// TurnstileActionUpload is the action the frontend renders the widget
	// with on the upload page. Siteverify returns it and the backend requires
	// it, so the frontend and backend must agree on this value.
	TurnstileActionUpload = "diploma-upload"
	// TurnstileSecretEnv is the environment variable that, when set,
	// overrides the secret in the config file. Handy for platforms that
	// inject secrets through the environment.
	TurnstileSecretEnv = "TURNSTILE_SECRET"
	// DefaultTurnstileSiteverifyURL is Cloudflare's token redemption endpoint.
	DefaultTurnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	// A token is at most 2048 characters, per the Turnstile documentation.
	turnstileMaxTokenLength = 2048
	turnstileTimeout        = 10 * time.Second
)

// TurnstileConfig configures the Cloudflare Turnstile check on the upload
// endpoint. The check is enabled when a secret is configured (in the file or
// through the TURNSTILE_SECRET environment variable); without one the upload
// endpoint is not protected and a warning is logged at startup.
type TurnstileConfig struct {
	// Sitekey of the widget, handed to the frontend through /api/config.
	SiteKey string `json:"site_key"`
	// Secret of the widget. Prefer the TURNSTILE_SECRET environment variable
	// when the platform can inject it; the config file is the fallback.
	Secret string `json:"secret"`
	// Hostnames on which the frontend is served, e.g. ["diploma.yivi.app"].
	// Siteverify reports the hostname of the page that solved the challenge
	// and the token is rejected unless that hostname is listed here. Only
	// list localhost and 127.0.0.1 in a development configuration.
	Hostnames []string `json:"hostnames"`
	// Siteverify endpoint; leave empty for the Cloudflare endpoint. Tests
	// point this at a fake.
	SiteverifyUrl string `json:"siteverify_url,omitempty"`
}

// Enabled reports whether a secret is configured, taking the environment
// override into account.
func (c TurnstileConfig) Enabled() bool {
	return c.resolvedSecret() != ""
}

func (c TurnstileConfig) resolvedSecret() string {
	if env := strings.TrimSpace(os.Getenv(TurnstileSecretEnv)); env != "" {
		return env
	}
	return strings.TrimSpace(c.Secret)
}

// TurnstileVerifier redeems a Turnstile token. Verify returns nil when the
// token is genuine, unused, minted for the expected action on an approved
// hostname; any other outcome (including an unreachable siteverify) is an
// error and the request must be refused.
type TurnstileVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) error
}

// ErrTurnstileRejected marks a token that siteverify did not accept, or that
// was accepted for the wrong action or hostname.
var ErrTurnstileRejected = errors.New("turnstile token rejected")

// CloudflareTurnstile is the TurnstileVerifier backed by Cloudflare's
// siteverify endpoint.
type CloudflareTurnstile struct {
	secret    string
	action    string
	hostnames map[string]bool
	url       string
	client    *http.Client
}

// NewCloudflareTurnstile builds the verifier for the upload action. It fails
// when the configuration is unusable: no secret, or no hostnames to check the
// siteverify answer against (an empty allowlist would accept tokens from any
// site that embeds the widget).
func NewCloudflareTurnstile(cfg TurnstileConfig) (*CloudflareTurnstile, error) {
	secret := cfg.resolvedSecret()
	if secret == "" {
		return nil, fmt.Errorf("turnstile: no secret configured")
	}
	if strings.TrimSpace(cfg.SiteKey) == "" {
		return nil, fmt.Errorf("turnstile: site_key is required when a secret is configured")
	}
	hostnames := make(map[string]bool, len(cfg.Hostnames))
	for _, h := range cfg.Hostnames {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			hostnames[h] = true
		}
	}
	if len(hostnames) == 0 {
		return nil, fmt.Errorf("turnstile: at least one expected hostname is required")
	}
	endpoint := cfg.SiteverifyUrl
	if endpoint == "" {
		endpoint = DefaultTurnstileSiteverifyURL
	}
	return &CloudflareTurnstile{
		secret:    secret,
		action:    TurnstileActionUpload,
		hostnames: hostnames,
		url:       endpoint,
		client:    &http.Client{Timeout: turnstileTimeout},
	}, nil
}

// Hostnames lists the approved frontend hostnames, for logging.
func (t *CloudflareTurnstile) Hostnames() []string {
	out := make([]string, 0, len(t.hostnames))
	for h := range t.hostnames {
		out = append(out, h)
	}
	return out
}

// siteverifyResponse is the answer of the siteverify endpoint.
type siteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
}

// Verify implements TurnstileVerifier.
func (t *CloudflareTurnstile) Verify(ctx context.Context, token, remoteIP string) error {
	if token == "" {
		return fmt.Errorf("%w: token missing", ErrTurnstileRejected)
	}
	if len(token) > turnstileMaxTokenLength {
		return fmt.Errorf("%w: token longer than %d characters", ErrTurnstileRejected, turnstileMaxTokenLength)
	}

	form := url.Values{}
	form.Set("secret", t.secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	ctx, cancel := context.WithTimeout(ctx, turnstileTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("turnstile: build siteverify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile: siteverify call failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("turnstile: siteverify answered status %d", resp.StatusCode)
	}

	var result siteverifyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); err != nil {
		return fmt.Errorf("turnstile: decode siteverify answer: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("%w: %s", ErrTurnstileRejected, strings.Join(result.ErrorCodes, ", "))
	}
	if result.Action != t.action {
		return fmt.Errorf("%w: action %q, expected %q", ErrTurnstileRejected, result.Action, t.action)
	}
	if !t.hostnames[strings.ToLower(result.Hostname)] {
		return fmt.Errorf("%w: hostname %q is not an approved frontend hostname", ErrTurnstileRejected, result.Hostname)
	}
	return nil
}

// clientIP returns the address of the client for the siteverify remoteip
// hint: the first X-Forwarded-For entry when a reverse proxy set one,
// otherwise the peer address. The hint only helps Cloudflare's scoring; it is
// not a security decision, so trusting the header is fine here.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := strings.TrimSpace(first); net.ParseIP(ip) != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}
