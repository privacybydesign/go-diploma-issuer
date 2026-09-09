package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"go-diploma-issuer/diploma"
	"go-diploma-issuer/diploma/verify"
	"go-diploma-issuer/models"

	"github.com/privacybydesign/irmago/irma"
	"github.com/stretchr/testify/require"
)

const (
	uploadEndpoint     = "/api/diploma/upload"
	disclosureEndpoint = "/api/diploma/start-disclosure"
	issueEndpoint      = "/api/diploma/issue"
)

var (
	fakePdf       = []byte("%PDF-1.5 fake diploma content\n%%EOF\n")
	fakeSecondPdf = []byte("%PDF-1.5 fake second diploma content\n%%EOF\n")
	fakeThirdPdf  = []byte("%PDF-1.5 fake third diploma content\n%%EOF\n")
)

func uploadOk(t *testing.T) *models.UploadResponse {
	t.Helper()
	resp, body, upload := postFile[models.UploadResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file", "diploma.pdf", fakePdf)
	mustStatus(t, resp, http.StatusOK, body)
	require.NotEmpty(t, upload.SessionId)
	return upload
}

func startDisclosureOk(t *testing.T, sessionId string) {
	t.Helper()
	resp, body, _ := postJSON[models.DisclosureSessionResponse](t, fmt.Sprintf(testHost, disclosureEndpoint), models.SessionRequest{SessionId: sessionId})
	mustStatus(t, resp, http.StatusOK, body)
}

func TestHealth(t *testing.T) {
	startTestServer(t, defaultDeps())
	resp, err := http.Get(fmt.Sprintf(testHost, "/api/health"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUploadHappyPath(t *testing.T) {
	deps := defaultDeps()
	startTestServer(t, deps)

	upload := uploadOk(t)
	require.Equal(t, 1, upload.Accepted)
	require.Equal(t, 0, upload.Rejected)
	require.Equal(t, "Anna Maria van der Berg", upload.Person.FullName)
	require.Equal(t, "1980-02-03", upload.Person.DateOfBirth)
	require.Len(t, upload.Files, 1)

	file := upload.Files[0]
	require.True(t, file.Accepted)
	require.Equal(t, "diploma.pdf", file.Filename)
	require.Empty(t, file.Error)
	require.NotNil(t, file.Validation)
	require.True(t, file.Validation.Valid)
	require.Equal(t, "valid", file.Validation.Key)
	require.True(t, file.Validation.TimestampTrusted)
	require.Equal(t, "2026-09-03T10:00:00Z", file.Validation.SigningTime)
	require.Contains(t, file.Validation.Signer, "Dienst Uitvoering Onderwijs")
	require.Len(t, file.Validation.Checks, 3)
	require.Equal(t, verify.CheckSignaturePresent, file.Validation.Checks[0].Id)

	require.NotNil(t, file.Document)
	require.Equal(t, "Diploma", file.Document.DocumentType)
	require.Equal(t, "Hoger algemeen voortgezet onderwijs", file.Document.Qualification)
	require.Equal(t, []string{"Natuur en Techniek"}, file.Document.Profiles)
	require.Equal(t, "Anna Maria van der Berg", file.Document.FullName)
	require.Equal(t, "1980-02-03", file.Document.DateOfBirth)
	require.Equal(t, "Johannes Fontanus College", file.Document.Institution)
	require.Equal(t, "Barneveld", file.Document.PlaceOfIssue)
	require.Equal(t, "1998-06-12", file.Document.DateAwarded)
	require.Equal(t, "4", file.Document.NlqfLevel)
	require.Equal(t, "319221", file.Document.DocumentNumber)
	require.Equal(t, "2026-09-03", file.Document.DownloadDate)
	require.True(t, file.Document.HasGradeList)

	require.Equal(t, 1, deps.validator.calls)
	require.Equal(t, fakePdf, deps.validator.last)

	session, err := deps.storage.Retrieve(upload.SessionId)
	require.NoError(t, err)
	require.Equal(t, StageValidated, session.Stage)
	require.Len(t, session.Documents, 1)
	require.Equal(t, "319221", session.Documents[0].DocumentNumber)
}

func TestUploadMultipleDiplomas(t *testing.T) {
	deps := defaultDeps()
	deps.parser.overrides = map[string]*diploma.Document{string(fakeSecondPdf): testSecondDiploma()}
	startTestServer(t, deps)

	resp, body, upload := postFiles[models.UploadResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file",
		upload{"havo.pdf", fakePdf}, upload{"propedeuse.pdf", fakeSecondPdf})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, 2, upload.Accepted)
	require.Equal(t, 0, upload.Rejected)
	require.Len(t, upload.Files, 2)
	require.Equal(t, "havo.pdf", upload.Files[0].Filename)
	require.Equal(t, "propedeuse.pdf", upload.Files[1].Filename)
	require.Equal(t, "HBO Bachelor Informatica Propedeuse", upload.Files[1].Document.Qualification)
	require.Equal(t, 2, deps.validator.calls)

	session, err := deps.storage.Retrieve(upload.SessionId)
	require.NoError(t, err)
	require.Len(t, session.Documents, 2)
	require.Equal(t, "319221", session.Documents[0].DocumentNumber)
	require.Equal(t, "2896311", session.Documents[1].DocumentNumber)
}

func TestUploadPartiallyRejected(t *testing.T) {
	otherPerson := testSecondDiploma()
	otherPerson.FullName = "Piet Jansen"

	deps := defaultDeps()
	deps.validator.overrides = map[string]*diploma.Verification{string(fakeSecondPdf): invalidVerification(verify.CheckCoversWholeFile)}
	deps.parser.overrides = map[string]*diploma.Document{string(fakeThirdPdf): otherPerson}
	startTestServer(t, deps)

	resp, body, upload := postFiles[models.UploadResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file",
		upload{"good.pdf", fakePdf}, upload{"tampered.pdf", fakeSecondPdf}, upload{"someone-else.pdf", fakeThirdPdf})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, 1, upload.Accepted)
	require.Equal(t, 2, upload.Rejected)
	require.Len(t, upload.Files, 3)

	require.True(t, upload.Files[0].Accepted)

	tampered := upload.Files[1]
	require.False(t, tampered.Accepted)
	require.Equal(t, ErrorValidationFailed, tampered.Error)
	require.NotNil(t, tampered.Validation)
	require.False(t, tampered.Validation.Valid)
	require.Equal(t, verify.CheckCoversWholeFile, tampered.Validation.Key)
	require.Nil(t, tampered.Document, "data of a rejected file is not returned")

	other := upload.Files[2]
	require.False(t, other.Accepted)
	require.Equal(t, ErrorDifferentPerson, other.Error)
	require.Nil(t, other.Document)

	session, err := deps.storage.Retrieve(upload.SessionId)
	require.NoError(t, err)
	require.Len(t, session.Documents, 1, "only the accepted extract is in the session")
}

func TestUploadDuplicateDiploma(t *testing.T) {
	deps := defaultDeps()
	startTestServer(t, deps)

	resp, body, upload := postFiles[models.UploadResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file",
		upload{"havo.pdf", fakePdf}, upload{"havo-again.pdf", fakePdf})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, 1, upload.Accepted)
	require.Equal(t, ErrorDuplicateDiploma, upload.Files[1].Error)
}

func TestUploadAllRejected(t *testing.T) {
	deps := defaultDeps()
	deps.validator.valid = false
	startTestServer(t, deps)

	resp, body, errResp := postFiles[models.ErrorResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file",
		upload{"a.pdf", fakePdf}, upload{"b.pdf", fakeSecondPdf})
	mustStatus(t, resp, http.StatusUnprocessableEntity, body)
	require.Equal(t, ErrorValidationFailed, errResp.Error)
	require.Len(t, errResp.Files, 2)
	for _, file := range errResp.Files {
		require.False(t, file.Accepted)
		require.Equal(t, ErrorValidationFailed, file.Error)
		require.Equal(t, verify.CheckCertificateChain, file.Validation.Key)
	}
	require.Equal(t, 2, deps.validator.calls)
}

func TestUploadRejectsBadRequests(t *testing.T) {
	deps := defaultDeps()
	startTestServer(t, deps)
	url := fmt.Sprintf(testHost, uploadEndpoint)

	t.Run("missing file field", func(t *testing.T) {
		resp, body, errResp := postFile[models.ErrorResponse](t, url, "", "", nil)
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorFileMissing, errResp.Error)
	})

	t.Run("not a pdf", func(t *testing.T) {
		resp, body, errResp := postFile[models.ErrorResponse](t, url, "file", "x.txt", []byte("hello"))
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorNotAPdf, errResp.Error)
		require.Len(t, errResp.Files, 1)
		require.Equal(t, ErrorNotAPdf, errResp.Files[0].Error)
	})

	t.Run("pdf header without trailer", func(t *testing.T) {
		resp, body, errResp := postFile[models.ErrorResponse](t, url, "file", "x.pdf", []byte("%PDF-1.5\nnot really a pdf"))
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorNotAPdf, errResp.Error)
	})

	t.Run("wrong extension", func(t *testing.T) {
		resp, body, errResp := postFile[models.ErrorResponse](t, url, "file", "x.exe", []byte("%PDF-1.5\n%%EOF\n"))
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorNotAPdf, errResp.Error)
	})

	t.Run("wrong content type", func(t *testing.T) {
		resp, body, errResp := postFileWithType[models.ErrorResponse](t, url, "x.pdf", "image/png", []byte("%PDF-1.5\n%%EOF\n"))
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorNotAPdf, errResp.Error)
	})

	t.Run("one file too large", func(t *testing.T) {
		big := append([]byte("%PDF"), make([]byte, 2<<20)...)
		resp, body, errResp := postFile[models.ErrorResponse](t, url, "file", "big.pdf", big)
		mustStatus(t, resp, http.StatusRequestEntityTooLarge, body)
		require.Equal(t, ErrorFileTooLarge, errResp.Error)
	})

	t.Run("too many files", func(t *testing.T) {
		resp, body, errResp := postFiles[models.ErrorResponse](t, url, "file",
			upload{"1.pdf", fakePdf}, upload{"2.pdf", fakePdf}, upload{"3.pdf", fakePdf}, upload{"4.pdf", fakePdf})
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorTooManyFiles, errResp.Error)
	})

	t.Run("get not allowed", func(t *testing.T) {
		resp, err := http.Get(url)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	})

	// The validator must never have been called for these.
	require.Equal(t, 0, deps.validator.calls)
}

