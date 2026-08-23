#!/usr/bin/env bash
# Dynamically create throwaway test users in OpenLDAP for testing across Dovecot & Radicale.
set -euo pipefail

USERNAME="${1:-testuser_$(date +%s)_$RANDOM}"
DOMAIN="${2:-example.org}"
PASSWORD="${3:-Password123!}"
EMAIL="${USERNAME}@${DOMAIN}"

CONTAINER_NAME="${CONTAINER_NAME:-ldap-server}"
LDAP_BASE="ou=users,dc=example,dc=org"
ADMIN_DN="cn=admin,dc=example,dc=org"
ADMIN_PASS="${ADMIN_PASS:-admin}"

UID_NUM=$((1000 + RANDOM % 8999))

echo "Creating throwaway user: ${EMAIL} ..."

LDIF=$(cat <<EOF
dn: uid=${EMAIL},${LDAP_BASE}
objectClass: inetOrgPerson
objectClass: posixAccount
objectClass: shadowAccount
uid: ${EMAIL}
cn: ${USERNAME}
sn: TestUser
mail: ${EMAIL}
userPassword: ${PASSWORD}
uidNumber: ${UID_NUM}
gidNumber: ${UID_NUM}
homeDirectory: /home/${USERNAME}
EOF
)

if command -v docker >/dev/null 2>&1 && docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
  echo "$LDIF" | docker exec -i "${CONTAINER_NAME}" ldapadd -x -D "${ADMIN_DN}" -w "${ADMIN_PASS}"
  echo "Successfully added user ${EMAIL} to live OpenLDAP container!"
else
  echo "OpenLDAP container not running. Appending user to docker/ldap/users.ldif..."
  echo "" >> docker/ldap/users.ldif
  echo "$LDIF" >> docker/ldap/users.ldif
  echo "Appended user ${EMAIL} to docker/ldap/users.ldif."
fi

echo "Credentials:"
echo "  Username: ${EMAIL}"
echo "  Password: ${PASSWORD}"
