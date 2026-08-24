# Default TLS cert/key for imap-jmap (mounted at /certs in docker-compose.yml).
#
# A default self-signed development certificate (cert.pem / key.pem) is tracked
# in this directory so that certificate trust is preserved across restarts and clones.
#
# To use a locally trusted mkcert certificate instead:
#   mkcert -install
#   mkcert -cert-file certs/cert.pem -key-file certs/key.pem localhost 127.0.0.1 imap-jmap
#
# The compose sets TLS_CERT_FILE=/certs/cert.pem and TLS_KEY_FILE=/certs/key.pem.
