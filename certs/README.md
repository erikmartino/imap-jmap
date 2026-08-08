# Place a browser-trusted TLS cert here for imap-jmap (mounted at /certs by docker-compose.bulwark.yml).
#
#   mkcert -install
#   mkcert -cert-file certs/cert.pem -key-file certs/key.pem localhost 127.0.0.1
#
# The compose sets TLS_CERT_FILE=/certs/cert.pem and TLS_KEY_FILE=/certs/key.pem.
# If these files are absent, imap-jmap falls back to a self-signed certificate.
# cert.pem / key.pem are gitignored (never commit private keys).
