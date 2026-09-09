package main

import (
	"crypto/rsa"
	"fmt"
	"os"
	"strings"
	"time"

	"go-diploma-issuer/diploma"

	"github.com/golang-jwt/jwt/v4"
	"github.com/privacybydesign/irmago/irma"
)

const DATE_FORMAT_CYMD = "2006-01-02"

// Identity sources the user can choose from when proving who they are.
const (
	SourceBrp            = "brp"
	SourcePassport       = "passport"
	SourceIdCard         = "id_card"
	SourceDrivingLicence = "driving_licence"
)

// IdentityCredentials holds the full credential type identifiers of the
// identity credentials a user may disclose, e.g.
// "pbdf-staging.gemeente.personalData". The attribute names within each
// credential are fixed by the Yivi scheme and hard coded below.
//
// The three document credentials are mandatory. BRP is optional: when it is
// left out of the configuration the disclosure request does not offer it and a
// disclosed BRP credential is not recognised as an identity.
type IdentityCredentials struct {
	Brp            string `json:"brp,omitempty"`
	Passport       string `json:"passport"`
	IdCard         string `json:"id_card"`
	DrivingLicence string `json:"driving_licence"`
}

// Attribute names per identity credential, as defined in the pbdf scheme.
const (
	BrpAttrFirstNames  = "firstnames"
	BrpAttrPrefix      = "prefix"
	BrpAttrFamilyName  = "familyname"
	BrpAttrDateOfBirth = "dateofbirth"

	DocAttrFirstName   = "firstName"
	DocAttrLastName    = "lastName"
	DocAttrDateOfBirth = "dateOfBirth"
)

// Validate checks that the three document credential identifiers are
// configured and that BRP, when configured, is a full identifier too.
func (ic IdentityCredentials) Validate() error {
	for name, value := range map[string]string{
		"passport":        ic.Passport,
		"id_card":         ic.IdCard,
		"driving_licence": ic.DrivingLicence,
	} {
		if strings.Count(value, ".") != 2 {
			return fmt.Errorf("identity credential %s must be a full credential identifier (scheme.issuer.credential), got %q", name, value)
		}
	}
	if ic.HasBrp() && strings.Count(ic.Brp, ".") != 2 {
		return fmt.Errorf("identity credential brp must be a full credential identifier (scheme.issuer.credential) or be left out, got %q", ic.Brp)
	}
	return nil
}

// HasBrp reports whether a BRP credential is configured.
func (ic IdentityCredentials) HasBrp() bool {
	return strings.TrimSpace(ic.Brp) != ""
}

// SourceOf returns the identity source a full credential identifier belongs
// to, or "" when it is none of the configured ones.
func (ic IdentityCredentials) SourceOf(credential string) string {
	if credential == "" {
		return ""
	}
	switch credential {
	case ic.Brp:
		return SourceBrp
	case ic.Passport:
		return SourcePassport
	case ic.IdCard:
		return SourceIdCard
	case ic.DrivingLicence:
		return SourceDrivingLicence
	}
	return ""
}

type JwtCreator interface {
	// CreateDisclosureJwt signs the request asking the user to disclose their
	// identity from one of the configured identity credentials.
	CreateDisclosureJwt() (jwt string, err error)
	// CreateIssuanceJwt signs the request issuing one diploma credential per
	// extract, all in a single issuance session.
	CreateIssuanceJwt(docs []*diploma.Document, identitySource string) (jwt string, err error)
}

func NewIrmaJwtCreator(privateKeyPath string,
	issuerId string,
	credential string,
	sdJwtBatchSize uint,
	validity time.Duration,
	identity IdentityCredentials,
) (*DefaultJwtCreator, error) {
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return nil, err
	}

	if strings.Count(credential, ".") != 2 {
		return nil, fmt.Errorf("credential must be a full credential identifier (scheme.issuer.credential), got %q", credential)
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}

	return &DefaultJwtCreator{
		issuerId:       issuerId,
		privateKey:     privateKey,
		credential:     credential,
		sdJwtBatchSize: sdJwtBatchSize,
		validity:       validity,
		identity:       identity,
	}, nil
}

type DefaultJwtCreator struct {
	privateKey     *rsa.PrivateKey
	issuerId       string
	credential     string
	sdJwtBatchSize uint
	validity       time.Duration
	identity       IdentityCredentials
}

