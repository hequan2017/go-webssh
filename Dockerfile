FROM golang:1.24.9-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /webssh .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /webssh /usr/local/bin/webssh
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["webssh"]
