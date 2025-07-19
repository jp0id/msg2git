# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
# RUN go env -w GOPROXY=https://goproxy.cn,direct
# 设置代理加速下载
RUN go env -w GOPROXY=https://goproxy.cn,direct
RUN go env -w GOSUMDB=sum.golang.google.cn
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build the application
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o msg2git .

# Runtime stage
FROM alpine:latest

WORKDIR /app

# 安装 ca-certificates
RUN apk --no-cache add ca-certificates

# Copy built binary from builder
COPY --from=builder /app/msg2git .

# Copy .env file (you can also use environment variables directly)
COPY .prod.env .env

# Create directory for notes
RUN mkdir -p /app/notes-repo

# Expose port if needed (though this is a bot, not a web server)
EXPOSE 80

# Run the application
CMD ["./msg2git"]

