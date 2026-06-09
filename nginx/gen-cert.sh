#!/bin/sh
# Generate self-signed TLS certificate for development/testing.
# For production, use Let's Encrypt or your CA's certificates.
# Usage: ./nginx/gen-cert.sh

set -e

CERT_DIR="$(dirname "$0")/certs"
mkdir -p "$CERT_DIR"

if [ -f "$CERT_DIR/server.crt" ] && [ -f "$CERT_DIR/server.key" ]; then
    echo "Certificates already exist in $CERT_DIR. Remove them to regenerate."
    exit 0
fi

openssl req -x509 -nodes -days 365 \
    -newkey rsa:2048 \
    -keyout "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.crt" \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

echo "Self-signed certificate generated in $CERT_DIR/"
echo "  server.crt  — certificate"
echo "  server.key  — private key"
echo ""
echo "NOTE: For production, replace with real certificates (e.g. Let's Encrypt)."
