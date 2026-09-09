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
	"net/textproto"
	"sync"
	"testing"
	"time"

	"go-diploma-issuer/diploma"
	"go-diploma-issuer/diploma/verify"

	"github.com/privacybydesign/irmago/irma"
	"github.com/privacybydesign/irmago/irma/server"
	"github.com/stretchr/testify/require"
)

var testConfig = ServerConfig{
	Host: "localhost",
	Port: 8081,
}

const testHost = "http://localhost:8081%s"

// fakeValidator answers with a fixed verification (or error) and records what
// it saw. Per file content an override can be registered.
type fakeValidator struct {
	valid     bool
	err       error
	overrides map[string]*diploma.Verification // keyed by file content
	calls     int
	last      []byte
	mutex     sync.Mutex
}

func validVerification() *diploma.Verification {
	return &diploma.Verification{
		Valid: true,
		Checks: []verify.Check{
			{ID: verify.CheckSignaturePresent, Name: "signature present", OK: true},
			{ID: verify.CheckCertificateChain, Name: "certificate chain to trust anchor", OK: true, Detail: "3 certificates"},
			{ID: verify.CheckSignerIdentity, Name: "signer is DUO", OK: true},
		},
		Signer:           "CN=Dienst Uitvoering Onderwijs,O=Dienst Uitvoering Onderwijs (DUO),C=NL",
		TrustAnchor:      "EU trusted list NL: KPN B.V.",
		SigningTime:      time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
		TimestampTrusted: true,
	}
}

func invalidVerification(failedCheck string) *diploma.Verification {
	return &diploma.Verification{
		Valid: false,
		Checks: []verify.Check{
			{ID: verify.CheckSignaturePresent, Name: "signature present", OK: true},
			{ID: failedCheck, Name: failedCheck, OK: false, Detail: "failed in test"},
		},
		SigningTime: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
	}
}

func (f *fakeValidator) Validate(_ context.Context, pdf []byte) (*diploma.Verification, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.calls++
	f.last = pdf
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.overrides[string(pdf)]; ok {
		return v, nil
	}
	if f.valid {
		return validVerification(), nil
	}
	return invalidVerification(verify.CheckCertificateChain), nil
}

// fakeParser returns a fixed document (or error); per file content an
// override can be registered.
type fakeParser struct {
	doc       *diploma.Document
	err       error
	overrides map[string]*diploma.Document // keyed by file content
	errors    map[string]error             // keyed by file content
}

