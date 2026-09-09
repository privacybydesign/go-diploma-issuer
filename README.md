# Go Diploma Issuer

The Go Diploma Issuer turns the digital **diploma extracts** of DUO (Dienst Uitvoering Onderwijs, "Mijn diploma's", the Dutch diploma register) into privacy-preserving [Yivi](https://yivi.app) credentials, one per diploma. It follows the architecture of the [go-vog-issuer](https://github.com/privacybydesign/go-vog-issuer) and the [go-passport-issuer](https://github.com/privacybydesign/go-passport-issuer): a Go backend with a ReDoc-documented API, a React frontend, Redis or in-memory session storage and Docker packaging. Credentials are issued over the IRMA protocol in both the IRMA (Idemix) and SD-JWT VC formats.

## How it works

1. **Get the extracts.** The holder logs in to [Mijn DUO](https://mijndiplomas.nl) with DigiD, opens *Mijn studies en diploma's* and downloads the extract (uittreksel) of every diploma as a PDF. The frontend explains this step by step and links to DUO's [Uittreksel diploma](https://duo.nl/particulier/uittreksel-diplomagegevens-downloaden.jsp) page. Only the holder can download these extracts; they are free.
2. **Upload.** The holder uploads one or more extract PDFs at once. The backend verifies every PDF itself, without calling DUO: the **PAdES signature** DUO puts on the extract must be a single certification signature that covers the whole file, be cryptographically valid, carry an RFC 3161 timestamp from a qualified timestamp authority, and be made with a qualified electronic seal certificate of DUO that chains to a qualified trust service on the **EU Trusted Lists** (eIDAS). Only then is the PDF read with PDFium running in WebAssembly (pure Go): qualification, holder, date of birth, institution, place and date of award, NLQF/EQF level and the extract number.
3. **Identity.** The holder proves who they are in the Yivi app. The disclosure request offers four alternatives, the app lets the user pick: BRP personal data (`gemeente.personalData`), passport, ID card or driving licence. The backend runs this session itself so it can read the result.
4. **Match.** The disclosed name and date of birth are compared with the holder named on the extracts (case- and diacritic-insensitive, surname with or without prefix in either order, first given name suffices). No match, no credentials; the holder may disclose again with another credential.
5. **Issue.** On a match the backend signs one IRMA issuance request containing a diploma credential per extract and the frontend hands it to the Yivi app.

The uploaded PDFs and the parsed data live in the session store for at most one hour and are removed as soon as the credentials are issued.

### Several diplomas at once

An upload may contain up to `max_files` extracts (default 10). Every file gets its own verdict in the response (`files[].accepted`, with the error key and the failed verification step when rejected). As long as one file is accepted the upload succeeds and a session is created for the accepted files; the frontend shows a green card with the data of every accepted extract and a red card with the reason for every rejected one, and the holder can continue with the accepted ones or upload other files. All extracts in one session must name the same holder (`error:different-person` otherwise) and the same diploma cannot be uploaded twice (`error:duplicate-diploma`). The identity check is done once for the whole set and the credentials are issued in a single Yivi session, one card per diploma.

### How DUO extracts are secured

The extract is a PDF signed by DUO. There is no online validation service like validatie.nl for the VOG; the trust is in the signature and in the EU Trusted Lists, and there is a manual check at [duo.nl/diplomacontrole](https://duo.nl/diplomacontrole) using the extract number. What the verifier (`backend/diploma/verify`) checks:

| Check (`validation.checks[].id`) | What it means |
|---|---|
| `signature_present`, `single_signature` | Exactly one digital signature in the PDF. |
| `covers_whole_file` | The `ByteRange` covers the whole file: anything appended after signing (an incremental update, "save as" in another program) makes the file invalid. |
| `certification_signature` | A DocMDP certification signature with `/P 1` (no changes allowed). |
| `pades_subfilter` | `/SubFilter /ETSI.CAdES.detached` (PAdES). |
| `cms_parse`, `cms_signature` | The CMS `SignedData` is a valid detached signature over the signed bytes. |
| `signing_certificate_v2` | The ESS `signing-certificate-v2` attribute binds the signature to the certificate (PAdES baseline). |
| `timestamp_token`, `timestamp_authority_trusted` | The RFC 3161 token hashes the signature value correctly and its TSA (certSIGN) is a `TSA/QTST` service on the EU lists. The token time is the validation time. |
| `certificate_chain` | The signer chains to a trust anchor at that time: *KPN BV PKIoverheid Organisatie Services CA - G3*, listed on the Dutch trusted list as a granted `CA/QC` service. |
| `qualified_eseal`, `key_usage_non_repudiation` | The certificate carries the QcCompliance and QcType e-seal statements and the non-repudiation key usage: a qualified electronic seal. |
| `signer_identity` | The subject is *Dienst Uitvoering Onderwijs (DUO)* with organizationIdentifier `NTRNL-50973029` (DUO's KvK number). |
| `ocsp` | Online revocation check of the signer certificate (only with `validation.ocsp: true`). |

The frontend translates every check id (`validation_check_<id>`) into a plain-language reason, e.g. "The PDF was changed after DUO signed it. Download the original extract again."

#### Where the trust comes from

Nobody has to maintain a root list. By default (`trust.source: "eutl"`) the anchors are loaded from the EU Trusted Lists defined by the eIDAS regulation: the European Commission publishes a signed *List of Trusted Lists* at `https://ec.europa.eu/tools/lotl/eu-lotl.xml`, which points to one list per member state; each list names the qualified trust services and the certificate that identifies each service. Those certificates are the trust anchors. `trust.territories` limits the download to the lists you need (`NL` for DUO's PKIoverheid CA, `RO` for certSIGN's timestamps). The lists are cached under `trust.cache_dir`, refreshed every `trust.refresh_hours` (default 24) in the background, and a stale cache is used when a list cannot be fetched. With `trust.fallback_to_pinned: true` the embedded PKIoverheid G3 and certSIGN roots (`backend/diploma/trust`) are used when the lists cannot be loaded at all; `trust.source: "pinned"` uses them always and works offline.

Known limits: the XAdES signatures on the lists are not verified (transport HTTPS is the integrity guarantee) and service status history is ignored (only the current status is used).

### Credential

The diploma credential type is not part of the Yivi scheme yet. A proposed `description.xml` with all attributes (bilingual names and descriptions) is in [`docs/scheme/diploma/description.xml`](docs/scheme/diploma/description.xml); the attribute names must stay in sync with `DiplomaAttributes` in `backend/jwt_creator.go`. One credential is issued per extract.

| Attribute | Value |
|-----------|-------|
| `documentType` | `Diploma` or `Getuigschrift` |
| `qualification` | e.g. `Hoger algemeen voortgezet onderwijs`, `HBO Bachelor Informatica Propedeuse` |
| `profile` | Profile(s) of a secondary education diploma, e.g. `Natuur en Techniek`; may be empty |
| `fullName` | Holder as printed (DUO does not split given names and surname) |
| `dateOfBirth` | YYYY-MM-DD |
| `institution`, `placeOfIssue` | As printed under *Uitgegeven door* |
| `dateAwarded` | YYYY-MM-DD |
| `nlqfLevel`, `eqfLevel` | e.g. `4`; empty when the extract prints no level |
| `documentNumber` | The number in the footer of the extract (for duo.nl/diplomacontrole) |
| `downloadDate` | YYYY-MM-DD, the day the extract was generated |
| `gradeList` | `yes` when the extract included the cijferlijst |
| `identitySource` | `brp`, `passport`, `id_card` or `driving_licence` |

## Getting started

### Prerequisites

- **Go** 1.27 or later (see `backend/go.mod`; the Go toolchain downloads it automatically)
- **Node.js** 24 or later (frontend)

No C toolchain or system libraries are needed: PDF parsing uses PDFium compiled to WebAssembly and signature verification is pure Go.

### Configuration

Create `local-secrets/config.json` (the folder is git-ignored); `config.sample.json` is a complete example:

```json
{
  "server_config": { "host": "0.0.0.0", "port": 8080, "enable_api_docs": true },
  "irma_server_url": "https://is.staging.yivi.app",
  "issuer_id": "diploma_issuer",
  "jwt_private_key_path": "/secrets/priv.pem",
  "sd_jwt_batch_size": 25,
  "credential_validity_days": 365,
  "credentials": {
    "diploma": { "full_credential": "pbdf-staging.pbdf.diploma" }
  },
  "identity_credentials": {
    "brp": "pbdf-staging.gemeente.personalData",
    "passport": "pbdf-staging.pbdf.passport",
    "id_card": "pbdf-staging.pbdf.idcard",
    "driving_licence": "pbdf-staging.pbdf.drivinglicence"
  },
  "trust": {
    "source": "eutl",
    "territories": ["NL", "RO"],
    "cache_dir": "/secrets/eutl-cache",
    "refresh_hours": 24,
    "fallback_to_pinned": true
  },
  "validation": { "ocsp": false },
  "max_upload_size_bytes": 5242880,
  "max_files": 10,
  "storage_type": "memory",
  "log_level": "info"
}
```

- `jwt_private_key_path` points to the RSA private key (PEM) that signs the session requests. The IRMA server must know the matching public key under the requestor name `issuer_id`, and that requestor must be allowed to **issue** the diploma credential and to **verify** the four identity credentials. The backend starts the disclosure session itself, so `irma_server_url` must be reachable from the backend as well as from the Yivi app.
- `identity_credentials` are the full credential type identifiers of the identity credentials; their attribute names (`firstnames`/`prefix`/`familyname`/`dateofbirth` for BRP, `firstName`/`lastName`/`dateOfBirth` for the documents) are fixed by the scheme. `passport`, `id_card` and `driving_licence` are required. `brp` is optional: leave the key out and the disclosure request only offers the three documents.
- `trust` configures where the signature trust anchors come from, see [Where the trust comes from](#where-the-trust-comes-from). Loading the EU lists at startup takes a few seconds when the cache is cold.
- `validation.ocsp` enables the online revocation check of DUO's signing certificate per upload (default off).
- `max_upload_size_bytes` bounds one PDF (default 5 MiB; a real extract is about 600 kB), `max_files` the number of extracts per upload (default 10).
- `storage_type` is `memory`, `redis` (with `redis_config`) or `redis_sentinel` (with `redis_sentinel_config`). Use Redis when running more than one instance.
- `sd_jwt_batch_size` is the number of SD-JWT VCs issued alongside each IRMA credential.

### Running the application

**Backend**

```bash
cd backend
go run . --config ../local-secrets/config.json
```

**Frontend** (development server on port 3000, proxies `/api` to the backend):

```bash
cd frontend
npm install
npm run dev
```

**Tests**

```bash
cd backend && go test ./...          # add -short to skip the tests that fetch the live EU trusted lists
cd frontend && npm test
```

The backend tests run the HTTP flow against fake validator, parser and IRMA server implementations and, when present, verify and parse the genuine DUO extracts in `backend/test-data`. Those extracts and the `expected.json` next to them (the values the parser should read from each) contain personal data and are not part of the repository; the tests that need them skip when they are absent. To run them locally, put your own extracts in `backend/test-data` and describe them in `backend/test-data/expected.json` (see `backend/diploma/parser_test.go` for the format).

**Command line check**

`backend/cmd/diplomacheck` verifies extracts from the command line and prints every check, handy when an upload is rejected:

```bash
cd backend
go run ./cmd/diplomacheck [-trust eutl|pinned] [-territories NL,RO] [-ocsp] [-json] file.pdf ...
```

### Docker

```bash
docker-compose up --build
```

This builds the frontend and backend into one image and starts it together with Redis. Mount `local-secrets` at `/secrets` (the compose file does); the EU list cache is written to `/secrets/eutl-cache` with the sample configuration.

### API documentation

The backend serves ReDoc at `/api/docs` when `enable_api_docs` is `true`. The OpenAPI specification is generated from the swag annotations in `backend/main.go`, `backend/server.go` and `backend/models`:

```bash
go install github.com/swaggo/swag/cmd/swag@latest
cd backend
go generate ./...
```

### API

| Endpoint | Purpose |
|----------|---------|
| `POST /api/diploma/upload` (multipart `file`, repeated) | Verify the signature of every extract and read it; returns a `session_id`, the holder and a per-file result (`files[]` with `accepted`, `validation`, `document` or `error`). 4xx/422 with `files` when no file was accepted; 503 `error:validation-service-unavailable` when the trust anchors are not available. |
| `POST /api/diploma/start-disclosure` `{session_id}` | Start the identity disclosure; returns the IRMA session package (`sessionPtr`, `frontendRequest`) for yivi-frontend. |
| `POST /api/diploma/issue` `{session_id}` | Fetch the disclosure result, compare it with the extracts and return the signed issuance JWT (one credential per extract) plus `irma_server_url` and `credentials`. `403 error:identity-mismatch` explains which of date of birth, surname and given names differed. |
| `GET /api/health` | Health check. |

Errors are JSON: `{"error": "error:<key>", "message": "...", "files": [...], "identity": {...}}`.

## Repository layout

```
backend/              Go backend
  diploma/            Extract parser (PDFium/WebAssembly), validator and trust store
    pdfsig/           Finds /ByteRange + /Contents in the raw PDF, extracts the CMS
    verify/           PAdES verification, returns a Result with per-check detail
    eutl/             Loads trust anchors from the EU Trusted Lists
    trust/            Pinned roots (PKIoverheid G3, certSIGN) for offline use, DUO issuer policy
  identity/           Name and date of birth comparison
  models/             API request/response models (swag annotated)
  docs/               Generated OpenAPI spec and ReDoc page
  cmd/diplomacheck/   Command line verifier
  test-data/          Genuine DUO extracts plus expected.json for the tests (local only, gitignored)
frontend/             React + Vite frontend (nl/en)
docs/scheme/diploma/  Proposed credential type for the Yivi scheme
```

## Not done (next steps)

- Verify the XAdES signatures on the LOTL and member state lists (the LOTL signing certificates are published in the EU Official Journal) and honour service status history.
- Read the grades from the cijferlijst pages and offer them as attributes.
- Replace the regex-based PDF signature scanning with a real parser if PDFs from other producers must be supported.

## Funding

This project builds on the Yivi issuers of the [Privacy by Design Foundation](https://privacybydesign.foundation).
