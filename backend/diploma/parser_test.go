package diploma

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sharedParser is initialised once: compiling the PDFium WebAssembly module
// takes seconds.
var sharedParser *PdfiumParser

func TestMain(m *testing.M) {
	parser, err := NewPdfiumParser()
	if err != nil {
		panic(err)
	}
	sharedParser = parser
	code := m.Run()
	_ = parser.Close()
	os.Exit(code)
}

// realExtracts describes the genuine DUO extracts in ../test-data. The PDFs
// and this file contain personal data and are not part of the repository;
// the tests that need them skip when they are absent.
type realExtracts struct {
	FullName     string `json:"full_name"`
	DateOfBirth  string `json:"date_of_birth"`
	DownloadDate string `json:"download_date"`
	Extracts     []struct {
		File          string   `json:"file"`
		DocumentType  string   `json:"document_type"`
		Qualification string   `json:"qualification"`
		Profiles      []string `json:"profiles"`
		Institution   string   `json:"institution"`
		Place         string   `json:"place"`
		Awarded       string   `json:"awarded"`
		NLQF          string   `json:"nlqf"`
		EQF           string   `json:"eqf"`
		Number        string   `json:"number"`
		Pages         int      `json:"pages"`
		GradeList     bool     `json:"grade_list"`
	} `json:"extracts"`
}

func loadRealExtracts(t *testing.T) realExtracts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "test-data", "expected.json"))
	if err != nil {
		t.Skipf("real extracts not available: %v", err)
	}
	var expected realExtracts
	require.NoError(t, json.Unmarshal(data, &expected))
	return expected
}

func readTestPdf(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "test-data", name))
	require.NoError(t, err)
	return data
}

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	require.NoError(t, err, s)
	return d
}

func TestParseRealExtracts(t *testing.T) {
	expected := loadRealExtracts(t)
	require.NotEmpty(t, expected.Extracts)

	for _, tc := range expected.Extracts {
		t.Run(tc.File, func(t *testing.T) {
			doc, err := sharedParser.Parse(readTestPdf(t, tc.File))
			require.NoError(t, err)

			require.Equal(t, tc.DocumentType, doc.DocumentType)
			require.Equal(t, tc.Qualification, doc.Qualification)
			require.Equal(t, tc.Profiles, doc.Profiles)
			require.Equal(t, expected.FullName, doc.FullName)
			require.Equal(t, date(t, expected.DateOfBirth), doc.DateOfBirth)
			require.Equal(t, tc.Institution, doc.Institution)
			require.Equal(t, tc.Place, doc.PlaceOfIssue)
			require.Equal(t, date(t, tc.Awarded), doc.DateAwarded)
			require.Equal(t, tc.NLQF, doc.NLQFLevel)
			require.Equal(t, tc.EQF, doc.EQFLevel)
			require.Equal(t, date(t, expected.DownloadDate), doc.DownloadDate)
			require.Equal(t, tc.Number, doc.DocumentNumber)
			require.Equal(t, tc.Pages, doc.Pages)
			require.Equal(t, tc.GradeList, doc.HasGradeList)
		})
	}
}

func TestParseRejectsNonPdf(t *testing.T) {
	_, err := sharedParser.Parse([]byte("this is not a pdf"))
	require.ErrorIs(t, err, ErrNotADiploma)
}

// word builds a Word at the given position with a nominal 10pt height.
func word(text string, top float64) Word {
	return Word{Text: text, Left: 70, Top: top, Right: 70 + float64(len(text))*5, Bottom: top - 8}
}

// syntheticExtract mimics the layout of a real extract, one line per word.
func syntheticExtract(lines ...string) *Pages {
	pages := &Pages{}
	top := 800.0
	for _, l := range lines {
		pages.First = append(pages.First, word(l, top))
		top -= 12
	}
	return pages
}

func TestExtractDocumentSynthetic(t *testing.T) {
	doc, err := ExtractDocument(syntheticExtract(
		"Uit het register onderwijsdeelnemers",
		"DIPLOMA",
		"Middelbaar beroepsonderwijs",
		"Verpleegkundige (niveau 4)",
		"BEHAALD DOOR",
		"Anna Maria van der Berg",
		"Geboren op 3 februari 1980",
		"UITGEGEVEN DOOR",
		"ROC Midden Nederland",
		"Utrecht, 1 juli 2001",
		"Dit diploma heeft niveau NLQF 4 / EQF 4.",
		"Downloaddatum: 2 september 2026",
		"Dit digitale document is een officieel bewijs van diplomagegevens. 123456 - pagina 1/1",
		"Het vervalsen van dit document is strafbaar. DUO doet dan aangifte.",
	))
	require.NoError(t, err)
	require.Equal(t, "Diploma", doc.DocumentType)
	require.Equal(t, "Middelbaar beroepsonderwijs Verpleegkundige (niveau 4)", doc.Qualification)
	require.Empty(t, doc.Profiles)
	require.Equal(t, "Anna Maria van der Berg", doc.FullName)
	require.Equal(t, time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC), doc.DateOfBirth)
	require.Equal(t, "ROC Midden Nederland", doc.Institution)
	require.Equal(t, "Utrecht", doc.PlaceOfIssue)
	require.Equal(t, time.Date(2001, 7, 1, 0, 0, 0, 0, time.UTC), doc.DateAwarded)
	require.Equal(t, "4", doc.NLQFLevel)
	require.Equal(t, "123456", doc.DocumentNumber)
	require.Equal(t, 1, doc.Pages)
	require.False(t, doc.HasGradeList)
}

func TestExtractDocumentErrors(t *testing.T) {
	_, err := ExtractDocument(&Pages{})
	require.ErrorIs(t, err, ErrNotADiploma)

	_, err = ExtractDocument(syntheticExtract("Verklaring Omtrent het Gedrag", "Datum 1 oktober 2025"))
	require.ErrorIs(t, err, ErrNotADiploma)

	// Register heading but no holder.
	_, err = ExtractDocument(syntheticExtract(
		"Uit het register onderwijsdeelnemers",
		"DIPLOMA",
		"Iets",
		"UITGEGEVEN DOOR",
		"School",
		"Ergens, 1 juli 2001",
		"1 - pagina 1/1",
	))
	require.ErrorIs(t, err, ErrNotADiploma)
}

func TestSamePerson(t *testing.T) {
	a := &Document{FullName: "Anna van der Berg", DateOfBirth: time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC)}
	b := &Document{FullName: "anna  van der berg", DateOfBirth: time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC)}
	c := &Document{FullName: "Anna van der Berg", DateOfBirth: time.Date(1980, 2, 4, 0, 0, 0, 0, time.UTC)}
	d := &Document{FullName: "Piet van der Berg", DateOfBirth: a.DateOfBirth}
	require.True(t, a.SamePerson(b))
	require.False(t, a.SamePerson(c))
	require.False(t, a.SamePerson(d))
	require.False(t, a.SamePerson(nil))
}

func TestParseDutchDate(t *testing.T) {
	expected := time.Date(1980, 2, 3, 0, 0, 0, 0, time.UTC)
	for _, input := range []string{"3 februari 1980", "03-02-1980", "1980-02-03", "3 februari 1980."} {
		got, err := ParseDutchDate(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, got)
	}
	for _, invalid := range []string{"", "31 februari 1980", "3 feb 1980", "gisteren"} {
		_, err := ParseDutchDate(invalid)
		require.Error(t, err, invalid)
	}
}
