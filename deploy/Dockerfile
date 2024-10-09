# Use a base golang image to build the CSI driver
FROM golang:1.22 as builder

# Set working directory inside the container
WORKDIR /app

# Copy the Go modules files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the custom CSI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /custom-csi ./cmd/main.go

# Final minimal image
FROM alpine:latest

# Working directory inside the final container
WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /custom-csi .

# Set the entrypoint to the binary
ENTRYPOINT ["./custom-csi"]
