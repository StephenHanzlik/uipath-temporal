# Kerberos E2E test fixture

This directory holds the docker-compose fixture used to exercise the
PostgreSQL Kerberos (GSSAPI) authentication path implemented under
`common/persistence/sql/sqlplugin/postgresql/session`.

## What it does

`docker-compose.kerberos.yml` (in the sibling `docker-compose/` dir)
brings up two containers:

- **`temporal-dev-krb5-kdc`** — an Alpine-based MIT KDC. On first
  start it creates the `EXAMPLE.LOCAL` realm, registers the
  principals `postgres/localhost@EXAMPLE.LOCAL` and
  `temporal@EXAMPLE.LOCAL`, and writes their keytabs into the host
  directory `develop/kerberos/keytabs/`. It binds host port 88 (TCP +
  UDP).
- **`temporal-dev-krb5-postgres`** — the official `postgres:13.5`
  image (built `--with-gssapi`) with the postgres keytab and a
  `pg_hba.conf` that requires `gss` for all TCP connections. It
  binds host port 5433.

## Bring it up

```bash
cd develop/docker-compose
docker compose -f docker-compose.kerberos.yml up --build -d
```

Wait a few seconds for the KDC to seed its database, then verify the
keytabs were created:

```bash
ls develop/kerberos/keytabs/
# postgres.keytab  temporal.keytab
```

## Run the E2E test

The test is gated behind a `kerberos` build tag so it never runs in
default CI:

```bash
go test -tags kerberos -count=1 -timeout 60s \
    ./common/persistence/sql/sqlplugin/postgresql/session/...
```

Override fixture paths or hostnames via env vars when running outside
the default layout:

| Env var                   | Default                                                              |
| ------------------------- | -------------------------------------------------------------------- |
| `TEMPORAL_TEST_KEYTAB`    | `develop/kerberos/keytabs/temporal.keytab` (relative to the test)    |
| `KRB5_CONFIG`             | `develop/kerberos/krb5.conf` (relative to the test)                  |
| `TEMPORAL_TEST_PG_ADDR`   | `127.0.0.1:5433`                                                     |
| `TEMPORAL_TEST_KRB_REALM` | `EXAMPLE.LOCAL`                                                      |
| `TEMPORAL_TEST_KRB_USER`  | `temporal`                                                           |

If the keytab or krb5.conf is missing, the test skips with a clear
message instead of failing — so it's safe to run in environments
without the fixture.

## Tear it down

```bash
cd develop/docker-compose
docker compose -f docker-compose.kerberos.yml down -v
rm -rf ../kerberos/keytabs
```

The `-v` removes the bind volume content reference; the
`rm -rf ../kerberos/keytabs` clears the host-side keytabs so the next
`up` regenerates them cleanly.

## Troubleshooting

- **`Cannot find KDC for requested realm`** — the KDC isn't reachable
  on `127.0.0.1:88`. Confirm with `docker compose ps` and
  `docker logs temporal-dev-krb5-kdc`.
- **`Clock skew too great`** — Kerberos requires the test host's
  clock to be within ±5 minutes of the KDC. Containers usually
  inherit the host clock, but Docker Desktop on macOS occasionally
  drifts after sleep. Restart Docker Desktop.
- **`Server not found in Kerberos database`** — the Postgres SPN in
  the keytab doesn't match what the client requests. The fixture
  uses `postgres/localhost@EXAMPLE.LOCAL`; the test config's
  `kerberos.spn` field must match.
- **`permission denied for keytab`** — re-run `docker compose up`;
  `init-kdc.sh` re-chmods the keytabs to 644 on each fresh init.
- **Port 88 already bound** — another KDC or a packet capture is
  holding it. Stop that, or temporarily edit the compose file to map
  to `8888:88` and update `krb5.conf` to match.

## What the fixture does NOT cover

- Cross-realm trusts (the test fixture has one realm).
- Active Directory-specific quirks like FAST disablement, RC4-HMAC
  enctypes, or constrained delegation. The unit tests cover the
  config wiring for these (`disableFAST`); a full AD-equivalent
  fixture would need Samba 4 or a real AD lab.
- Credential cache (ccache) authentication. The fixture uses keytab
  auth because it's the production-server case; ccache works
  identically from the test code's perspective if you `kinit` first
  and point the test at the resulting cache file via
  `kerberos.credentialCacheFile`.
