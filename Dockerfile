FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ucollection ./cmd/ucollection

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/ucollection /ucollection
COPY --from=builder /src/web/static /web/static

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/ucollection"]
