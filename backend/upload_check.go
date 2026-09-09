package main

import (
	"bytes"
	"mime"
	"path"
	"strings"
)

// pdfTrailerWindow is how far from the end of the file the %%EOF marker may
// sit. The PDF specification requires it on the last line; real-world producers
// (including the DUO extract generator) follow that closely, so a small window
// leaves room for trailing whitespace without accepting arbitrary appended data.
const pdfTrailerWindow = 1024

// pdfContentTypes lists the multipart part Content-Types accepted for the
// uploaded file. Browsers send application/pdf; some tools send a generic
// octet-stream or nothing at all. Anything else is a strong sign the client
// picked the wrong file.
var pdfContentTypes = map[string]bool{
	"":                         true,
	"application/pdf":          true,
	"application/x-pdf":        true,
	"application/octet-stream": true,
}

// acceptableUploadMetadata checks the client-supplied filename and Content-Type
// of the multipart part. Both are under the client's control and therefore no
// security boundary, but rejecting obviously wrong values early gives a clearer
// error than a failed validation call and adds a layer of defence in depth.
func acceptableUploadMetadata(filename, contentType string) bool {
	if filename != "" {
		ext := strings.ToLower(path.Ext(filename))
		if ext != ".pdf" {
			return false
		}
	}
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return pdfContentTypes[strings.ToLower(mediaType)]
}

// looksLikePdf checks the file signature at both ends of the document:
// a "%PDF-<major>.<minor>" header at offset 0 and a "%%EOF" end-of-file marker
// close to the end. A file that merely starts with "%PDF" (a polyglot or a
// truncated download) does not pass.
func looksLikePdf(data []byte) bool {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return false
	}
	// Header: %PDF-1.x or %PDF-2.x; the version is a digit, a dot, a digit.
	if len(data) < len("%PDF-1.0") {
		return false
	}
	version := data[len("%PDF-"):len("%PDF-1.0")]
	if !isDigit(version[0]) || version[1] != '.' || !isDigit(version[2]) {
		return false
	}

	tail := data
	if len(tail) > pdfTrailerWindow {
		tail = tail[len(tail)-pdfTrailerWindow:]
	}
	tail = bytes.TrimRight(tail, " \t\r\n\x00")
	return bytes.HasSuffix(tail, []byte("%%EOF"))
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
