# ---------- Frontend ----------
FROM node:24-slim AS frontend-build
WORKDIR /app/frontend
COPY frontend .
RUN npm ci
RUN npm run build

# ---------- Backend build (pure Go, no cgo: PDFium runs in WebAssembly) ----------
FROM golang:1.27-bookworm AS backend-build
WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend .
ENV CGO_ENABLED=0
RUN go build -o server

# ---------- Runtime ----------
FROM debian:bookworm-slim

# ca-certificates: TLS to the EU trusted lists (ec.europa.eu and the member state lists), DUO's OCSP responder and the IRMA server.
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

COPY --from=backend-build /app/backend/server /app/backend/server
COPY --from=frontend-build /app/frontend/build /app/frontend/build

WORKDIR /app/backend
EXPOSE 8080
CMD ["./server", "--config", "/secrets/config.json"]
