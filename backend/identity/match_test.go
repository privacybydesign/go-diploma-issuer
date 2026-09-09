package identity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var documentPerson = Person{
	GivenNames:  "Anna Maria",
	Surname:     "Berg",
	Prefix:      "van der",
	DateOfBirth: "1980-02-03",
}

func TestMatchBrp(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna Maria",
		Prefix:      "van der",
		Surname:     "Berg",
		DateOfBirth: "03-02-1980",
	})
	require.True(t, result.Matched, result.Reasons)
	require.Empty(t, result.Reasons)
}

func TestMatchPassportStyle(t *testing.T) {
	// Passport / ID card: transliterated upper case, prefix inside the last name.
	result := Match(documentPerson, Person{
		GivenNames:  "ANNA MARIA",
		Surname:     "VAN DER BERG",
		DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchTrailingPrefix(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna",
		Surname:     "Berg van der",
		DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchFirstGivenNameSuffices(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna",
		Surname:     "van der Berg",
		DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchDiacriticsAndPunctuation(t *testing.T) {
	document := Person{GivenNames: "Zoë", Surname: "Müller-Lüdenscheidt", DateOfBirth: "1 januari 2000"}
	result := Match(document, Person{GivenNames: "ZOE", Surname: "MULLER LUDENSCHEIDT", DateOfBirth: "2000-01-01"})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchDateOfBirthMismatch(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna Maria",
		Surname:     "van der Berg",
		DateOfBirth: "1980-02-04",
	})
	require.False(t, result.Matched)
	require.False(t, result.DateOfBirthMatch)
	require.True(t, result.SurnameMatch)
	require.True(t, result.GivenNamesMatch)
	require.Contains(t, result.Reasons, "date of birth differs")
}

func TestMatchSurnameMismatch(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna Maria",
		Surname:     "Bergen",
		Prefix:      "van der",
		DateOfBirth: "1980-02-03",
	})
	require.False(t, result.Matched)
	require.False(t, result.SurnameMatch)
	require.Contains(t, result.Reasons, "surname differs")
}

func TestMatchGivenNamesMismatch(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Maria",
		Surname:     "van der Berg",
		DateOfBirth: "1980-02-03",
	})
	require.False(t, result.Matched)
	require.False(t, result.GivenNamesMatch)
	require.Contains(t, result.Reasons, "given names differ")
}

func TestMatchUnreadableDate(t *testing.T) {
	result := Match(documentPerson, Person{
		GivenNames:  "Anna",
		Surname:     "van der Berg",
		DateOfBirth: "unknown",
	})
	require.False(t, result.Matched)
	require.False(t, result.DateOfBirthMatch)
	require.NotEmpty(t, result.Reasons)
}

func TestMatchEmptyDisclosed(t *testing.T) {
	result := Match(documentPerson, Person{})
	require.False(t, result.Matched)
	require.False(t, result.SurnameMatch)
	require.False(t, result.GivenNamesMatch)
}

func TestNormalize(t *testing.T) {
	require.Equal(t, "VAN DER BERG", Normalize("  van   der\tBerg "))
	require.Equal(t, "MULLER LUDENSCHEIDT", Normalize("Müller-Lüdenscheidt"))
	require.Equal(t, "O BRIEN", Normalize("O'Brien"))
	require.Equal(t, "", Normalize(""))
}

func TestParseDate(t *testing.T) {
	expected := time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC)
	for _, input := range []string{"1980-02-03", "03-02-1980", "3 februari 1980", "3 February 1980", "19800203", "03/02/1980", "1980-02-03T00:00:00Z"} {
		got, err := ParseDate(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, got, input)
	}
	for _, invalid := range []string{"", "14-13-1980", "yesterday"} {
		_, err := ParseDate(invalid)
		require.Error(t, err, invalid)
	}
}

func TestMatchFullNameBrp(t *testing.T) {
	result := MatchFullName("Anna Maria van der Berg", "1980-02-03", Person{
		GivenNames: "Anna Maria", Prefix: "van der", Surname: "Berg", DateOfBirth: "03-02-1980",
	})
	require.True(t, result.Matched, result.Reasons)
	require.Empty(t, result.Reasons)
}

func TestMatchFullNamePassportStyle(t *testing.T) {
	// Passport: upper case, prefix inside the last name, first given name only.
	result := MatchFullName("Anna Maria van der Berg", "1980-02-03", Person{
		GivenNames: "ANNA", Surname: "VAN DER BERG", DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)

	// Trailing prefix as some MRZ transliterations do.
	result = MatchFullName("Anna van der Berg", "1980-02-03", Person{
		GivenNames: "Anna", Surname: "Berg van der", DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchFullNameNoPrefix(t *testing.T) {
	result := MatchFullName("Piet Jansen", "3 februari 1980", Person{
		GivenNames: "Piet", Surname: "Jansen", DateOfBirth: "1980-02-03",
	})
	require.True(t, result.Matched, result.Reasons)
}

func TestMatchFullNameSurnameMismatch(t *testing.T) {
	result := MatchFullName("Anna Maria van der Berg", "1980-02-03", Person{
		GivenNames: "Anna", Surname: "Bergen", Prefix: "van der", DateOfBirth: "1980-02-03",
	})
	require.False(t, result.Matched)
	require.False(t, result.SurnameMatch)
	require.True(t, result.GivenNamesMatch, "the first word is still compared with the given names")
	require.Contains(t, result.Reasons, "surname differs")
}

func TestMatchFullNameGivenNamesMismatch(t *testing.T) {
	result := MatchFullName("Anna Maria van der Berg", "1980-02-03", Person{
		GivenNames: "Maria", Surname: "van der Berg", DateOfBirth: "1980-02-03",
	})
	require.False(t, result.Matched)
	require.True(t, result.SurnameMatch)
	require.False(t, result.GivenNamesMatch)
	require.Contains(t, result.Reasons, "given names differ")
}

func TestMatchFullNameSurnameOnly(t *testing.T) {
	// A printed name that is nothing but the surname has no given names.
	result := MatchFullName("Berg", "1980-02-03", Person{
		GivenNames: "Anna", Surname: "Berg", DateOfBirth: "1980-02-03",
	})
	require.False(t, result.Matched)
	require.False(t, result.SurnameMatch)
}

func TestMatchFullNameDateOfBirth(t *testing.T) {
	result := MatchFullName("Piet Jansen", "3 februari 1980", Person{
		GivenNames: "Piet", Surname: "Jansen", DateOfBirth: "1980-02-04",
	})
	require.False(t, result.Matched)
	require.False(t, result.DateOfBirthMatch)
	require.Contains(t, result.Reasons, "date of birth differs")

	result = MatchFullName("Piet Jansen", "onbekend", Person{
		GivenNames: "Piet", Surname: "Jansen", DateOfBirth: "1980-02-03",
	})
	require.False(t, result.DateOfBirthMatch)
	require.NotEmpty(t, result.Reasons)
}