func TestUploadTrustUnavailable(t *testing.T) {
	deps := defaultDeps()
	deps.validator.err = diploma.ErrTrustUnavailable
	startTestServer(t, deps)

	resp, body, errResp := postFile[models.ErrorResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file", "diploma.pdf", fakePdf)
	mustStatus(t, resp, http.StatusServiceUnavailable, body)
	require.Equal(t, ErrorValidationService, errResp.Error)
}

func TestUploadValidatorFailure(t *testing.T) {
	deps := defaultDeps()
	deps.validator.err = errors.New("boom")
	startTestServer(t, deps)

	resp, body, errResp := postFile[models.ErrorResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file", "diploma.pdf", fakePdf)
	mustStatus(t, resp, http.StatusInternalServerError, body)
	require.Equal(t, ErrorInternal, errResp.Error)
}

func TestUploadNotADiploma(t *testing.T) {
	deps := defaultDeps()
	deps.parser = fakeParser{err: fmt.Errorf("%w: register heading not found", diploma.ErrNotADiploma)}
	startTestServer(t, deps)

	resp, body, errResp := postFile[models.ErrorResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file", "diploma.pdf", fakePdf)
	mustStatus(t, resp, http.StatusBadRequest, body)
	require.Equal(t, ErrorNotADiploma, errResp.Error)
	require.Len(t, errResp.Files, 1)
	require.NotNil(t, errResp.Files[0].Validation, "the signature outcome is still reported")
	require.True(t, errResp.Files[0].Validation.Valid)
}

func TestStartDisclosure(t *testing.T) {
	deps := defaultDeps()
	startTestServer(t, deps)
	upload := uploadOk(t)

	resp, body, pkg := postJSON[models.DisclosureSessionResponse](t, fmt.Sprintf(testHost, disclosureEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, "https://irma.example/irma/session/xyz", pkg.SessionPtr.U)
	require.Equal(t, "disclosing", pkg.SessionPtr.Irmaqr)
	require.NotNil(t, pkg.FrontendRequest)
	// The requestor token stays server side.
	require.NotContains(t, string(body), `"token"`)
	require.Equal(t, "disclosure-jwt", deps.irma.startJwt)

	session, err := deps.storage.Retrieve(upload.SessionId)
	require.NoError(t, err)
	require.Equal(t, StageDisclosing, session.Stage)
	require.Equal(t, "tok", session.IrmaToken)
}

func TestStartDisclosureErrors(t *testing.T) {
	t.Run("unknown session", func(t *testing.T) {
		startTestServer(t, defaultDeps())
		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, disclosureEndpoint), models.SessionRequest{SessionId: "nope"})
		mustStatus(t, resp, http.StatusNotFound, body)
		require.Equal(t, ErrorUnknownSession, errResp.Error)
	})

	t.Run("missing session id", func(t *testing.T) {
		startTestServer(t, defaultDeps())
		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, disclosureEndpoint), map[string]string{})
		mustStatus(t, resp, http.StatusBadRequest, body)
		require.Equal(t, ErrorInvalidRequest, errResp.Error)
	})

	t.Run("irma server down", func(t *testing.T) {
		deps := defaultDeps()
		deps.irma.startErr = errors.New("irma down")
		startTestServer(t, deps)
		upload := uploadOk(t)
		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, disclosureEndpoint), models.SessionRequest{SessionId: upload.SessionId})
		mustStatus(t, resp, http.StatusBadGateway, body)
		require.Equal(t, ErrorIrmaServer, errResp.Error)
	})
}

