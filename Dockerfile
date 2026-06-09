# ── Build stage ──
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/authsvc ./cmd/authsvc
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/gateway ./cmd/gateway

# ── AuthSvc image ──
FROM gcr.io/distroless/static-debian12 AS authsvc
COPY --from=builder /bin/authsvc /authsvc
EXPOSE 50051
ENTRYPOINT ["/authsvc"]

# ── Gateway image ──
FROM gcr.io/distroless/static-debian12 AS gateway
COPY --from=builder /bin/gateway /gateway
COPY web/ /web/
EXPOSE 8080
ENTRYPOINT ["/gateway"]
