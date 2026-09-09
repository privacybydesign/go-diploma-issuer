package verify

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sample = "../../test-data/Hoger algemeen voortgezet onderwijs.pdf"

func load(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(sample)
	if err != nil {
		t.Skipf("sample PDF not available: %v", err)
	}
	return b
}

func run(t *testing.T, pdf []byte) *Result {
	t.Helper()
	res, err := PDF(context.Background(), pdf, Options{})
	if err != nil {
		t.Fatalf("PDF(): %v", err)
	}
	return res
}

func failing(res *Result) []string {
	var out []string
	for _, c := range res.Checks {
		if !c.OK {
			out = append(out, c.Name)
		}
	}
	return out
}

func TestGenuineDiplomaIsValid(t *testing.T) {
	files, _ := filepath.Glob("../../test-data/*.pdf")
	if len(files) == 0 {
		t.Skip("no sample PDFs")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		res := run(t, b)
		if !res.Valid {
			t.Errorf("%s: expected valid, failing checks: %v", filepath.Base(f), failing(res))
		}
		if !res.TimestampTrusted {
			t.Errorf("%s: expected trusted timestamp", filepath.Base(f))
		}
		if res.Signer == nil || res.Signer.Subject.CommonName != "Dienst Uitvoering Onderwijs" {
			t.Errorf("%s: unexpected signer %v", filepath.Base(f), res.Signer)
		}
	}
}

// Changing a single byte inside the signed range (here: in the page content)
// must break the CMS message digest.
func TestTamperedContentIsInvalid(t *testing.T) {
	pdf := bytes.Clone(load(t))
	i := bytes.Index(pdf, []byte("/Title ("))
	if i < 0 {
		t.Fatal("no /Title in sample")
	}
	pdf[i+8] ^= 0x01
	res := run(t, pdf)
	if res.Valid {
		t.Fatal("tampered PDF reported valid")
	}
	if got := failing(res); len(got) != 1 || got[0] != "CMS signature" {
		t.Errorf("expected only the CMS signature check to fail, got %v", got)
	}
}

// Appending an incremental update (the normal way PDF tools "edit" a file)
// leaves the original signature intact but no longer covering the file.
func TestAppendedUpdateIsInvalid(t *testing.T) {
	pdf := bytes.Clone(load(t))
	pdf = append(pdf, []byte("\n999 0 obj\n<< /Type /Annot /Contents (extra cijfer) >>\nendobj\n%%EOF\n")...)
	res := run(t, pdf)
	if res.Valid {
		t.Fatal("PDF with appended update reported valid")
	}
	if got := failing(res); len(got) != 1 || got[0] != "signature covers whole file" {
		t.Errorf("expected only the coverage check to fail, got %v", got)
	}
}

func TestUnsignedPDFIsInvalid(t *testing.T) {
	res := run(t, []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"))
	if res.Valid {
		t.Fatal("unsigned PDF reported valid")
	}
}

func TestNotAPDF(t *testing.T) {
	res := run(t, []byte("hello"))
	if res.Valid {
		t.Fatal("non-PDF reported valid")
	}
}

func TestParsePDFDate(t *testing.T) {
	got, err := parsePDFDate("D:20260903133049+02'00'")
	if err != nil {
		t.Fatal(err)
	}
	if got.UTC().Format("2006-01-02T15:04:05Z") != "2026-09-03T11:30:49Z" {
		t.Errorf("got %v", got)
	}
}
