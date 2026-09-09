# React frontend

This directory contains the React + TypeScript frontend of the diploma issuer, built with Vite.

The flow has three pages:

1. `/nl` – explains where to download the diploma extracts (Mijn diploma's of DUO, log in with DigiD) and how the check works.
2. `/nl/upload` – upload one or more diploma extract PDFs; the backend verifies the DUO signature of every file and shows what it read, per file. Rejected files are explained per file; the user can continue with the accepted ones.
3. `/nl/verify` – prove your identity in the Yivi app (BRP, passport, ID card or driving licence); the backend compares it with the extracts and, on a match, returns the issuance request (one credential per diploma) that is handed to the Yivi app.
4. `/nl/done` – the diplomas are in the app.

## Development

```
npm install
npm run dev
```

The dev server proxies `/api` to the backend on `http://localhost:8080`.

## Tests

```
npm test
```

## Build

```
npm run build
```