func TestIssueHappyPath(t *testing.T) {
	deps := defaultDeps()
	deps.parser.overrides = map[string]*diploma.Document{string(fakeSecondPdf): testSecondDiploma()}
	startTestServer(t, deps)

	resp, body, upload := postFiles[models.UploadResponse](t, fmt.Sprintf(testHost, uploadEndpoint), "file",
		upload{"havo.pdf", fakePdf}, upload{"propedeuse.pdf", fakeSecondPdf})
	mustStatus(t, resp, http.StatusOK, body)
	startDisclosureOk(t, upload.SessionId)

	resp, body, issuance := postJSON[models.IssuanceResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, "issuance-jwt", issuance.Jwt)
	require.Equal(t, "https://irma.example", issuance.IrmaServerURL)
	require.Equal(t, 2, issuance.Credentials)
	require.True(t, issuance.Identity.Matched)
	require.Equal(t, SourcePassport, issuance.Identity.Source)

	require.Equal(t, irma.RequestorToken("tok"), deps.irma.asked)
	require.Len(t, deps.jwt.issuedDocs, 2, "both diplomas are issued at once")
	require.Equal(t, "319221", deps.jwt.issuedDocs[0].DocumentNumber)
	require.Equal(t, "2896311", deps.jwt.issuedDocs[1].DocumentNumber)
	require.Equal(t, SourcePassport, deps.jwt.issuedSource)

	// The session is consumed: a second issuance is not possible.
	_, err := deps.storage.Retrieve(upload.SessionId)
	require.Error(t, err)
	resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusNotFound, body)
	require.Equal(t, ErrorUnknownSession, errResp.Error)
}

