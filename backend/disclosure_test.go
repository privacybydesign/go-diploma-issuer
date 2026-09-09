package main

import (
	"testing"

	"github.com/privacybydesign/irmago/irma"
	"github.com/stretchr/testify/require"
)

func disclosedAttr(id, value string) *irma.DisclosedAttribute {
	v := value
	return &irma.DisclosedAttribute{
		RawValue:   &v,
		Identifier: irma.NewAttributeTypeIdentifier(id),
		Status:     irma.AttributeProofStatusPresent,
	}
}

func TestExtractIdentityIgnoresBrpWhenNotConfigured(t *testing.T) {
	withoutBrp := testIdentityCredentials
	withoutBrp.Brp = ""
	disclosed := [][]*irma.DisclosedAttribute{{
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrFirstNames, "Anna Maria"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrPrefix, "van der"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrFamilyName, "Berg"),
		disclosedAttr(testIdentityCredentials.Brp+"."+BrpAttrDateOfBirth, "03-02-1980"),
	}}
	_, err := ExtractIdentity(disclosed, withoutBrp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no identity credential")
}

func TestExtractIdentityBrp(t *testing.T) {
	disclosed := [][]*irma.DisclosedAttribute{{
		disclosedAttr("irma-demo.gemeente.personalData.firstnames", "Anna Maria"),
		disclosedAttr("irma-demo.gemeente.personalData.prefix", "van der"),
		disclosedAttr("irma-demo.gemeente.personalData.familyname", "Berg"),
		disclosedAttr("irma-demo.gemeente.personalData.dateofbirth", "03-02-1980"),
	}}
	id, err := ExtractIdentity(disclosed, testIdentityCredentials)
	require.NoError(t, err)
	require.Equal(t, SourceBrp, id.Source)
	require.Equal(t, "Anna Maria", id.Person.GivenNames)
	require.Equal(t, "van der", id.Person.Prefix)
	require.Equal(t, "Berg", id.Person.Surname)
	require.Equal(t, "03-02-1980", id.Person.DateOfBirth)
}

func TestExtractIdentityPassportAndFriends(t *testing.T) {
	for credential, source := range map[string]string{
		"irma-demo.pbdf.passport":       SourcePassport,
		"irma-demo.pbdf.idcard":         SourceIdCard,
		"irma-demo.pbdf.drivinglicence": SourceDrivingLicence,
	} {
		disclosed := [][]*irma.DisclosedAttribute{{
			disclosedAttr(credential+".firstName", "ANNA MARIA"),
			disclosedAttr(credential+".lastName", "VAN DER BERG"),
			disclosedAttr(credential+".dateOfBirth", "1980-02-03"),
		}}
		id, err := ExtractIdentity(disclosed, testIdentityCredentials)
		require.NoError(t, err, credential)
		require.Equal(t, source, id.Source)
		require.Equal(t, "ANNA MARIA", id.Person.GivenNames)
		require.Equal(t, "VAN DER BERG", id.Person.Surname)
		require.Equal(t, "", id.Person.Prefix)
		require.Equal(t, "1980-02-03", id.Person.DateOfBirth)
	}
}

func TestExtractIdentityIgnoresNullAttributes(t *testing.T) {
	// An optional BRP prefix that is absent (null) must not break extraction.
	nullPrefix := &irma.DisclosedAttribute{
		Identifier: irma.NewAttributeTypeIdentifier("irma-demo.gemeente.personalData.prefix"),
		Status:     irma.AttributeProofStatusNull,
	}
	disclosed := [][]*irma.DisclosedAttribute{{
		disclosedAttr("irma-demo.gemeente.personalData.firstnames", "Anna"),
		nullPrefix,
		disclosedAttr("irma-demo.gemeente.personalData.familyname", "Berg"),
		disclosedAttr("irma-demo.gemeente.personalData.dateofbirth", "03-02-1980"),
	}}
	id, err := ExtractIdentity(disclosed, testIdentityCredentials)
	require.NoError(t, err)
	require.Equal(t, "", id.Person.Prefix)
	require.Equal(t, "Berg", id.Person.Surname)
}

func TestExtractIdentityErrors(t *testing.T) {
	_, err := ExtractIdentity(nil, testIdentityCredentials)
	require.Error(t, err)

	// Unknown credential only.
	_, err = ExtractIdentity([][]*irma.DisclosedAttribute{{
		disclosedAttr("irma-demo.pbdf.email.email", "a@b.c"),
	}}, testIdentityCredentials)
	require.Error(t, err)

	// Two identity credentials at once.
	_, err = ExtractIdentity([][]*irma.DisclosedAttribute{{
		disclosedAttr("irma-demo.pbdf.passport.firstName", "A"),
		disclosedAttr("irma-demo.pbdf.passport.lastName", "B"),
		disclosedAttr("irma-demo.pbdf.passport.dateOfBirth", "1980-02-03"),
	}, {
		disclosedAttr("irma-demo.pbdf.idcard.firstName", "A"),
	}}, testIdentityCredentials)
	require.Error(t, err)

	// Missing date of birth.
	_, err = ExtractIdentity([][]*irma.DisclosedAttribute{{
		disclosedAttr("irma-demo.pbdf.passport.firstName", "A"),
		disclosedAttr("irma-demo.pbdf.passport.lastName", "B"),
	}}, testIdentityCredentials)
	require.Error(t, err)
}
