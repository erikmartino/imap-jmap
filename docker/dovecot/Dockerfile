FROM dovecot/dovecot:latest-root

# The official image does not ship the LDAP auth driver; add it from Debian's
# packages. This also installs Debian's dovecot-core, so the config below is a
# self-contained /etc/dovecot/dovecot.conf that does not include conf.d/*.
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        dovecot-imapd dovecot-ldap dovecot-lmtpd && \
    rm -rf /var/lib/apt/lists/*

RUN mkdir -p /srv/vmail && chown -R vmail:vmail /srv/vmail

COPY docker/dovecot/entrypoint.sh /usr/local/bin/dovecot-entrypoint
RUN chmod +x /usr/local/bin/dovecot-entrypoint

EXPOSE 143 24 12345

ENTRYPOINT ["/usr/local/bin/dovecot-entrypoint"]