func TestIssueIdentityMismatchAllowsRetry(t *testing.T) {
	deps := defaultDeps()
	deps.irma.result = validDisclosure("PIET", "JANSEN", "1975-01-01")
	startTestServer(t, deps)
	upload := uploadOk(t)
	startDisclosureOk(t, upload.SessionId)

	resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusForbidden, body)
	require.Equal(t, ErrorIdentityMismatch, errResp.Error)
	require.NotNil(t, errResp.Identity)
	require.False(t, errResp.Identity.Matched)
	require.False(t, errResp.Identity.DateOfBirthMatch)
	require.False(t, errResp.Identity.SurnameMatch)
	require.False(t, errResp.Identity.GivenNamesMatch)
	require.Equal(t, SourcePassport, errResp.Identity.Source)
	require.Nil(t, deps.jwt.issuedDocs, "nothing may be issued on a mismatch")

	// The extracts stay, the spent disclosure is forgotten...
	session, err := deps.storage.Retrieve(upload.SessionId)
	require.NoError(t, err)
	require.Equal(t, StageValidated, session.Stage)
	require.Empty(t, session.IrmaToken)
	require.Len(t, session.Documents, 1)

	// ...so issuing without a new disclosure is refused...
	resp, body, errResp = postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusConflict, body)
	require.Equal(t, ErrorDisclosureNotDone, errResp.Error)

	// ...and a new, matching disclosure succeeds.
	deps.irma.result = validDisclosure("Anna", "van der Berg", "03-02-1980")
	startDisclosureOk(t, upload.SessionId)
	resp, body, _ = postJSON[models.IssuanceResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusOK, body)
}

