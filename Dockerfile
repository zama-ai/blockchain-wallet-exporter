ARG ARCH="amd64"
ARG OS="linux"
FROM golang:1.22.3 as builder

RUN apt-get update && apt-get install -y \
    make \
    git \
    gcc \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /blockchain-wallet-exporter
COPY . .

ARG VERSION=undefined
RUN CGO_ENABLED=0 \
    go build -ldflags "-s -w -X main.version=$VERSION -X main.commit=$(git rev-parse HEAD) -X main.date=$(date +%Y-%m-%d-%H:%M:%S)" -o blockchain-wallet-exporter ./cmd/main.go 
RUN chmod +x blockchain-wallet-exporter

#FROM scratch
FROM quay.io/prometheus/busybox-${OS}-${ARCH}:latest

EXPOSE 9090

COPY --from=builder /blockchain-wallet-exporter/blockchain-wallet-exporter /blockchain-wallet-exporter
ENTRYPOINT ["/blockchain-wallet-exporter"]