func (jc *DefaultJwtCreator) sign(request irma.SessionRequest) (string, error) {
	return irma.SignSessionRequest(
		request,
		jwt.GetSigningMethod(jwt.SigningMethodRS256.Alg()),
		jc.privateKey,
		jc.issuerId,
	)
}

func (jc *DefaultJwtCreator) CreateDisclosureJwt() (string, error) {
	return jc.sign(jc.createDisclosureRequest())
}

func (jc *DefaultJwtCreator) CreateIssuanceJwt(docs []*diploma.Document, identitySource string) (string, error) {
	if len(docs) == 0 {
		return "", fmt.Errorf("no diploma to issue")
	}
	attributes := make([]map[string]string, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			return "", fmt.Errorf("no diploma to issue")
		}
		attributes = append(attributes, DiplomaAttributes(doc, identitySource))
	}
	return jc.sign(jc.createIssuanceRequest(attributes))
}

func attr(credential, name string) irma.AttributeRequest {
	return irma.AttributeRequest{Type: irma.NewAttributeTypeIdentifier(credential + "." + name)}
}

// createDisclosureRequest builds a condiscon with one conjunction per
// configured identity source; the Yivi app lets the user pick whichever
// credential they hold. The BRP conjunction is only offered when a BRP
// credential is configured.
func (jc *DefaultJwtCreator) createDisclosureRequest() *irma.DisclosureRequest {
	alternatives := irma.AttributeDisCon{}
	if jc.identity.HasBrp() {
		alternatives = append(alternatives, irma.AttributeCon{
			attr(jc.identity.Brp, BrpAttrFirstNames),
			attr(jc.identity.Brp, BrpAttrPrefix),
			attr(jc.identity.Brp, BrpAttrFamilyName),
			attr(jc.identity.Brp, BrpAttrDateOfBirth),
		})
	}
	for _, document := range []string{jc.identity.Passport, jc.identity.IdCard, jc.identity.DrivingLicence} {
		alternatives = append(alternatives, irma.AttributeCon{
			attr(document, DocAttrFirstName),
			attr(document, DocAttrLastName),
			attr(document, DocAttrDateOfBirth),
		})
	}

	request := irma.NewDisclosureRequest()
	request.Disclose = irma.AttributeConDisCon{alternatives}
	request.Labels = map[int]irma.TranslatedString{
		0: {"en": "Your identity", "nl": "Je identiteit"},
	}
	return request
}

// createIssuanceRequest creates an IRMA issuance request with one diploma
// credential per attribute set.
func (jc *DefaultJwtCreator) createIssuanceRequest(attributeSets []map[string]string) *irma.IssuanceRequest {
	validity := irma.Timestamp(time.Unix(time.Now().Add(jc.validity).Unix(), 0))

	credentials := make([]*irma.CredentialRequest, 0, len(attributeSets))
	for _, attributes := range attributeSets {
		credentials = append(credentials, &irma.CredentialRequest{
			CredentialTypeID: irma.NewCredentialTypeIdentifier(jc.credential),
			Attributes:       attributes,
			SdJwtBatchSize:   jc.sdJwtBatchSize,
			Validity:         &validity,
		})
	}
	return irma.NewIssuanceRequest(credentials)
}

// yesNo renders a boolean the way the scheme's yesno display hint expects.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// formatDate renders a date as YYYY-MM-DD, or "" for the zero time (a field
// the extract does not print).
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(DATE_FORMAT_CYMD)
}

// DiplomaAttributes maps a parsed extract onto the attributes of the diploma
// credential. The attribute names must stay in sync with
// docs/scheme/diploma/description.xml.
func DiplomaAttributes(doc *diploma.Document, identitySource string) map[string]string {
	return map[string]string{
		"documentType":   doc.DocumentType,
		"qualification":  doc.Qualification,
		"profile":        doc.ProfilesString(),
		"fullName":       doc.FullName,
		"dateOfBirth":    formatDate(doc.DateOfBirth),
		"institution":    doc.Institution,
		"placeOfIssue":   doc.PlaceOfIssue,
		"dateAwarded":    formatDate(doc.DateAwarded),
		"nlqfLevel":      doc.NLQFLevel,
		"eqfLevel":       doc.EQFLevel,
		"documentNumber": doc.DocumentNumber,
		"downloadDate":   formatDate(doc.DownloadDate),
		"gradeList":      yesNo(doc.HasGradeList),
		"identitySource": identitySource,
	}
}
