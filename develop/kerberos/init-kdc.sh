#!/bin/bash
# Initializes a fresh KDC database, creates the test principals,
# generates keytabs into the shared volume, and starts krb5kdc in
# the foreground.
set -euo pipefail

REALM="${REALM:-EXAMPLE.LOCAL}"
SHARED_DIR="${SHARED_DIR:-/shared}"
MASTER_PASS="${MASTER_PASS:-masterpassword}"

POSTGRES_KEYTAB="${SHARED_DIR}/postgres.keytab"
TEMPORAL_KEYTAB="${SHARED_DIR}/temporal.keytab"

mkdir -p "${SHARED_DIR}"

if [ ! -f /var/lib/krb5kdc/principal ]; then
    echo "Initializing KDC database for realm ${REALM}..."
    kdb5_util create -s -r "${REALM}" -P "${MASTER_PASS}"

    # Allow the admin principal to do anything (only used internally for ktadd)
    echo "*/admin@${REALM} *" > /var/lib/krb5kdc/kadm5.acl

    echo "Creating principals..."
    kadmin.local -q "addprinc -randkey postgres/localhost@${REALM}"
    kadmin.local -q "addprinc -randkey temporal@${REALM}"

    echo "Generating keytabs into ${SHARED_DIR}..."
    rm -f "${POSTGRES_KEYTAB}" "${TEMPORAL_KEYTAB}"
    kadmin.local -q "ktadd -k ${POSTGRES_KEYTAB} postgres/localhost@${REALM}"
    kadmin.local -q "ktadd -k ${TEMPORAL_KEYTAB} temporal@${REALM}"

    chmod 644 "${POSTGRES_KEYTAB}" "${TEMPORAL_KEYTAB}"
    echo "KDC initialization complete."
else
    echo "KDC database already exists, skipping initialization."
fi

echo "Starting krb5kdc in foreground..."
exec krb5kdc -n
