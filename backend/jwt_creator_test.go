package main

import (
	"os"
	"testing"
	"time"

	"go-diploma-issuer/diploma"

	"github.com/golang-jwt/jwt/v4"
	"github.com/privacybydesign/irmago/irma"
	"github.com/stretchr/testify/require"
)

var testIdentityCredentials = IdentityCredentials{
	Brp:            "irma-demo.gemeente.personalData",
	Passport:       "irma-demo.pbdf.passport",
	IdCard:         "irma-demo.pbdf.idcard",
	DrivingLicence: "irma-demo.pbdf.drivinglicence",
}

const testCredential = "irma-demo.pbdf.diploma"

func testDiploma() *diploma.Document {
	return &diploma.Document{
		DocumentType:   "Diploma",
		Qualification:  "Hoger algemeen voortgezet onderwijs",
		Profiles:       []string{"Natuur en Techniek"},
		FullName:       "Anna Maria van der Berg",
		DateOfBirth:    time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC),
		Institution:    "Johannes Fontanus College",
		PlaceOfIssue:   "Barneveld",
		DateAwarded:    time.Date(1998, 6, 12, 0, 0, 0, 0, time.UTC),
		NLQFLevel:      "4",
		EQFLevel:       "4",
		DownloadDate:   time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		DocumentNumber: "319221",
		Pages:          2,
		HasGradeList:   true,
	}
}

func testSecondDiploma() *diploma.Document {
	return &diploma.Document{
		DocumentType:   "Getuigschrift",
		Qualification:  "HBO Bachelor Informatica Propedeuse",
		Profiles:       []string{},
		FullName:       "Anna Maria van der Berg",
		DateOfBirth:    time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC),
		Institution:    "Hogeschool van Arnhem en Nijmegen",
		PlaceOfIssue:   "Arnhem",
		DateAwarded:    time.Date(1999, 7, 1, 0, 0, 0, 0, time.UTC),
		DownloadDate:   time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		DocumentNumber: "2896311",
		Pages:          1,
	}
}

func newTestJwtCreator(t *testing.T) *DefaultJwtCreator {
	t.Helper()
	creator, err := NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", testCredential, 25, 365*24*time.Hour, testIdentityCredentials)
	require.NoError(t, err)
	return creator
}

func publicKey(t *testing.T) any {
	t.Helper()
	keyBytes, err := os.ReadFile("test-secrets/pub.pem")
	require.NoError(t, err)
	key, err := jwt.ParseRSAPublicKeyFromPEM(keyBytes)
	require.NoError(t, err)
	return key
}

func TestNewIrmaJwtCreatorValidation(t *testing.T) {
	_, err := NewIrmaJwtCreator("test-secrets/does-not-exist.pem", "diploma_issuer", testCredential, 25, time.Hour, testIdentityCredentials)
	require.Error(t, err)

	_, err = NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", "diploma", 25, time.Hour, testIdentityCredentials)
	require.Error(t, err)

	broken := testIdentityCredentials
	broken.IdCard = ""
	_, err = NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", testCredential, 25, time.Hour, broken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "id_card")

	// BRP is optional: leaving it out is fine, a malformed value is not.
	withoutBrp := testIdentityCredentials
	withoutBrp.Brp = ""
	_, err = NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", testCredential, 25, time.Hour, withoutBrp)
	require.NoError(t, err)

	malformedBrp := testIdentityCredentials
	malformedBrp.Brp = "personalData"
	_, err = NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", testCredential, 25, time.Hour, malformedBrp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "brp")
}

func TestCreateDisclosureJwtWithoutBrp(t *testing.T) {
	withoutBrp := testIdentityCredentials
	withoutBrp.Brp = ""
	creator, err := NewIrmaJwtCreator("test-secrets/priv.pem", "diploma_issuer", testCredential, 25, time.Hour, withoutBrp)
	require.NoError(t, err)

	signed, err := creator.CreateDisclosureJwt()
	require.NoError(t, err)
	parsed, err := irma.ParseRequestorJwt("verification_request", signed)
	require.NoError(t, err)
	request := parsed.SessionRequest().(*irma.DisclosureRequest)

	require.Len(t, request.Disclose, 1)
	require.Len(t, request.Disclose[0], 3, "only the three document credentials are offered")
	require.Equal(t, "irma-demo.pbdf.passport.firstName", request.Disclose[0][0][0].Type.String())
	require.Equal(t, "irma-demo.pbdf.idcard.lastName", request.Disclose[0][1][1].Type.String())
	require.Equal(t, "irma-demo.pbdf.drivinglicence.dateOfBirth", request.Disclose[0][2][2].Type.String())
	for _, con := range request.Disclose[0] {
		for _, a := range con {
			require.NotContains(t, a.Type.String(), "gemeente", "no BRP attribute may be requested")
		}
	}
}

