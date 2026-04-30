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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.temporal.io/server/common/auth"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/persistence/sql/sqlplugin/postgresql/driver"
)

// envOrDefault returns the value of the named env var, or fallback if empty.
func envOrDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func TestKerberosE2E_KeytabAuth(t *testing.T) {
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

type noopResolver struct{}

func (noopResolver) Resolve(addr string) []string { return []string{addr} }
