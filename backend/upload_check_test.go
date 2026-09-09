package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLooksLikePdf(t *testing.T) {
	// Genuine extracts are not part of the repository; check them when present.
	files, _ := filepath.Glob("test-data/*.pdf")
	for _, name := range files {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		require.True(t, looksLikePdf(data), name)
	}

	cases := map[string]struct {
		data []byte
		want bool
	}{
		"minimal pdf":             {[]byte("%PDF-1.4\n1 0 obj\nendobj\n%%EOF\n"), true},
		"pdf 2.0":                 {[]byte("%PDF-2.0\n%%EOF"), true},
		"trailing whitespace":     {[]byte("%PDF-1.7\n%%EOF\r\n\r\n   "), true},
		"empty":                   {nil, false},
		"header only":             {[]byte("%PDF-1.5"), false},
		"bare prefix no version":  {[]byte("%PDF\n%%EOF"), false},
		"bad version":             {[]byte("%PDF-x.y\n%%EOF"), false},
		"missing eof":             {[]byte("%PDF-1.5\n1 0 obj\nendobj\n"), false},
		"eof too far from end":    {[]byte("%PDF-1.5\n%%EOF\n" + string(make([]byte, 2048))), false},
		"appended data after eof": {[]byte("%PDF-1.5\n%%EOF\nMZ this is not pdf"), false},
		"png":                     {[]byte("\x89PNG\r\n\x1a\n"), false},
		"text file":               {[]byte("hello"), false},
		"html":                    {[]byte("<html><body>%PDF-1.5 %%EOF</body></html>"), false},
		"leading whitespace":      {[]byte("\n%PDF-1.5\n%%EOF"), false},
		"zip claiming to be pdf":  {[]byte("PK\x03\x04%PDF-1.5%%EOF"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, looksLikePdf(tc.data))
		})
	}
}

func TestAcceptableUploadMetadata(t *testing.T) {
	cases := map[string]struct {
		filename, contentType string
		want                  bool
	}{
		"browser upload":          {"diploma.pdf", "application/pdf", true},
		"upper case extension":    {"DIPLOMA.PDF", "application/pdf", true},
		"x-pdf":                   {"diploma.pdf", "application/x-pdf", true},
		"octet stream":            {"diploma.pdf", "application/octet-stream", true},
		"no content type":         {"diploma.pdf", "", true},
		"no filename":             {"", "application/pdf", true},
		"content type with param": {"diploma.pdf", "application/pdf; charset=binary", true},
		"txt extension":           {"diploma.txt", "application/pdf", false},
		"double extension":        {"diploma.pdf.exe", "application/pdf", false},
		"no extension":            {"diploma", "application/pdf", false},
		"image type":              {"diploma.pdf", "image/png", false},
		"html type":               {"diploma.pdf", "text/html", false},
		"garbage type":            {"diploma.pdf", "not a media type", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, acceptableUploadMetadata(tc.filename, tc.contentType))
		})
	}
}
