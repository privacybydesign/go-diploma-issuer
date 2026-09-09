package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go-diploma-issuer/diploma"
	"go-diploma-issuer/identity"
	"go-diploma-issuer/models"

	"github.com/gorilla/mux"
	"github.com/privacybydesign/irmago/irma"
)

//go:embed docs/swagger.yaml
var swaggerSpec []byte

//go:embed docs/redoc.html
var redocHTML []byte

// Error keys returned in the "error" field of an ErrorResponse and, per file,
// in a FileResult.
const (
	ErrorInternal          = "error:internal"
	ErrorMethodNotAllowed  = "error:method-not-allowed"
	ErrorInvalidRequest    = "error:invalid-request"
	ErrorFileMissing       = "error:file-missing"
	ErrorTooManyFiles      = "error:too-many-files"
	ErrorFileTooLarge      = "error:file-too-large"
	ErrorNotAPdf           = "error:not-a-pdf"
	ErrorNotADiploma       = "error:not-a-diploma"
	ErrorValidationFailed  = "error:validation-failed"
	ErrorValidationService = "error:validation-service-unavailable"
	ErrorDifferentPerson   = "error:different-person"
	ErrorDuplicateDiploma  = "error:duplicate-diploma"
	ErrorUnknownSession    = "error:unknown-session"
	ErrorDisclosureNotDone = "error:disclosure-not-done"
	ErrorDisclosureInvalid = "error:disclosure-invalid"
	ErrorIdentityMismatch  = "error:identity-mismatch"
	ErrorIrmaServer        = "error:irma-server"
)

const (
	DefaultMaxUploadSize = 5 << 20 // 5 MiB per file
	DefaultMaxFiles      = 10
	// multipartOverhead is the room left for multipart boundaries and headers
	// on top of the file sizes when bounding the request body.
	multipartOverhead = 64 << 10
)

type ServerConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseTls         bool   `json:"use_tls,omitempty"`
	TlsPrivKeyPath string `json:"tls_priv_key_path,omitempty"`
	TlsCertPath    string `json:"tls_cert_path,omitempty"`
	// Serve the API documentation on /api/docs and /api/docs/swagger.yaml.
	// Disabled unless explicitly enabled, so the docs stay off in production.
	EnableApiDocs bool `json:"enable_api_docs,omitempty"`
}

type ServerState struct {
	irmaServerURL       string
	sessionStorage      SessionStorage
	jwtCreator          JwtCreator
	validator           diploma.Validator
	parser              diploma.Parser
	irmaClient          IrmaClient
	identityCredentials IdentityCredentials
	maxUploadSize       int64
	maxFiles            int
}

type SpaHandler struct {
	staticPath string
	indexPath  string
}

type Server struct {
	server *http.Server
	config ServerConfig
}

