package diploma

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go-diploma-issuer/diploma/eutl"
	"go-diploma-issuer/diploma/trust"
	"go-diploma-issuer/diploma/verify"
)

// ErrTrustUnavailable is returned when no trust anchors could be loaded, so a
// signature cannot be validated at all. The document itself is not at fault.
var ErrTrustUnavailable = errors.New("trust anchors unavailable")

// Verification is the outcome of the cryptographic verification of one
// extract: the PAdES signature, the timestamp, the certificate chain and the
// identity of the signer (DUO).
type Verification struct {
	// Valid is true only when every check passed.
	Valid bool `json:"valid"`
	// Checks lists every individual check in the order it ran.
	Checks []verify.Check `json:"checks"`
	// Signer is the subject of the signing certificate, e.g. "CN=Dienst
	// Uitvoering Onderwijs,O=Dienst Uitvoering Onderwijs (DUO),C=NL".
	Signer string `json:"signer,omitempty"`
	// TrustAnchor describes where the trust anchor came from (an EU trusted
	// list entry or a pinned root).
	TrustAnchor string `json:"trust_anchor,omitempty"`
	// SigningTime is the time the signature was made (from the RFC 3161
	// timestamp when TimestampTrusted, otherwise as claimed by the signer).
	SigningTime      time.Time `json:"signing_time"`
	TimestampTrusted bool      `json:"timestamp_trusted"`
}

// Key is a stable identifier for the outcome: "valid" or the ID of the first
// failed check.
func (v *Verification) Key() string {
	if v.Valid {
		return "valid"
	}
	for _, c := range v.Checks {
		if !c.OK {
			return c.ID
		}
	}
	return "invalid"
}

// FailedChecks returns the checks that did not pass.
func (v *Verification) FailedChecks() []verify.Check {
	var failed []verify.Check
	for _, c := range v.Checks {
		if !c.OK {
			failed = append(failed, c)
		}
	}
	return failed
}

// Validator checks a diploma extract for authenticity and integrity.
type Validator interface {
	// Validate verifies the signature. A well formed answer, including a
	// rejection, is returned as a Verification; an error means the check
	// could not be performed (ErrTrustUnavailable or an internal failure).
	Validate(ctx context.Context, pdf []byte) (*Verification, error)
}

// TrustConfig configures where the trust anchors come from.
type TrustConfig struct {
	// Source is "eutl" (EU Trusted Lists, the default) or "pinned" (the
	// embedded PKIoverheid and certSIGN roots, works offline).
	Source string `json:"source"`
	// Territories limits the member state lists that are downloaded, e.g.
	// ["NL", "RO"]. Empty loads all of them.
	Territories []string `json:"territories"`
	// CacheDir stores the downloaded lists. Empty disables caching.
	CacheDir string `json:"cache_dir"`
	// RefreshHours is how often the lists are reloaded. Defaults to 24.
	RefreshHours int `json:"refresh_hours"`
	// FallbackToPinned uses the embedded roots when the EU lists cannot be
	// loaded (at startup or on refresh). Defaults to false, in which case a
	// failed initial load is fatal and a failed refresh keeps the old lists.
	FallbackToPinned bool `json:"fallback_to_pinned"`
}

const (
	TrustSourceEUTL   = "eutl"
	TrustSourcePinned = "pinned"
	DefaultRefresh    = 24 * time.Hour
)

// TrustStore holds the current trust anchors and refreshes them in the
// background. It is safe for concurrent use.
type TrustStore struct {
	config     TrustConfig
	httpClient *http.Client

	mutex       sync.RWMutex
	source      verify.TrustSource
	description string
	loadedAt    time.Time
}

