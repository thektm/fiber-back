FROM golang:1.21-alpine AS builder
RUN apk add --no-cache git build-base ca-certificates
WORKDIR /src

# Download dependencies early
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
WORKDIR /src/cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/server

FROM alpine:3.18
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/server /app/server
WORKDIR /app
ENV PORT=3001
EXPOSE 3001
CMD ["/app/server"]