func (s *Server) ListenAndServe() error {
	if s.config.UseTls {
		slog.Info("Starting server with TLS", "host", s.config.Host, "port", s.config.Port, "cert", s.config.TlsCertPath, "key", s.config.TlsPrivKeyPath)
		return s.server.ListenAndServeTLS(s.config.TlsCertPath, s.config.TlsPrivKeyPath)
	}
	slog.Info("Starting server without TLS", "host", s.config.Host, "port", s.config.Port)
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	slog.Info("Shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.server.Shutdown(ctx)
	if err != nil {
		slog.Error("Error during server shutdown", "error", err)
	} else {
		slog.Info("Server shut down successfully")
	}
	return err
}

// ServeHTTP inspects the URL path to locate a file within the static dir
// on the SPA handler. If a file is found, it will be served. If not, the
// file located at the index path on the SPA handler will be served. This
// is suitable behavior for serving an SPA (single page application).
// https://github.com/gorilla/mux?tab=readme-ov-file#serving-single-page-applications
func (h SpaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slog.Debug("SPA handler serving request", "path", r.URL.Path)
	// Join internally call path.Clean to prevent directory traversal
	path := filepath.Join(h.staticPath, r.URL.Path)
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}

	if err != nil {
		// Log the raw OS error server-side and return a generic 500 so we don't
		// leak filesystem paths or OS internals to the client.
		slog.Error("static file error", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if fi.IsDir() {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}

	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func NewServer(state *ServerState, config ServerConfig) (*Server, error) {
	slog.Info("Creating new server", "host", config.Host, "port", config.Port, "tls", config.UseTls)
	if state.maxUploadSize <= 0 {
		state.maxUploadSize = DefaultMaxUploadSize
	}
	if state.maxFiles <= 0 {
		state.maxFiles = DefaultMaxFiles
	}
	router := mux.NewRouter()

	router.HandleFunc("/api/health", handleHealth).Methods(http.MethodGet)

	router.HandleFunc("/api/diploma/upload", func(w http.ResponseWriter, r *http.Request) {
		handleUpload(state, w, r)
	})
	router.HandleFunc("/api/diploma/start-disclosure", func(w http.ResponseWriter, r *http.Request) {
		handleStartDisclosure(state, w, r)
	})
	router.HandleFunc("/api/diploma/issue", func(w http.ResponseWriter, r *http.Request) {
		handleIssue(state, w, r)
	})

	// API Documentation
	if config.EnableApiDocs {
		router.HandleFunc("/api/docs", HandleRedocRequest).Methods(http.MethodGet)
		router.HandleFunc("/api/docs/swagger.yaml", HandleSwaggerRequest).Methods(http.MethodGet)
		slog.Info("API documentation enabled", "path", "/api/docs")
	} else {
		slog.Info("API documentation disabled")
	}

	spa := SpaHandler{staticPath: "../frontend/build", indexPath: "index.html"}
	router.PathPrefix("/").Handler(spa)

	addr := fmt.Sprintf("%v:%v", config.Host, config.Port)
	srv := &http.Server{
		Handler: router,
		Addr:    addr,
		// The upload handler verifies up to maxFiles signatures (and may
		// query OCSP), so allow more than the usual 15 seconds before the
		// write deadline hits.
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	slog.Info("Server created successfully", "address", addr)
	return &Server{
		server: srv,
		config: config,
	}, nil
}

// handleHealth returns the health status of the service
// @Summary Health check
// @Description Returns the health status of the API service
// @Tags Health
// @Produce json
// @Success 200 {object} models.HealthResponse
// @Router /health [get]
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := writeJSON(w, http.StatusOK, models.HealthResponse{Ok: true}); err != nil {
		slog.Error("failed to write health response", "error", err)
	}
}

// fileOutcome is the result of processing one uploaded file: the API facing
// FileResult plus, when accepted, the parsed document.
type fileOutcome struct {
	result   models.FileResult
	document *diploma.Document
	// status is the HTTP status the rejection would get on its own.
	status int
}

// rejected builds a rejected outcome.
func rejected(filename string, status int, errorKey, message string, validation *models.ValidationInfo) fileOutcome {
	return fileOutcome{
		status: status,
		result: models.FileResult{
			Filename:   filename,
			Accepted:   false,
			Error:      errorKey,
			Message:    message,
			Validation: validation,
		},
	}
}

// handleUpload verifies and parses one or more uploaded diploma extracts
// @Summary Upload and verify diploma extracts
// @Description Accepts one or more diploma extract PDFs from DUO's "Mijn diploma's" (repeat the multipart form field "file", at most max_files per request). Every file is verified cryptographically: the PDF must carry a single certification signature that covers the whole file, the signature must be valid, timestamped by a qualified timestamp authority and made with a qualified electronic seal certificate of DUO that chains to a trust service on the EU Trusted Lists. Only then is the printed data read (qualification, holder, date of birth, institution, award date, level). All accepted extracts must name the same holder. Files that fail are reported per file; as long as one file is accepted the request succeeds and a session is created for the accepted files. The session expires after one hour.
// @Tags Diploma
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "One or more diploma extract PDFs downloaded from Mijn diploma's (DUO)"
// @Success 200 {object} models.UploadResponse "at least one file was accepted; check files[].accepted for the rest"
// @Failure 400 {object} models.ErrorResponse "no file, too many files, or none of the files is a PDF / a diploma extract (per-file detail in files)"
// @Failure 413 {object} models.ErrorResponse "a file or the request exceeds the size limit"
// @Failure 422 {object} models.ErrorResponse "none of the files was accepted because the signature verification failed (per-file detail in files)"
// @Failure 503 {object} models.ErrorResponse "the trust anchors (EU trusted lists) are not available, so signatures cannot be verified right now"
// @Failure 500 {object} models.ErrorResponse
// @Router /diploma/upload [post]
func handleUpload(state *ServerState, w http.ResponseWriter, r *http.Request) {
	defer closeRequestBody(r)

	if !requirePOST(w, r) {
		return
	}
	const endpoint = "diploma/upload"
	slog.Info("Received request", "endpoint", endpoint)

	bodyLimit := state.maxUploadSize*int64(state.maxFiles) + multipartOverhead
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	if err := r.ParseMultipartForm(state.maxUploadSize); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondWithErr(w, http.StatusRequestEntityTooLarge, ErrorFileTooLarge, "upload exceeds the size limit", err, "endpoint", endpoint)
			return
		}
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidRequest, "failed to parse multipart form", err, "endpoint", endpoint)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		respondWithErr(w, http.StatusBadRequest, ErrorFileMissing, "multipart field 'file' is missing", nil, "endpoint", endpoint)
		return
	}
	if len(headers) > state.maxFiles {
		respondWithErr(w, http.StatusBadRequest, ErrorTooManyFiles, fmt.Sprintf("at most %d files per upload", state.maxFiles), fmt.Errorf("%d files uploaded", len(headers)), "endpoint", endpoint)
		return
	}

	var (
		results   = make([]models.FileResult, 0, len(headers))
		documents []*diploma.Document
		first     *fileOutcome
	)
	for _, header := range headers {
		outcome, err := processFile(state, r.Context(), header, documents)
		if err != nil {
			// Not a problem with the file but with us: stop the whole upload.
			status, key := http.StatusInternalServerError, ErrorInternal
			if errors.Is(err, diploma.ErrTrustUnavailable) {
				status, key = http.StatusServiceUnavailable, ErrorValidationService
			}
			respondWithErr(w, status, key, "failed to verify uploaded file", err, "endpoint", endpoint)
			return
		}
		results = append(results, outcome.result)
		if outcome.result.Accepted {
			documents = append(documents, outcome.document)
		} else if first == nil {
			o := outcome
			first = &o
		}
	}

	if len(documents) == 0 {
		respondWithJSONErr(w, first.status, models.ErrorResponse{
			Error:   first.result.Error,
			Message: "none of the uploaded files was accepted",
			Files:   results,
		}, "all uploaded files rejected", fmt.Errorf("%d file(s), first: %s", len(results), first.result.Message), "endpoint", endpoint)
		return
	}

	sessionId := GenerateSessionId()
	if sessionId == "" {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to generate session ID", fmt.Errorf("failed to generate session ID"))
		return
	}
	session := &Session{
		Id:        sessionId,
		CreatedAt: time.Now(),
		Stage:     StageValidated,
		Documents: documents,
	}
	if err := state.sessionStorage.Store(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint)
		return
	}

	response := models.UploadResponse{
		SessionId: sessionId,
		Person: models.PersonInfo{
			FullName:    documents[0].FullName,
			DateOfBirth: documents[0].DateOfBirth.Format(DATE_FORMAT_CYMD),
		},
		Files:    results,
		Accepted: len(documents),
		Rejected: len(results) - len(documents),
	}
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to marshal response message", err)
		return
	}
	numbers := make([]string, 0, len(documents))
	for _, doc := range documents {
		numbers = append(numbers, doc.DocumentNumber)
	}
	slog.Info("Diploma extracts verified and parsed", "session_id", sessionId, "accepted", len(documents), "rejected", response.Rejected, "document_numbers", numbers)
}

