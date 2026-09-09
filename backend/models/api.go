// Package models contains the request and response bodies of the diploma
// issuer API.
package models

// ErrorResponse is returned on every failed request.
type ErrorResponse struct {
	// Stable machine readable error key, e.g. "error:validation-failed"
	Error string `json:"error" example:"error:validation-failed"`
	// Human readable explanation (English)
	Message string `json:"message,omitempty" example:"none of the uploaded files was accepted"`
	// Per-file outcome when an upload was refused because no file was accepted
	Files []FileResult `json:"files,omitempty"`
	// Identity comparison outcome when the error concerns the identity match
	Identity *IdentityMatchInfo `json:"identity,omitempty"`
}

// CheckInfo is one step of the signature verification.
type CheckInfo struct {
	// Stable identifier, e.g. certificate_chain
	Id string `json:"id" example:"certificate_chain"`
	// Short English label
	Name string `json:"name" example:"certificate chain to trust anchor"`
	Ok   bool   `json:"ok" example:"true"`
	// Technical detail of the outcome
	Detail string `json:"detail" example:"3 certificates, anchor \"Staat der Nederlanden Root CA - G3\""`
}

// ValidationInfo describes the cryptographic verification of the extract: the
// PAdES signature DUO put on the PDF, its timestamp and the certificate chain
// to an EU trusted list anchor.
type ValidationInfo struct {
	// True when every check passed: the PDF is authentic and unaltered
	Valid bool `json:"valid" example:"true"`
	// "valid", or the id of the first failed check
	Key string `json:"key" example:"valid"`
	// Subject of the signing certificate
	Signer string `json:"signer,omitempty" example:"CN=Dienst Uitvoering Onderwijs,O=Dienst Uitvoering Onderwijs (DUO),C=NL"`
	// Provenance of the trust anchor the signer chains to
	TrustAnchor string `json:"trust_anchor,omitempty" example:"EU trusted list NL: KPN B.V., service \"KPN BV PKIoverheid Organisatie Services CA - G3\" (CA/QC)"`
	// Time of signing, RFC 3339
	SigningTime string `json:"signing_time" example:"2026-09-03T12:34:56Z"`
	// True when the signing time comes from a trusted RFC 3161 timestamp
	TimestampTrusted bool        `json:"timestamp_trusted" example:"true"`
	Checks           []CheckInfo `json:"checks"`
}

// DocumentInfo is the data read from a diploma extract, echoed back so the
// user can check it before continuing.
type DocumentInfo struct {
	// "Diploma" or "Getuigschrift"
	DocumentType string `json:"document_type" example:"Diploma"`
	// Name of the qualification
	Qualification string `json:"qualification" example:"Hoger algemeen voortgezet onderwijs"`
	// Profiles, if printed
	Profiles []string `json:"profiles" example:"Natuur en Techniek"`
	// Holder as printed (given names and surname in one line)
	FullName string `json:"full_name" example:"Anna Maria van der Berg"`
	// Date of birth, YYYY-MM-DD
	DateOfBirth string `json:"date_of_birth" example:"1980-02-03"`
	// Institution that awarded the diploma
	Institution  string `json:"institution" example:"Hogeschool van Arnhem en Nijmegen"`
	PlaceOfIssue string `json:"place_of_issue" example:"Arnhem"`
	// Date the diploma was awarded, YYYY-MM-DD
	DateAwarded string `json:"date_awarded" example:"2009-07-01"`
	// NLQF level, if printed
	NlqfLevel string `json:"nlqf_level" example:"4"`
	// EQF level, if printed
	EqfLevel string `json:"eqf_level" example:"4"`
	// Number DUO prints in the footer of the extract
	DocumentNumber string `json:"document_number" example:"2896311"`
	// Day the extract was downloaded from DUO, YYYY-MM-DD
	DownloadDate string `json:"download_date" example:"2026-09-03"`
	Pages        int    `json:"pages" example:"1"`
	// True when the extract includes the list of grades
	HasGradeList bool `json:"has_grade_list" example:"false"`
}