func TestIssueDisclosureStates(t *testing.T) {
	t.Run("not finished", func(t *testing.T) {
		deps := defaultDeps()
		deps.irma.result.Status = irma.ServerStatusConnected
		startTestServer(t, deps)
		upload := uploadOk(t)
		startDisclosureOk(t, upload.SessionId)

		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
		mustStatus(t, resp, http.StatusConflict, body)
		require.Equal(t, ErrorDisclosureNotDone, errResp.Error)

		// Still waiting: the disclosure session is kept for polling.
		session, err := deps.storage.Retrieve(upload.SessionId)
		require.NoError(t, err)
		require.Equal(t, StageDisclosing, session.Stage)
	})

	t.Run("invalid proof", func(t *testing.T) {
		deps := defaultDeps()
		deps.irma.result.ProofStatus = irma.ProofStatusInvalid
		startTestServer(t, deps)
		upload := uploadOk(t)
		startDisclosureOk(t, upload.SessionId)

		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
		mustStatus(t, resp, http.StatusForbidden, body)
		require.Equal(t, ErrorDisclosureInvalid, errResp.Error)
		require.Nil(t, deps.jwt.issuedDocs)
	})

	t.Run("irma server down", func(t *testing.T) {
		deps := defaultDeps()
		deps.irma.resultErr = errors.New("irma down")
		startTestServer(t, deps)
		upload := uploadOk(t)
		startDisclosureOk(t, upload.SessionId)

		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
		mustStatus(t, resp, http.StatusBadGateway, body)
		require.Equal(t, ErrorIrmaServer, errResp.Error)
	})

	t.Run("disclosure never started", func(t *testing.T) {
		deps := defaultDeps()
		startTestServer(t, deps)
		upload := uploadOk(t)

		resp, body, errResp := postJSON[models.ErrorResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
		mustStatus(t, resp, http.StatusConflict, body)
		require.Equal(t, ErrorDisclosureNotDone, errResp.Error)
	})
}

func TestIssueWithBrpDisclosure(t *testing.T) {
	deps := defaultDeps()
	deps.irma.result.Disclosed = [][]*irma.DisclosedAttribute{{
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrFirstNames, "Anna Maria"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrPrefix, "van der"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrFamilyName, "Berg"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrDateOfBirth, "03-02-1980"),
	}}
	startTestServer(t, deps)
	upload := uploadOk(t)
	startDisclosureOk(t, upload.SessionId)

	resp, body, issuance := postJSON[models.IssuanceResponse](t, fmt.Sprintf(testHost, issueEndpoint), models.SessionRequest{SessionId: upload.SessionId})
	mustStatus(t, resp, http.StatusOK, body)
	require.Equal(t, SourceBrp, issuance.Identity.Source)
	require.Equal(t, SourceBrp, deps.jwt.issuedSource)
}

func TestMatchDocumentsRequiresAll(t *testing.T) {
	disclosed := validDisclosure("Anna", "van der Berg", "1980-02-03")
	person, err := ExtractIdentity(disclosed.Disclosed, testIdentityCredentials)
	require.NoError(t, err)

	other := testSecondDiploma()
	other.FullName = "Piet Jansen"
	require.True(t, matchDocuments([]*diploma.Document{testDiploma(), testSecondDiploma()}, person.Person).Matched)
	require.False(t, matchDocuments([]*diploma.Document{testDiploma(), other}, person.Person).Matched)
	require.False(t, matchDocuments(nil, person.Person).Matched)
}