// processFile runs one uploaded file through the checks: metadata, PDF
// signature, cryptographic verification, parsing and consistency with the
// extracts accepted before it. A returned error means the check itself could
// not be performed; anything wrong with the file is a rejected outcome.
func processFile(state *ServerState, ctx context.Context, header *multipart.FileHeader, accepted []*diploma.Document) (fileOutcome, error) {
	filename := header.Filename
	file, err := header.Open()
	if err != nil {
		return fileOutcome{}, fmt.Errorf("open uploaded file %q: %w", filename, err)
	}
	defer func() { _ = file.Close() }()

	pdf, err := io.ReadAll(io.LimitReader(file, state.maxUploadSize+1))
	if err != nil {
		return fileOutcome{}, fmt.Errorf("read uploaded file %q: %w", filename, err)
	}
	if int64(len(pdf)) > state.maxUploadSize {
		return rejected(filename, http.StatusRequestEntityTooLarge, ErrorFileTooLarge, "the file exceeds the size limit", nil), nil
	}
	if !acceptableUploadMetadata(filename, header.Header.Get("Content-Type")) || !looksLikePdf(pdf) {
		return rejected(filename, http.StatusBadRequest, ErrorNotAPdf, "the file is not a PDF", nil), nil
	}

	// Authenticity and integrity first: only a genuine extract is worth
	// parsing.
	verification, err := state.validator.Validate(ctx, pdf)
	if err != nil {
		return fileOutcome{}, err
	}
	validation := validationInfo(verification)
	if !verification.Valid {
		slog.Warn("uploaded file rejected by signature verification", "filename", filename, "key", verification.Key())
		return rejected(filename, http.StatusUnprocessableEntity, ErrorValidationFailed, "the signature of the document could not be verified", &validation), nil
	}

	doc, err := state.parser.Parse(pdf)
	if err != nil {
		if errors.Is(err, diploma.ErrNotADiploma) {
			slog.Warn("uploaded file is not a diploma extract", "filename", filename, "error", err)
			return rejected(filename, http.StatusBadRequest, ErrorNotADiploma, "the document is not a diploma extract from DUO", &validation), nil
		}
		return fileOutcome{}, fmt.Errorf("parse %q: %w", filename, err)
	}

	for _, other := range accepted {
		if !doc.SamePerson(other) {
			return rejected(filename, http.StatusUnprocessableEntity, ErrorDifferentPerson, "the extract names a different person than the other extracts", &validation), nil
		}
		if doc.DocumentNumber == other.DocumentNumber && doc.Qualification == other.Qualification && doc.DateAwarded.Equal(other.DateAwarded) {
			return rejected(filename, http.StatusBadRequest, ErrorDuplicateDiploma, "this diploma was already uploaded", &validation), nil
		}
	}

	info := documentInfo(doc)
	return fileOutcome{
		status:   http.StatusOK,
		document: doc,
		result: models.FileResult{
			Filename:   filename,
			Accepted:   true,
			Validation: &validation,
			Document:   &info,
		},
	}, nil
}