// FileResult is the outcome for one uploaded file.
type FileResult struct {
	// File name as uploaded
	Filename string `json:"filename" example:"Hoger algemeen voortgezet onderwijs.pdf"`
	// True when the file was accepted into the session
	Accepted bool `json:"accepted" example:"true"`
	// Error key when the file was rejected, e.g. error:validation-failed
	Error string `json:"error,omitempty" example:"error:validation-failed"`
	// Human readable explanation when rejected (English)
	Message string `json:"message,omitempty" example:"the signature of the document could not be verified"`
	// Verification outcome, present as soon as the signature was checked
	Validation *ValidationInfo `json:"validation,omitempty"`
	// Parsed data, present when the file was accepted
	Document *DocumentInfo `json:"document,omitempty"`
}

// PersonInfo is the holder all accepted extracts have in common.
type PersonInfo struct {
	FullName string `json:"full_name" example:"Anna Maria van der Berg"`
	// Date of birth, YYYY-MM-DD
	DateOfBirth string `json:"date_of_birth" example:"1980-02-03"`
}

// UploadResponse is returned after at least one extract has been verified
// and parsed.
type UploadResponse struct {
	// Session identifier to use in the follow-up requests (32 hex characters)
	SessionId string `json:"session_id" example:"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"`
	// The holder named on the accepted extracts
	Person PersonInfo `json:"person"`
	// Outcome per uploaded file, in upload order
	Files []FileResult `json:"files"`
	// Number of accepted files (diplomas that will be issued)
	Accepted int `json:"accepted" example:"2"`
	// Number of rejected files
	Rejected int `json:"rejected" example:"0"`
}

// SessionRequest identifies the upload session for the follow-up steps.
type SessionRequest struct {
	// Session identifier from /diploma/upload
	SessionId string `json:"session_id" example:"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"`
}

// SessionPointer tells the Yivi app where to find the session (the IRMA "Qr"
// structure).
type SessionPointer struct {
	// Session URL on the IRMA server
	U string `json:"u" example:"https://irma.example.com/irma/session/abc123"`
	// Session type
	Irmaqr string `json:"irmaqr" example:"disclosing"`
}

// DisclosureSessionResponse is the IRMA session package for the identity
// disclosure, consumed directly by yivi-frontend.
type DisclosureSessionResponse struct {
	SessionPtr SessionPointer `json:"sessionPtr"`
	// Frontend session request (pairing options) as produced by the IRMA server
	FrontendRequest any `json:"frontendRequest" swaggertype:"object"`
}

// IdentityMatchInfo explains the comparison between the extracts and the
// disclosed identity.
type IdentityMatchInfo struct {
	// Which credential the identity came from: brp, passport, id_card or driving_licence
	Source           string   `json:"source" example:"passport"`
	Matched          bool     `json:"matched" example:"true"`
	DateOfBirthMatch bool     `json:"date_of_birth_match" example:"true"`
	SurnameMatch     bool     `json:"surname_match" example:"true"`
	GivenNamesMatch  bool     `json:"given_names_match" example:"true"`
	Reasons          []string `json:"reasons,omitempty" example:"surname differs"`
}

// IssuanceResponse contains the JWT and IRMA server URL for credential issuance
type IssuanceResponse struct {
	// Signed JWT containing the IRMA issuance request, one credential per diploma
	Jwt string `json:"jwt" example:"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."`
	// URL of the IRMA server for credential issuance
	IrmaServerURL string `json:"irma_server_url" example:"https://irma.example.com"`
	// Number of diploma credentials in the issuance request
	Credentials int `json:"credentials" example:"2"`
	// Outcome of the identity comparison that authorised the issuance
	Identity IdentityMatchInfo `json:"identity"`
}

// HealthResponse contains the health status of the service
type HealthResponse struct {
	// True if the service is healthy
	Ok bool `json:"ok" example:"true"`
}
