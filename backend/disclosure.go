package main

import (
	"fmt"
	"strings"

	"go-diploma-issuer/identity"

	"github.com/privacybydesign/irmago/irma"
)

// DisclosedIdentity is the identity a user proved through Yivi together with
// the credential it came from.
type DisclosedIdentity struct {
	Source string
	Person identity.Person
}

// ExtractIdentity turns the disclosed attributes of a finished IRMA session
// into an identity. It looks for one of the configured identity credentials
// and reads the name and date of birth attributes of that credential.
// Attributes of unknown credentials, including a BRP credential when none is
// configured, are ignored.
func ExtractIdentity(disclosed [][]*irma.DisclosedAttribute, credentials IdentityCredentials) (*DisclosedIdentity, error) {
	values := map[string]string{}  // full attribute id -> raw value
	sources := map[string]string{} // source -> credential id
	for _, conjunction := range disclosed {
		for _, attribute := range conjunction {
			if attribute == nil || attribute.Status != irma.AttributeProofStatusPresent {
				continue
			}
			id := attribute.Identifier.String()
			credential := attribute.Identifier.CredentialTypeIdentifier().String()
			source := credentials.SourceOf(credential)
			if source == "" {
				continue
			}
			sources[source] = credential
			if attribute.RawValue != nil {
				values[id] = *attribute.RawValue
			}
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no identity credential among the disclosed attributes")
	}
	if len(sources) > 1 {
		return nil, fmt.Errorf("attributes of multiple identity credentials disclosed")
	}

	for source, credential := range sources {
		get := func(name string) string {
			return strings.TrimSpace(values[credential+"."+name])
		}
		var person identity.Person
		if source == SourceBrp {
			person = identity.Person{
				GivenNames:  get(BrpAttrFirstNames),
				Prefix:      get(BrpAttrPrefix),
				Surname:     get(BrpAttrFamilyName),
				DateOfBirth: get(BrpAttrDateOfBirth),
			}
		} else {
			person = identity.Person{
				GivenNames:  get(DocAttrFirstName),
				Surname:     get(DocAttrLastName),
				DateOfBirth: get(DocAttrDateOfBirth),
			}
		}
		if person.GivenNames == "" || person.Surname == "" || person.DateOfBirth == "" {
			return nil, fmt.Errorf("disclosed %s credential misses a name or date of birth attribute", source)
		}
		return &DisclosedIdentity{Source: source, Person: person}, nil
	}
	return nil, fmt.Errorf("no identity credential among the disclosed attributes")
}