// handleStartDisclosure starts the identity disclosure session
// @Summary Start identity disclosure
// @Description Starts a Yivi disclosure session in which the holder proves their identity with one of four credentials: BRP personal data (gemeente.personalData), passport, ID card or driving licence. The Yivi app lets the user pick. The response is the IRMA session package that yivi-frontend consumes directly (use it as the response of the `session.start` request with the default mapping and `result: false`). The requestor token is kept server side.
// @Tags Diploma
// @Accept json
// @Produce json
// @Param request body models.SessionRequest true "Session from /diploma/upload"
// @Success 200 {object} models.DisclosureSessionResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse "unknown or expired session"
// @Failure 502 {object} models.ErrorResponse "the IRMA server did not accept the session"
// @Failure 500 {object} models.ErrorResponse
// @Router /diploma/start-disclosure [post]
func handleStartDisclosure(state *ServerState, w http.ResponseWriter, r *http.Request) {
	defer closeRequestBody(r)

	if !requirePOST(w, r) {
		return
	}
	const endpoint = "diploma/start-disclosure"
	slog.Info("Received request", "endpoint", endpoint)

	request, err := decodeSessionRequest(r)
	if err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidRequest, "failed to decode request", err, "endpoint", endpoint)
		return
	}
	session, err := state.sessionStorage.Retrieve(request.SessionId)
	if err != nil {
		respondWithErr(w, http.StatusNotFound, ErrorUnknownSession, "unknown session", err, "endpoint", endpoint, "session_id", request.SessionId)
		return
	}

	signedJwt, err := state.jwtCreator.CreateDisclosureJwt()
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to create disclosure jwt", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}
	pkg, err := state.irmaClient.StartSession(r.Context(), signedJwt)
	if err != nil {
		respondWithErr(w, http.StatusBadGateway, ErrorIrmaServer, "failed to start disclosure session on irma server", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}

	session.Stage = StageDisclosing
	session.IrmaToken = string(pkg.Token)
	if err := state.sessionStorage.Store(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}

	response := models.DisclosureSessionResponse{
		SessionPtr: models.SessionPointer{
			U:      pkg.SessionPtr.URL,
			Irmaqr: string(pkg.SessionPtr.Type),
		},
		FrontendRequest: pkg.FrontendRequest,
	}
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to marshal response message", err)
		return
	}
	slog.Info("Disclosure session started", "session_id", session.Id)
}

