#!/bin/sh
set -e

# The vmail-data volume starts root-owned; Dovecot runs as vmail (uid/gid 1000)
# and must be able to create per-user Maildirs under /srv/vmail.
chown -R vmail:vmail /srv/vmail 2>/dev/null || true

exec /usr/sbin/dovecot -F
