# ---------- Frontend ----------
FROM node:24-slim AS frontend-build
WORKDIR /app/frontend
COPY frontend .
RUN npm ci
RUN npm run build

# ---------- Backend build (pure Go, no cgo: PDFium runs in WebAssembly) ----------
FROM golang:1.27.3-alpine AS backend-build
WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend .
ENV CGO_ENABLED=0
RUN go build -o server

# Create application user
RUN adduser -D -u 1000 -g nonroot nonroot

# ---------- Runtime ----------
FROM scratch

# Ensure the application user and group is set
COPY --from=backend-build /etc/passwd /etc/passwd
COPY --from=backend-build /etc/group /etc/group
COPY --from=backend-build --chown=nonroot:nonroot /home/nonroot/ /home/nonroot/

# Switch to application user
USER nonroot

COPY --from=backend-build /app/backend/server /app/backend/server
COPY --from=frontend-build /app/frontend/build /app/frontend/build

WORKDIR /app/backend
EXPOSE 8080
CMD ["./server", "--config", "/secrets/config.json"]