// handleIssue verifies the disclosed identity and issues the diploma credentials
// @Summary Verify identity and issue diploma credentials
// @Description Fetches the result of the disclosure session, compares the disclosed name and date of birth with the holder named on the extracts and, when they match, returns a signed IRMA issuance request with one diploma credential per accepted extract (IRMA and SD-JWT VC formats, issued over the IRMA protocol in a single session). On a mismatch the disclosure is discarded and the user may disclose again with another credential; the uploaded extracts stay available until the session expires.
// @Tags Diploma
// @Accept json
// @Produce json
// @Param request body models.SessionRequest true "Session from /diploma/upload"
// @Success 200 {object} models.IssuanceResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse "the disclosed identity does not match the holder of the extracts, or the disclosure proof is invalid"
// @Failure 404 {object} models.ErrorResponse "unknown or expired session"
// @Failure 409 {object} models.ErrorResponse "the disclosure session has not finished (or was not started)"
// @Failure 502 {object} models.ErrorResponse "the IRMA server could not be reached"
// @Failure 500 {object} models.ErrorResponse
// @Router /diploma/issue [post]
func handleIssue(state *ServerState, w http.ResponseWriter, r *http.Request) {
	defer closeRequestBody(r)

	if !requirePOST(w, r) {
		return
	}
	const endpoint = "diploma/issue"
	slog.Info("Received request", "endpoint", endpoint)

	request, err := decodeSessionRequest(r)
	if err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidRequest, "failed to decode request", err, "endpoint", endpoint)
		return
	}
	session, err := state.sessionStorage.Retrieve(request.SessionId)
	if err != nil {
		respondWithErr(w, http.StatusNotFound, ErrorUnknownSession, "unknown session", err, "endpoint", endpoint, "session_id", request.SessionId)
		return
	}
	if session.Stage != StageDisclosing || session.IrmaToken == "" {
		respondWithErr(w, http.StatusConflict, ErrorDisclosureNotDone, "disclosure session was not started", nil, "endpoint", endpoint, "session_id", session.Id)
		return
	}
	if len(session.Documents) == 0 {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "session holds no documents", nil, "endpoint", endpoint, "session_id", session.Id)
		return
	}

	result, err := state.irmaClient.GetSessionResult(r.Context(), irma.RequestorToken(session.IrmaToken))
	if err != nil {
		respondWithErr(w, http.StatusBadGateway, ErrorIrmaServer, "failed to fetch disclosure result", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}
	if result.Status != irma.ServerStatusDone {
		respondWithErr(w, http.StatusConflict, ErrorDisclosureNotDone, "disclosure session not finished", fmt.Errorf("status %s", result.Status), "endpoint", endpoint, "session_id", session.Id)
		return
	}
	if result.ProofStatus != irma.ProofStatusValid {
		resetDisclosure(state, session)
		respondWithErr(w, http.StatusForbidden, ErrorDisclosureInvalid, "disclosure proof invalid", fmt.Errorf("proof status %s", result.ProofStatus), "endpoint", endpoint, "session_id", session.Id)
		return
	}

	disclosed, err := ExtractIdentity(result.Disclosed, state.identityCredentials)
	if err != nil {
		resetDisclosure(state, session)
		respondWithErr(w, http.StatusForbidden, ErrorDisclosureInvalid, "disclosed attributes unusable", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}

	// Every extract in the session names the same holder (enforced at
	// upload), but compare each one anyway: the credentials are only issued
	// when all of them match.
	match := matchDocuments(session.Documents, disclosed.Person)
	matchInfo := models.IdentityMatchInfo{
		Source:           disclosed.Source,
		Matched:          match.Matched,
		DateOfBirthMatch: match.DateOfBirthMatch,
		SurnameMatch:     match.SurnameMatch,
		GivenNamesMatch:  match.GivenNamesMatch,
		Reasons:          match.Reasons,
	}
	if !match.Matched {
		// The disclosure is spent; the user may try again with another
		// credential, so keep the verified extracts around.
		resetDisclosure(state, session)
		respondWithJSONErr(w, http.StatusForbidden, models.ErrorResponse{
			Error:    ErrorIdentityMismatch,
			Message:  "the disclosed identity does not match the holder named on the diploma extracts",
			Identity: &matchInfo,
		}, "identity mismatch", fmt.Errorf("%v", match.Reasons), "endpoint", endpoint, "session_id", session.Id, "source", disclosed.Source)
		return
	}

	signedJwt, err := state.jwtCreator.CreateIssuanceJwt(session.Documents, disclosed.Source)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to create issuance jwt", err, "endpoint", endpoint, "session_id", session.Id)
		return
	}

	// Consume the session before writing the response so the diplomas cannot
	// be issued twice even if the write below fails.
	removeSession(state.sessionStorage, session.Id)

	response := models.IssuanceResponse{
		Jwt:           signedJwt,
		IrmaServerURL: state.irmaServerURL,
		Credentials:   len(session.Documents),
		Identity:      matchInfo,
	}
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to marshal response message", err)
		return
	}
	slog.Info("Diploma credentials issued", "session_id", session.Id, "credentials", len(session.Documents), "source", disclosed.Source)
}

