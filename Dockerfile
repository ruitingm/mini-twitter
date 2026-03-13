FROM golang:1.22-alpine AS builder
ARG SERVICE
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/service ./cmd/${SERVICE}

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/service /bin/service
ENTRYPOINT ["/bin/service"]
