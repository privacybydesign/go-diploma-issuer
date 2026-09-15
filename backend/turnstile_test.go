package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go-diploma-issuer/models"

	"github.com/stretchr/testify/require"
)

// fakeSiteverify stands in for Cloudflare's siteverify endpoint: it records
// the form it received and answers with a fixed body and status.
type fakeSiteverify struct {
	status int
	body   string
	form   map[string]string
	calls  int
	mutex  sync.Mutex
}

func (f *fakeSiteverify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.calls++
	_ = r.ParseForm()
	f.form = map[string]string{}
	for k := range r.PostForm {
		f.form[k] = r.PostForm.Get(k)
	}
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte(f.body))
}

func newTurnstile(t *testing.T, fake *fakeSiteverify, hostnames ...string) *CloudflareTurnstile {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	verifier, err := NewCloudflareTurnstile(TurnstileConfig{
		SiteKey:       "site-key",
		Secret:        "secret-value",
		Hostnames:     hostnames,
		SiteverifyUrl: srv.URL,
	})
	require.NoError(t, err)
	return verifier
}

func TestNewCloudflareTurnstileRejectsUnusableConfig(t *testing.T) {
	t.Setenv(TurnstileSecretEnv, "")

	_, err := NewCloudflareTurnstile(TurnstileConfig{SiteKey: "k", Hostnames: []string{"diploma.example"}})
	require.ErrorContains(t, err, "no secret")

	_, err = NewCloudflareTurnstile(TurnstileConfig{Secret: "s", Hostnames: []string{"diploma.example"}})
	require.ErrorContains(t, err, "site_key")

	_, err = NewCloudflareTurnstile(TurnstileConfig{SiteKey: "k", Secret: "s"})
	require.ErrorContains(t, err, "hostname")

	_, err = NewCloudflareTurnstile(TurnstileConfig{SiteKey: "k", Secret: "s", Hostnames: []string{" ", ""}})
	require.ErrorContains(t, err, "hostname")

	verifier, err := NewCloudflareTurnstile(TurnstileConfig{SiteKey: "k", Secret: "s", Hostnames: []string{" DIPLOMA.Example "}})
	require.NoError(t, err)
	require.Equal(t, []string{"diploma.example"}, verifier.Hostnames())
}

func TestTurnstileSecretFromEnvironment(t *testing.T) {
	t.Setenv(TurnstileSecretEnv, "from-env")
	cfg := TurnstileConfig{SiteKey: "k", Hostnames: []string{"diploma.example"}}
	require.True(t, cfg.Enabled())

	fake := &fakeSiteverify{status: http.StatusOK, body: `{"success":true,"action":"diploma-upload","hostname":"diploma.example"}`}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	cfg.SiteverifyUrl = srv.URL
	verifier, err := NewCloudflareTurnstile(cfg)
	require.NoError(t, err)

	require.NoError(t, verifier.Verify(context.Background(), "tok", ""))
	require.Equal(t, "from-env", fake.form["secret"])

	t.Setenv(TurnstileSecretEnv, "")
	require.False(t, cfg.Enabled())
}