// matchDocuments compares the disclosed person with the holder of every
// extract. The result is that of the first extract that does not match, or of
// the first extract when all match.
func matchDocuments(docs []*diploma.Document, disclosed identity.Person) identity.Result {
	var first identity.Result
	for i, doc := range docs {
		result := identity.MatchFullName(doc.FullName, doc.DateOfBirth.Format(DATE_FORMAT_CYMD), disclosed)
		if i == 0 {
			first = result
		}
		if !result.Matched {
			return result
		}
	}
	return first
}

// resetDisclosure forgets the (spent) disclosure session so the user can
// disclose again, keeping the verified extracts.
func resetDisclosure(state *ServerState, session *Session) {
	session.Stage = StageValidated
	session.IrmaToken = ""
	if err := state.sessionStorage.Store(session); err != nil {
		slog.Error("failed to reset disclosure state", "error", err, "session_id", session.Id)
	}
}

// removeSession deletes the session, logging (not reporting) failures: a
// removal failure must not alter the response.
func removeSession(storage SessionStorage, sessionId string) {
	if err := storage.Remove(sessionId); err != nil {
		slog.Error("failed to remove session", "error", err, "session_id", sessionId)
	}
}

func decodeSessionRequest(r *http.Request) (models.SessionRequest, error) {
	var request models.SessionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&request); err != nil {
		return request, fmt.Errorf("decode request body: %w", err)
	}
	if request.SessionId == "" {
		return request, fmt.Errorf("session_id is required")
	}
	return request, nil
}

