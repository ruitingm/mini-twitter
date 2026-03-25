FROM golang:1.25-alpine AS builder
ARG SERVICE
WORKDIR /app

# Install git (needed for some go modules)
RUN apk add --no-cache git

# Copy go files first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy rest of project
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/service ./cmd/${SERVICE}

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/service /bin/service
ENTRYPOINT ["/bin/service"]

# docker-compose up --build