func TestCloudflareTurnstileVerify(t *testing.T) {
	accepted := `{"success":true,"challenge_ts":"2026-09-08T10:00:00Z","hostname":"diploma.example","action":"diploma-upload","cdata":""}`

	t.Run("accepts a valid token and sends the canonical form", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: accepted}
		verifier := newTurnstile(t, fake, "diploma.example")

		require.NoError(t, verifier.Verify(context.Background(), "the-token", "203.0.113.7"))
		require.Equal(t, 1, fake.calls)
		require.Equal(t, "secret-value", fake.form["secret"])
		require.Equal(t, "the-token", fake.form["response"])
		require.Equal(t, "203.0.113.7", fake.form["remoteip"])
	})

	t.Run("omits remoteip when unknown", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: accepted}
		verifier := newTurnstile(t, fake, "diploma.example")

		require.NoError(t, verifier.Verify(context.Background(), "the-token", ""))
		_, present := fake.form["remoteip"]
		require.False(t, present)
	})

	t.Run("compares the hostname case-insensitively", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: strings.Replace(accepted, "diploma.example", "DIPLOMA.Example", 1)}
		verifier := newTurnstile(t, fake, "diploma.example", "localhost")
		require.NoError(t, verifier.Verify(context.Background(), "the-token", ""))
	})

	t.Run("refuses an empty or oversized token without calling siteverify", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: accepted}
		verifier := newTurnstile(t, fake, "diploma.example")

		err := verifier.Verify(context.Background(), "", "")
		require.ErrorIs(t, err, ErrTurnstileRejected)
		err = verifier.Verify(context.Background(), strings.Repeat("x", turnstileMaxTokenLength+1), "")
		require.ErrorIs(t, err, ErrTurnstileRejected)
		require.Equal(t, 0, fake.calls)
	})

	t.Run("refuses a token siteverify rejects", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: `{"success":false,"error-codes":["timeout-or-duplicate"]}`}
		verifier := newTurnstile(t, fake, "diploma.example")

		err := verifier.Verify(context.Background(), "used-token", "")
		require.ErrorIs(t, err, ErrTurnstileRejected)
		require.ErrorContains(t, err, "timeout-or-duplicate")
	})

	t.Run("refuses a token minted for another action", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: strings.Replace(accepted, "diploma-upload", "login", 1)}
		verifier := newTurnstile(t, fake, "diploma.example")

		err := verifier.Verify(context.Background(), "tok", "")
		require.ErrorIs(t, err, ErrTurnstileRejected)
		require.ErrorContains(t, err, `action "login"`)
	})

	t.Run("refuses a token solved on another site", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: strings.Replace(accepted, "diploma.example", "evil.example", 1)}
		verifier := newTurnstile(t, fake, "diploma.example")

		err := verifier.Verify(context.Background(), "tok", "")
		require.ErrorIs(t, err, ErrTurnstileRejected)
		require.ErrorContains(t, err, "evil.example")
	})

	t.Run("fails closed when siteverify errors", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusBadGateway, body: "upstream error"}
		verifier := newTurnstile(t, fake, "diploma.example")

		err := verifier.Verify(context.Background(), "tok", "")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrTurnstileRejected)
	})

	t.Run("fails closed on a malformed answer", func(t *testing.T) {
		fake := &fakeSiteverify{status: http.StatusOK, body: "not json"}
		verifier := newTurnstile(t, fake, "diploma.example")
		require.Error(t, verifier.Verify(context.Background(), "tok", ""))
	})

	t.Run("fails closed when siteverify is unreachable", func(t *testing.T) {
		verifier, err := NewCloudflareTurnstile(TurnstileConfig{
			SiteKey: "k", Secret: "s", Hostnames: []string{"diploma.example"},
			SiteverifyUrl: "http://127.0.0.1:1/siteverify",
		})
		require.NoError(t, err)
		require.Error(t, verifier.Verify(context.Background(), "tok", ""))
	})
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/diploma/upload", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	require.Equal(t, "192.0.2.10", clientIP(r))

	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	require.Equal(t, "203.0.113.7", clientIP(r))

	r.Header.Set("X-Forwarded-For", "not an ip")
	require.Equal(t, "192.0.2.10", clientIP(r))

	r.Header.Del("X-Forwarded-For")
	r.RemoteAddr = "[2001:db8::1]:443"
	require.Equal(t, "2001:db8::1", clientIP(r))

	r.RemoteAddr = "garbage"
	require.Equal(t, "", clientIP(r))
}

// fakeTurnstile accepts exactly one token value and records what it saw.
type fakeTurnstile struct {
	accept string
	calls  int
	last   string
	lastIP string
}

func (f *fakeTurnstile) Verify(_ context.Context, token, remoteIP string) error {
	f.calls++
	f.last = token
	f.lastIP = remoteIP
	if token != f.accept {
		return fmt.Errorf("%w: unexpected token", ErrTurnstileRejected)
	}
	return nil
}

