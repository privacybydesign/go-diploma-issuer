package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-diploma-issuer/diploma/eutl"
)

// End-to-end against the live EU trusted lists; skipped with -short.
func TestGenuineDiplomaValidAgainstEUTrustedLists(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	files, _ := filepath.Glob("../../test-data/*.pdf")
	if len(files) == 0 {
		t.Skip("no sample PDFs")
	}
	store, err := eutl.Load(context.Background(), eutl.Options{Territories: []string{"NL", "RO"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		res, err := PDF(context.Background(), b, Options{Trust: store})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Valid {
			t.Errorf("%s: failing checks %v", filepath.Base(f), failing(res))
		}
		if res.Anchor == "" || res.Anchor[:15] != "EU trusted list" {
			t.Errorf("%s: anchor %q not from EU trusted list", filepath.Base(f), res.Anchor)
		}
	}
}