func validationInfo(v *diploma.Verification) models.ValidationInfo {
	checks := make([]models.CheckInfo, 0, len(v.Checks))
	for _, c := range v.Checks {
		checks = append(checks, models.CheckInfo{Id: c.ID, Name: c.Name, Ok: c.OK, Detail: c.Detail})
	}
	info := models.ValidationInfo{
		Valid:            v.Valid,
		Key:              v.Key(),
		Signer:           v.Signer,
		TrustAnchor:      v.TrustAnchor,
		TimestampTrusted: v.TimestampTrusted,
		Checks:           checks,
	}
	if !v.SigningTime.IsZero() {
		info.SigningTime = v.SigningTime.UTC().Format(time.RFC3339)
	}
	return info
}

func documentInfo(doc *diploma.Document) models.DocumentInfo {
	profiles := doc.Profiles
	if profiles == nil {
		profiles = []string{}
	}
	return models.DocumentInfo{
		DocumentType:   doc.DocumentType,
		Qualification:  doc.Qualification,
		Profiles:       profiles,
		FullName:       doc.FullName,
		DateOfBirth:    formatDate(doc.DateOfBirth),
		Institution:    doc.Institution,
		PlaceOfIssue:   doc.PlaceOfIssue,
		DateAwarded:    formatDate(doc.DateAwarded),
		NlqfLevel:      doc.NLQFLevel,
		EqfLevel:       doc.EQFLevel,
		DocumentNumber: doc.DocumentNumber,
		DownloadDate:   formatDate(doc.DownloadDate),
		Pages:          doc.Pages,
		HasGradeList:   doc.HasGradeList,
	}
}

func HandleRedocRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(redocHTML); err != nil {
		slog.Error("failed to write redoc html", "error", err)
	}
}

func HandleSwaggerRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if _, err := w.Write(swaggerSpec); err != nil {
		slog.Error("failed to write swagger spec", "error", err)
	}
}

func GenerateSessionId() string {
	sessionId := make([]byte, 16)
	if _, err := rand.Read(sessionId); err != nil {
		slog.Error("failed to generate session ID", "error", err)
		return ""
	}
	return fmt.Sprintf("%x", sessionId)
}

// respondWithErr writes an ErrorResponse with the given key.
func respondWithErr(w http.ResponseWriter, code int, errorKey string, logMsg string, e error, extras ...any) {
	respondWithJSONErr(w, code, models.ErrorResponse{Error: errorKey, Message: logMsg}, logMsg, e, extras...)
}

func respondWithJSONErr(w http.ResponseWriter, code int, body models.ErrorResponse, logMsg string, e error, extras ...any) {
	args := []any{"error", e, "status_code", code, "error_key", body.Error}
	args = append(args, extras...)
	slog.Error(logMsg, args...)
	if err := writeJSON(w, code, body); err != nil {
		slog.Error("failed to write error response", "error", err)
	}
}

// helpers ------------

func closeRequestBody(r *http.Request) {
	if err := r.Body.Close(); err != nil {
		slog.Error("failed to close request body", "error", err)
	}
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		slog.Debug("Non-POST request rejected", "method", r.Method, "path", r.URL.Path)
		respondWithErr(w, http.StatusMethodNotAllowed, ErrorMethodNotAllowed, "method not allowed", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		slog.Error("Failed to marshal JSON payload", "error", err)
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err = w.Write(payload); err != nil {
		slog.Error("failed to write body to http response", "error", err)
	}
	return nil
}