// postDiplomaWithToken uploads the fake PDF together with a Turnstile token
// (an empty token leaves the field out, like a page without the widget).
func postDiplomaWithToken(t *testing.T, token string) (*http.Response, []byte, *models.ErrorResponse) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if token != "" {
		require.NoError(t, writer.WriteField(TurnstileTokenField, token))
	}
	part, err := writer.CreateFormFile("file", "diploma.pdf")
	require.NoError(t, err)
	_, err = part.Write(fakePdf)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	resp, err := http.Post(fmt.Sprintf(testHost, uploadEndpoint), writer.FormDataContentType(), &body)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errResp models.ErrorResponse
	_ = json.Unmarshal(respBody, &errResp)
	return resp, respBody, &errResp
}

func getConfig(t *testing.T) models.ConfigResponse {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf(testHost, "/api/config"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var cfg models.ConfigResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	return cfg
}

func TestUploadGatedByTurnstile(t *testing.T) {
	deps := defaultDeps()
	turnstile := &fakeTurnstile{accept: "good-token"}
	deps.turnstile = turnstile
	deps.turnstileSiteKey = "0x4AAAAAAEskYIZQOLbu1QvE"
	startTestServer(t, deps)

	t.Run("frontend learns the sitekey", func(t *testing.T) {
		require.Equal(t, "0x4AAAAAAEskYIZQOLbu1QvE", getConfig(t).TurnstileSiteKey)
	})

	t.Run("upload without a token is refused before any work is done", func(t *testing.T) {
		resp, body, errResp := postDiplomaWithToken(t, "")
		mustStatus(t, resp, http.StatusForbidden, body)
		require.Equal(t, ErrorBotCheckFailed, errResp.Error)
		require.Equal(t, 0, deps.validator.calls)
	})

	t.Run("upload with a rejected token is refused", func(t *testing.T) {
		resp, body, errResp := postDiplomaWithToken(t, "replayed-token")
		mustStatus(t, resp, http.StatusForbidden, body)
		require.Equal(t, ErrorBotCheckFailed, errResp.Error)
		require.Equal(t, "replayed-token", turnstile.last)
		require.Equal(t, 0, deps.validator.calls)
	})

	t.Run("upload with a valid token proceeds as before", func(t *testing.T) {
		resp, body, _ := postDiplomaWithToken(t, "good-token")
		mustStatus(t, resp, http.StatusOK, body)
		var upload models.UploadResponse
		require.NoError(t, json.Unmarshal(body, &upload))
		require.NotEmpty(t, upload.SessionId)
		require.Equal(t, "good-token", turnstile.last)
		require.Equal(t, "127.0.0.1", turnstile.lastIP)
		require.Equal(t, 1, deps.validator.calls)
	})
}

func TestUploadWithoutTurnstileConfigured(t *testing.T) {
	deps := defaultDeps()
	startTestServer(t, deps)

	require.Empty(t, getConfig(t).TurnstileSiteKey)
	uploadOk(t)
}

// Guard against the real verifier being wired into the test server by
// accident: an unreachable siteverify must fail closed.
func TestUploadFailsClosedWhenSiteverifyUnreachable(t *testing.T) {
	verifier, err := NewCloudflareTurnstile(TurnstileConfig{
		SiteKey: "k", Secret: "s", Hostnames: []string{"localhost"},
		SiteverifyUrl: "http://127.0.0.1:1/siteverify",
	})
	require.NoError(t, err)

	deps := defaultDeps()
	deps.turnstile = verifier
	startTestServer(t, deps)

	resp, body, errResp := postDiplomaWithToken(t, "tok")
	mustStatus(t, resp, http.StatusForbidden, body)
	require.Equal(t, ErrorBotCheckFailed, errResp.Error)
	require.Equal(t, 0, deps.validator.calls)
	require.False(t, errors.Is(err, ErrTurnstileRejected))
}
