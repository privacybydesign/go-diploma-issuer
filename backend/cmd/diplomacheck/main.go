// Command diplomacheck verifies the authenticity of DUO diploma PDFs.
//
//	diplomacheck [-trust eutl|pinned] [-ocsp] [-json] file.pdf [file.pdf ...]
//
// By default trust anchors are loaded from the EU Trusted Lists (cached for
// 24h under the user cache directory). Exit status is 0 when every file is
// valid and 1 otherwise.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-diploma-issuer/diploma/eutl"
	"go-diploma-issuer/diploma/trust"
	"go-diploma-issuer/diploma/verify"
)

func main() {
	ocspFlag := flag.Bool("ocsp", false, "also check the signer certificate against the OCSP responder (needs network)")
	jsonFlag := flag.Bool("json", false, "machine readable output")
	trustFlag := flag.String("trust", "eutl", `trust anchor source: "eutl" (EU Trusted Lists, online) or "pinned" (embedded roots, offline)`)
	territories := flag.String("territories", "", "comma separated member states to load from the EU lists, e.g. NL,RO (default: all)")
	cacheDir := flag.String("cache-dir", defaultCacheDir(), "directory for cached trusted lists")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-trust eutl|pinned] [-ocsp] [-json] file.pdf [file.pdf ...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var source verify.TrustSource
	switch *trustFlag {
	case "eutl":
		var terr []string
		if *territories != "" {
			terr = strings.Split(*territories, ",")
		}
		store, err := eutl.Load(ctx, eutl.Options{CacheDir: *cacheDir, Territories: terr})
		if err != nil {
			fmt.Fprintf(os.Stderr, "loading EU trusted lists: %v\n", err)
			os.Exit(2)
		}
		if !*jsonFlag {
			fmt.Printf("EU trusted lists: %d territories loaded (%s), %d services, fetched %s\n",
				len(store.Loaded), strings.Join(store.Loaded, " "), len(store.Services),
				store.FetchedAt.Local().Format(time.RFC3339))
			for t, err := range store.Errors {
				fmt.Printf("  warning: %s list not loaded: %v\n", t, err)
			}
			fmt.Println()
		}
		source = store
	case "pinned":
		pinned, err := trust.NewPinned()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		source = pinned
	default:
		fmt.Fprintf(os.Stderr, "unknown -trust %q\n", *trustFlag)
		os.Exit(2)
	}

	allValid := true
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			allValid = false
			continue
		}
		res, err := verify.PDF(ctx, data, verify.Options{OCSP: *ocspFlag, Trust: source})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			allValid = false
			continue
		}
		if !res.Valid {
			allValid = false
		}
		if *jsonFlag {
			printJSON(path, res)
		} else {
			printHuman(path, res)
		}
	}
	if !allValid {
		os.Exit(1)
	}
}

func defaultCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "diplomacheck", "eutl")
	}
	return ""
}

func printHuman(path string, r *verify.Result) {
	verdict := "VALID"
	if !r.Valid {
		verdict = "INVALID"
	}
	fmt.Printf("== %s\n", path)
	fmt.Printf("   %s\n", verdict)
	if r.Document.Title != "" {
		fmt.Printf("   Document : %s\n", r.Document.Title)
	}
	if r.Document.Author != "" {
		fmt.Printf("   Author   : %s (producer: %s)\n", r.Document.Author, r.Document.Producer)
	}
	if r.Anchor != "" {
		fmt.Printf("   Trust    : %s\n", r.Anchor)
	}
	if r.Signer != nil {
		fmt.Printf("   Signer   : %s\n", r.Signer.Subject.String())
		fmt.Printf("   Cert     : serial %x, valid %s .. %s\n", r.Signer.SerialNumber,
			r.Signer.NotBefore.Format("2006-01-02"), r.Signer.NotAfter.Format("2006-01-02"))
	}
	if !r.SigningTime.IsZero() {
		src := "signer-claimed, untrusted"
		if r.TimestampTrusted {
			src = "RFC 3161 timestamp"
		}
		fmt.Printf("   Signed   : %s (%s)\n", r.SigningTime.Local().Format(time.RFC3339), src)
	}
	for _, c := range r.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Printf("   [%s] %-42s %s\n", mark, c.Name, c.Detail)
	}
	fmt.Println()
}

func printJSON(path string, r *verify.Result) {
	type out struct {
		File             string         `json:"file"`
		Valid            bool           `json:"valid"`
		Title            string         `json:"title,omitempty"`
		Author           string         `json:"author,omitempty"`
		Signer           string         `json:"signer,omitempty"`
		TrustAnchor      string         `json:"trust_anchor,omitempty"`
		SignerSerial     string         `json:"signer_serial,omitempty"`
		SigningTime      *time.Time     `json:"signing_time,omitempty"`
		TimestampTrusted bool           `json:"timestamp_trusted"`
		Checks           []verify.Check `json:"checks"`
	}
	o := out{File: path, Valid: r.Valid, Title: r.Document.Title, Author: r.Document.Author,
		TimestampTrusted: r.TimestampTrusted, Checks: r.Checks, TrustAnchor: r.Anchor}
	if r.Signer != nil {
		o.Signer = r.Signer.Subject.String()
		o.SignerSerial = fmt.Sprintf("%x", r.Signer.SerialNumber)
	}
	if !r.SigningTime.IsZero() {
		t := r.SigningTime
		o.SigningTime = &t
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(o)
}