func TestCreateDisclosureJwt(t *testing.T) {
	creator := newTestJwtCreator(t)
	signed, err := creator.CreateDisclosureJwt()
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(signed, claims, func(token *jwt.Token) (any, error) { return publicKey(t), nil })
	require.NoError(t, err)
	require.Equal(t, "diploma_issuer", claims["iss"])
	require.Equal(t, "verification_request", claims["sub"])

	parsed, err := irma.ParseRequestorJwt("verification_request", signed)
	require.NoError(t, err)
	request := parsed.SessionRequest().(*irma.DisclosureRequest)

	require.Len(t, request.Disclose, 1, "one condiscon entry with four alternatives")
	require.Len(t, request.Disclose[0], 4)
	require.Equal(t, "irma-demo.gemeente.personalData.firstnames", request.Disclose[0][0][0].Type.String())
	require.Equal(t, "irma-demo.gemeente.personalData.dateofbirth", request.Disclose[0][0][3].Type.String())
	require.Equal(t, "irma-demo.pbdf.passport.firstName", request.Disclose[0][1][0].Type.String())
	require.Equal(t, "irma-demo.pbdf.idcard.lastName", request.Disclose[0][2][1].Type.String())
	require.Equal(t, "irma-demo.pbdf.drivinglicence.dateOfBirth", request.Disclose[0][3][2].Type.String())
	require.Equal(t, "Je identiteit", request.Labels[0]["nl"])
}

func TestCreateIssuanceJwt(t *testing.T) {
	creator := newTestJwtCreator(t)
	signed, err := creator.CreateIssuanceJwt([]*diploma.Document{testDiploma(), testSecondDiploma()}, SourcePassport)
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(signed, claims, func(token *jwt.Token) (any, error) { return publicKey(t), nil })
	require.NoError(t, err)
	require.Equal(t, "diploma_issuer", claims["iss"])
	require.Equal(t, "issue_request", claims["sub"])

	parsed, err := irma.ParseRequestorJwt("issue_request", signed)
	require.NoError(t, err)
	request := parsed.SessionRequest().(*irma.IssuanceRequest)
	require.Len(t, request.Credentials, 2, "one credential per diploma")

	for _, credential := range request.Credentials {
		require.Equal(t, testCredential, credential.CredentialTypeID.String())
		require.Equal(t, uint(25), credential.SdJwtBatchSize)
		require.NotNil(t, credential.Validity)
		validity := time.Time(*credential.Validity)
		require.WithinDuration(t, time.Now().Add(365*24*time.Hour), validity, time.Minute)
		require.Len(t, credential.Attributes, 14)
	}

	attributes := request.Credentials[0].Attributes
	require.Equal(t, "Diploma", attributes["documentType"])
	require.Equal(t, "Hoger algemeen voortgezet onderwijs", attributes["qualification"])
	require.Equal(t, "Natuur en Techniek", attributes["profile"])
	require.Equal(t, "Anna Maria van der Berg", attributes["fullName"])
	require.Equal(t, "1980-02-03", attributes["dateOfBirth"])
	require.Equal(t, "Johannes Fontanus College", attributes["institution"])
	require.Equal(t, "Barneveld", attributes["placeOfIssue"])
	require.Equal(t, "1998-06-12", attributes["dateAwarded"])
	require.Equal(t, "4", attributes["nlqfLevel"])
	require.Equal(t, "4", attributes["eqfLevel"])
	require.Equal(t, "319221", attributes["documentNumber"])
	require.Equal(t, "2026-09-03", attributes["downloadDate"])
	require.Equal(t, "yes", attributes["gradeList"])
	require.Equal(t, "passport", attributes["identitySource"])

	second := request.Credentials[1].Attributes
	require.Equal(t, "Getuigschrift", second["documentType"])
	require.Equal(t, "HBO Bachelor Informatica Propedeuse", second["qualification"])
	require.Equal(t, "", second["profile"])
	require.Equal(t, "", second["nlqfLevel"], "a level that is not printed stays empty")
	require.Equal(t, "no", second["gradeList"])
	require.Equal(t, "2896311", second["documentNumber"])
}

func TestCreateIssuanceJwtRequiresDocuments(t *testing.T) {
	creator := newTestJwtCreator(t)
	_, err := creator.CreateIssuanceJwt(nil, SourceBrp)
	require.Error(t, err)
	_, err = creator.CreateIssuanceJwt([]*diploma.Document{nil}, SourceBrp)
	require.Error(t, err)
}

func TestIdentityCredentialsSourceOf(t *testing.T) {
	require.Equal(t, SourceBrp, testIdentityCredentials.SourceOf("irma-demo.gemeente.personalData"))
	require.Equal(t, SourcePassport, testIdentityCredentials.SourceOf("irma-demo.pbdf.passport"))
	require.Equal(t, SourceIdCard, testIdentityCredentials.SourceOf("irma-demo.pbdf.idcard"))
	require.Equal(t, SourceDrivingLicence, testIdentityCredentials.SourceOf("irma-demo.pbdf.drivinglicence"))
	require.Equal(t, "", testIdentityCredentials.SourceOf("irma-demo.pbdf.email"))

	withoutBrp := testIdentityCredentials
	withoutBrp.Brp = ""
	require.Equal(t, "", withoutBrp.SourceOf("irma-demo.gemeente.personalData"), "BRP is not an identity source when not configured")
	require.Equal(t, "", withoutBrp.SourceOf(""), "an empty credential id must not match the empty BRP setting")
	require.Equal(t, SourcePassport, withoutBrp.SourceOf("irma-demo.pbdf.passport"))
}
