//go:build kerberos

// End-to-end test for the Kerberos GSSAPI authentication path.
// Requires a running KDC and Kerberos-authenticated Postgres; see
// develop/kerberos/README.md for the docker-compose fixture.
//
// Run with:
//   go test -tags kerberos -count=1 -timeout 60s \
//     ./common/persistence/sql/sqlplugin/postgresql/session/...

package session

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.temporal.io/server/common/auth"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/persistence/sql/sqlplugin/postgresql/driver"
)

// resetGSSProviderForTesting wipes the package-level Once and cached
// kerberos config so a subsequent NewSession call can register a
// fresh GSS provider with a different config. Production code uses
// the once-and-rejection-on-mismatch behavior to prevent two SQL
// stores from racing on different Kerberos settings; tests need to
// bypass it to exercise both the keytab and ccache code paths in one
// process.
func resetGSSProviderForTesting(t *testing.T) {
	t.Helper()
	gssProviderOnce = sync.Once{}
	gssProviderCfg = nil
}

// envOrDefault returns the value of the named env var, or fallback if empty.
func envOrDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// parallelize:ignore - mutates package-level gssProviderOnce
func TestKerberosE2E_KeytabAuth(t *testing.T) {
	resetGSSProviderForTesting(t)
	// Defaults assume the docker-compose.kerberos.yml fixture is up
	// and that keytabs landed in develop/kerberos/keytabs/.
	keytab := envOrDefault("TEMPORAL_TEST_KEYTAB", "../../../../../../develop/kerberos/keytabs/temporal.keytab")
	krb5Conf := envOrDefault("KRB5_CONFIG", "../../../../../../develop/kerberos/krb5.conf")
	addr := envOrDefault("TEMPORAL_TEST_PG_ADDR", "127.0.0.1:5433")
	realm := envOrDefault("TEMPORAL_TEST_KRB_REALM", "EXAMPLE.LOCAL")
	user := envOrDefault("TEMPORAL_TEST_KRB_USER", "temporal")

	if _, err := os.Stat(keytab); err != nil {
		t.Skipf("keytab not found at %q (set TEMPORAL_TEST_KEYTAB or bring up develop/docker-compose/docker-compose.kerberos.yml): %v", keytab, err)
	}
	if _, err := os.Stat(krb5Conf); err != nil {
		t.Skipf("krb5.conf not found at %q: %v", krb5Conf, err)
	}

	cfg := &config.SQL{
		PluginName:      "postgres12_pgx",
		DatabaseName:    "temporal",
		ConnectAddr:     addr,
		ConnectProtocol: "tcp",
		User:            user,
		Kerberos: &auth.Kerberos{
			Enabled:     true,
			Username:    user,
			Realm:       realm,
			KeytabFile:  keytab,
			ConfigFile:  krb5Conf,
			ServiceName: "postgres",
			// SPN must match what the Postgres keytab holds (service
			// and host components only; the realm is taken from
			// krb5.conf default_realm). Fixture principal is
			// postgres/localhost@EXAMPLE.LOCAL.
			SPN: "postgres/localhost",
		},
	}

	sess, err := NewSession(cfg, &driver.PGXDriver{}, &noopResolver{})
	require.NoError(t, err, "NewSession with kerberos config should succeed against the test fixture")
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sess.PingContext(ctx), "ping over kerberos-authenticated connection should succeed")

	var who string
	err = sess.GetContext(ctx, &who, "SELECT current_user")
	require.NoError(t, err)
	require.Equal(t, user, who, "current_user should match the kerberos principal (with realm stripped via include_realm=0)")
}

// parallelize:ignore - mutates package-level gssProviderOnce
func TestKerberosE2E_CCacheAuth(t *testing.T) {
	resetGSSProviderForTesting(t)
	// Same fixture as the keytab test; this exercises the alternate
	// credential source path in newKrb5Client (CredentialCacheFile
	// instead of KeytabFile). init-kdc.sh refreshes this ccache on
	// every container start so it shouldn't be stale.
	ccache := envOrDefault("TEMPORAL_TEST_CCACHE", "../../../../../../develop/kerberos/keytabs/temporal.ccache")
	krb5Conf := envOrDefault("KRB5_CONFIG", "../../../../../../develop/kerberos/krb5.conf")
	addr := envOrDefault("TEMPORAL_TEST_PG_ADDR", "127.0.0.1:5433")
	realm := envOrDefault("TEMPORAL_TEST_KRB_REALM", "EXAMPLE.LOCAL")
	user := envOrDefault("TEMPORAL_TEST_KRB_USER", "temporal")

	if _, err := os.Stat(ccache); err != nil {
		t.Skipf("ccache not found at %q (set TEMPORAL_TEST_CCACHE or bring up develop/docker-compose/docker-compose.kerberos.yml): %v", ccache, err)
	}
	if _, err := os.Stat(krb5Conf); err != nil {
		t.Skipf("krb5.conf not found at %q: %v", krb5Conf, err)
	}

	cfg := &config.SQL{
		PluginName:      "postgres12_pgx",
		DatabaseName:    "temporal",
		ConnectAddr:     addr,
		ConnectProtocol: "tcp",
		User:            user,
		Kerberos: &auth.Kerberos{
			Enabled:             true,
			Realm:               realm,
			CredentialCacheFile: ccache,
			ConfigFile:          krb5Conf,
			ServiceName:         "postgres",
			SPN:                 "postgres/localhost",
		},
	}

	sess, err := NewSession(cfg, &driver.PGXDriver{}, &noopResolver{})
	require.NoError(t, err, "NewSession with kerberos ccache config should succeed against the test fixture")
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sess.PingContext(ctx))

	var who string
	err = sess.GetContext(ctx, &who, "SELECT current_user")
	require.NoError(t, err)
	require.Equal(t, user, who)
}

type noopResolver struct{}

func (noopResolver) Resolve(addr string) []string { return []string{addr} }