func (f fakeParser) Parse(pdf []byte) (*diploma.Document, error) {
	if err, ok := f.errors[string(pdf)]; ok {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	if doc, ok := f.overrides[string(pdf)]; ok {
		copied := *doc
		return &copied, nil
	}
	copied := *f.doc
	return &copied, nil
}

// fakeJwtCreator returns fixed JWTs and records the issued documents.
type fakeJwtCreator struct {
	disclosureJwt string
	issuanceJwt   string
	issuedDocs    []*diploma.Document
	issuedSource  string
	err           error
}

func (f *fakeJwtCreator) CreateDisclosureJwt() (string, error) {
	return f.disclosureJwt, f.err
}

func (f *fakeJwtCreator) CreateIssuanceJwt(docs []*diploma.Document, source string) (string, error) {
	f.issuedDocs = docs
	f.issuedSource = source
	return f.issuanceJwt, f.err
}

// fakeIrmaClient simulates the IRMA server: StartSession hands out a token and
// GetSessionResult returns the configured result for that token.
type fakeIrmaClient struct {
	startErr  error
	resultErr error
	result    *server.SessionResult
	token     irma.RequestorToken
	startJwt  string
	asked     irma.RequestorToken
}

func (f *fakeIrmaClient) StartSession(_ context.Context, signedJwt string) (*server.SessionPackage, error) {
	f.startJwt = signedJwt
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &server.SessionPackage{
		SessionPtr:      &irma.Qr{URL: "https://irma.example/irma/session/xyz", Type: irma.ActionDisclosing},
		Token:           f.token,
		FrontendRequest: &irma.FrontendSessionRequest{Authorization: "auth", MinProtocolVersion: irma.NewVersion(1, 0), MaxProtocolVersion: irma.NewVersion(1, 1)},
	}, nil
}

func (f *fakeIrmaClient) GetSessionResult(_ context.Context, token irma.RequestorToken) (*server.SessionResult, error) {
	f.asked = token
	if f.resultErr != nil {
		return nil, f.resultErr
	}
	return f.result, nil
}

// validDisclosure builds a DONE/VALID passport disclosure for the given person.
func validDisclosure(givenNames, lastName, dateOfBirth string) *server.SessionResult {
	return &server.SessionResult{
		Token:       "tok",
		Status:      irma.ServerStatusDone,
		Type:        irma.ActionDisclosing,
		ProofStatus: irma.ProofStatusValid,
		Disclosed: [][]*irma.DisclosedAttribute{{
			disclosedAttr(testIdentityCredentials.Passport+"."+DocAttrFirstName, givenNames),
			disclosedAttr(testIdentityCredentials.Passport+"."+DocAttrLastName, lastName),
			disclosedAttr(testIdentityCredentials.Passport+"."+DocAttrDateOfBirth, dateOfBirth),
		}},
	}
}

type testDeps struct {
	storage   SessionStorage
	validator *fakeValidator
	parser    fakeParser
	jwt       *fakeJwtCreator
	irma      *fakeIrmaClient
}

func defaultDeps() *testDeps {
	return &testDeps{
		storage:   NewInMemorySessionStorage(),
		validator: &fakeValidator{valid: true},
		parser:    fakeParser{doc: testDiploma()},
		jwt:       &fakeJwtCreator{disclosureJwt: "disclosure-jwt", issuanceJwt: "issuance-jwt"},
		irma:      &fakeIrmaClient{token: "tok", result: validDisclosure("ANNA MARIA", "VAN DER BERG", "1980-02-03")},
	}
}

func startTestServer(t *testing.T, deps *testDeps) *Server {
	t.Helper()

	state := &ServerState{
		irmaServerURL:       "https://irma.example",
		sessionStorage:      deps.storage,
		jwtCreator:          deps.jwt,
		validator:           deps.validator,
		parser:              deps.parser,
		irmaClient:          deps.irma,
		identityCredentials: testIdentityCredentials,
		maxUploadSize:       1 << 20,
		maxFiles:            3,
	}

	srv, err := NewServer(state, testConfig)
	require.NoError(t, err)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("server error: %v", err)
		}
	}()

	waitUntilHealthy(t, fmt.Sprintf(testHost, "/api/health"))
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Logf("error shutting down server: %v", err)
		}
	})
	return srv
}

func waitUntilHealthy(t *testing.T, url string) {
	t.Helper()
	const maxAttempts = 50
	for i := 0; i < maxAttempts; i++ {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not start in time")
}

func postJSON[T any](t *testing.T, url string, payload any) (*http.Response, []byte, *T) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewBuffer(b)
	}
	resp, err := http.Post(url, "application/json", body)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var v T
	_ = json.Unmarshal(respBody, &v)
	return resp, respBody, &v
}

// upload is one file of a multipart upload.
type upload struct {
	filename string
	content  []byte
}

// postFile uploads content as multipart field "file".
func postFile[T any](t *testing.T, url, field, filename string, content []byte) (*http.Response, []byte, *T) {
	t.Helper()
	if field == "" {
		return postFiles[T](t, url, "")
	}
	return postFiles[T](t, url, field, upload{filename, content})
}

// postFiles uploads several files, all as multipart field "field".
func postFiles[T any](t *testing.T, url, field string, files ...upload) (*http.Response, []byte, *T) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := writer.CreateFormFile(field, f.filename)
		require.NoError(t, err)
		_, err = part.Write(f.content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	resp, err := http.Post(url, writer.FormDataContentType(), &body)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var v T
	_ = json.Unmarshal(respBody, &v)
	return resp, respBody, &v
}

// postFileWithType uploads content as multipart field "file" with an explicit
// part Content-Type instead of the application/octet-stream that
// CreateFormFile sets.
func postFileWithType[T any](t *testing.T, url, filename, contentType string, content []byte) (*http.Response, []byte, *T) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", contentType)
	part, err := writer.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	resp, err := http.Post(url, writer.FormDataContentType(), &body)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var v T
	_ = json.Unmarshal(respBody, &v)
	return resp, respBody, &v
}

func mustStatus(t *testing.T, resp *http.Response, want int, body []byte) {
	t.Helper()
	require.Equalf(t, want, resp.StatusCode, "body: %s", body)
}