// NewTrustStore loads the anchors once. With Source "eutl" this downloads the
// EU List of Trusted Lists and the member state lists (or reads them from the
// cache), which takes a few seconds.
func NewTrustStore(ctx context.Context, config TrustConfig, httpClient *http.Client) (*TrustStore, error) {
	if config.Source == "" {
		config.Source = TrustSourceEUTL
	}
	if config.Source != TrustSourceEUTL && config.Source != TrustSourcePinned {
		return nil, fmt.Errorf("unknown trust source %q (use %q or %q)", config.Source, TrustSourceEUTL, TrustSourcePinned)
	}
	if config.RefreshHours <= 0 {
		config.RefreshHours = int(DefaultRefresh / time.Hour)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	store := &TrustStore{config: config, httpClient: httpClient}
	if err := store.Refresh(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// Refresh reloads the anchors. A failure to load the EU lists keeps the
// previous anchors (or falls back to the pinned roots when configured); the
// error is returned so the caller can log it.
func (s *TrustStore) Refresh(ctx context.Context) error {
	source, description, err := s.load(ctx)
	if err != nil {
		if s.config.FallbackToPinned {
			pinned, perr := trust.NewPinned()
			if perr != nil {
				return perr
			}
			slog.Warn("EU trusted lists unavailable, falling back to pinned roots", "error", err)
			s.set(pinned, "pinned roots (EU trusted lists unavailable)")
			return nil
		}
		return err
	}
	s.set(source, description)
	return nil
}

func (s *TrustStore) load(ctx context.Context) (verify.TrustSource, string, error) {
	if s.config.Source == TrustSourcePinned {
		pinned, err := trust.NewPinned()
		if err != nil {
			return nil, "", err
		}
		return pinned, "pinned roots", nil
	}
	store, err := eutl.Load(ctx, eutl.Options{
		HTTPClient:  s.httpClient,
		CacheDir:    s.config.CacheDir,
		MaxAge:      time.Duration(s.config.RefreshHours) * time.Hour,
		Territories: s.config.Territories,
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrTrustUnavailable, err)
	}
	for territory, terr := range store.Errors {
		slog.Warn("EU trusted list not loaded", "territory", territory, "error", terr)
	}
	description := fmt.Sprintf("EU trusted lists (%d territories, %d services, fetched %s)",
		len(store.Loaded), len(store.Services), store.FetchedAt.UTC().Format(time.RFC3339))
	return store, description, nil
}

func (s *TrustStore) set(source verify.TrustSource, description string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.source = source
	s.description = description
	s.loadedAt = time.Now()
	slog.Info("trust anchors loaded", "source", description)
}

// Current returns the anchors in use, or nil when none are loaded.
func (s *TrustStore) Current() verify.TrustSource {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.source
}

// Description says where the current anchors came from.
func (s *TrustStore) Description() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.description
}

// RunRefresh reloads the anchors every RefreshHours until ctx is cancelled.
// Run it in a goroutine.
func (s *TrustStore) RunRefresh(ctx context.Context) {
	interval := time.Duration(s.config.RefreshHours) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				slog.Warn("failed to refresh trust anchors, keeping the current ones", "error", err)
			}
		}
	}
}

// PadesValidator verifies the PAdES signature DUO puts on every extract
// against the anchors of a TrustStore.
type PadesValidator struct {
	trust      *TrustStore
	ocsp       bool
	httpClient *http.Client
}

// NewPadesValidator creates a validator. With ocsp enabled the signer
// certificate is also checked online against DUO's OCSP responder.
func NewPadesValidator(store *TrustStore, ocsp bool, httpClient *http.Client) *PadesValidator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &PadesValidator{trust: store, ocsp: ocsp, httpClient: httpClient}
}

// OCSP reports whether online revocation checking is enabled.
func (v *PadesValidator) OCSP() bool { return v.ocsp }

// Validate verifies the signature of the PDF.
func (v *PadesValidator) Validate(ctx context.Context, pdf []byte) (*Verification, error) {
	source := v.trust.Current()
	if source == nil {
		return nil, ErrTrustUnavailable
	}
	result, err := verify.PDF(ctx, pdf, verify.Options{
		Trust:      source,
		OCSP:       v.ocsp,
		HTTPClient: v.httpClient,
	})
	if err != nil {
		return nil, err
	}
	return FromResult(result), nil
}

// FromResult converts the verifier's result into the API facing Verification.
func FromResult(result *verify.Result) *Verification {
	v := &Verification{
		Valid:            result.Valid,
		Checks:           result.Checks,
		TrustAnchor:      result.Anchor,
		SigningTime:      result.SigningTime,
		TimestampTrusted: result.TimestampTrusted,
	}
	if v.Checks == nil {
		v.Checks = []verify.Check{}
	}
	if result.Signer != nil {
		v.Signer = result.Signer.Subject.String()
	}
	return v
}
