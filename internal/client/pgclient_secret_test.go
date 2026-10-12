package client_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable_cli/internal/client"
	"github.com/cybertec-postgresql/pg_timetable_cli/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSecretTask rewrites the first task of the given chain so it carries
// secret references in the three places the scheduler resolves them.
func seedSecretTask(t *testing.T, tc *testutils.PostgresTestContainer, chainID int, command, conn, param string) int64 {
	t.Helper()
	ctx := context.Background()
	var taskID int64
	require.NoError(t, tc.Pool.QueryRow(ctx,
		`UPDATE timetable.task SET command = $2, database_connection = NULLIF($3, '')
		 WHERE chain_id = $1 RETURNING task_id`, chainID, command, conn).Scan(&taskID))
	if param != "" {
		// add_job may already have created parameter 1 for this task.
		_, err := tc.Pool.Exec(ctx,
			`INSERT INTO timetable.parameter (task_id, order_id, value) VALUES ($1, 1, $2::jsonb)
			 ON CONFLICT (task_id, order_id) DO UPDATE SET value = EXCLUDED.value`,
			taskID, param)
		require.NoError(t, err)
	}
	return taskID
}

// TestListSecrets verifies stored secrets are listed as metadata, ordered by
// client and name.
func TestListSecrets(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	tc.AddSecret(t, "worker-2", "smtp_main", []byte("0123456789"))
	tc.AddSecret(t, "worker-1", "db_pass", []byte("abc"))

	c := newConnectedClient(t, tc)
	secrets, err := c.ListSecrets(context.Background())
	require.NoError(t, err)
	require.Len(t, secrets, 2)

	assert.Equal(t, "worker-1", secrets[0].ClientName)
	assert.Equal(t, "db_pass", secrets[0].SecretName)
	assert.Equal(t, 3, secrets[0].ValueBytes)
	assert.NotEmpty(t, secrets[0].UpdatedAt)
	assert.NotEmpty(t, secrets[0].UpdatedBy)

	assert.Equal(t, "worker-2", secrets[1].ClientName)
	assert.Equal(t, "smtp_main", secrets[1].SecretName)
	assert.Equal(t, 10, secrets[1].ValueBytes)
}

// TestListSecrets_NeverExposesValue is the regression guard for the Secret
// Store's central rule: the store has no plaintext read path, so nothing pgtt
// renders for a secret may carry the stored bytes.
func TestListSecrets_NeverExposesValue(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	const marker = "PLAINTEXT-CANARY"
	tc.AddSecret(t, "worker-1", "canary", []byte(marker))

	c := newConnectedClient(t, tc)
	secrets, err := c.ListSecrets(context.Background())
	require.NoError(t, err)
	require.Len(t, secrets, 1)

	rendered, err := json.Marshal(secrets[0])
	require.NoError(t, err)
	assert.NotContains(t, string(rendered), marker,
		"ListSecrets must never carry the stored value into its output")
}

// TestListSecretRefs verifies every ${secret:name} reference in a chain is
// found in all three places the scheduler resolves them, and that each is
// marked resolvable only when a matching secret row exists.
func TestListSecretRefs(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	id := seedJob(t, tc, "chain-secrets", "* * * * *")
	_, err := tc.Pool.Exec(ctx,
		`UPDATE timetable.chain SET client_name = 'worker-1' WHERE chain_id = $1`, id)
	require.NoError(t, err)
	seedSecretTask(t, tc, id,
		"SELECT '${secret:in_command}'",
		"host=db password=${secret:db_pass}",
		`{"pw": "${secret:in_param}", "${secret:in_key}": "literal", "n": 1}`)

	// Only one of the referenced secrets is actually stored.
	tc.AddSecret(t, "worker-1", "db_pass", []byte("enc"))

	c := newConnectedClient(t, tc)
	refs, err := c.ListSecretRefs(ctx, "chain-secrets")
	require.NoError(t, err)
	require.Len(t, refs, 2) // command text is never resolved

	byName := map[string]client.SecretRef{}
	for _, r := range refs {
		byName[r.SecretName] = r
		assert.Equal(t, id, r.ChainID)
		assert.Equal(t, "worker-1", r.ClientName)
	}

	require.Contains(t, byName, "db_pass")
	assert.Equal(t, "connection", byName["db_pass"].Location)
	assert.True(t, byName["db_pass"].Resolvable)
	assert.Empty(t, byName["db_pass"].Note)

	assert.NotContains(t, byName, "in_command")

	require.Contains(t, byName, "in_param")
	assert.Equal(t, "parameter", byName["in_param"].Location)
	assert.False(t, byName["in_param"].Resolvable)
	assert.NotContains(t, byName, "in_key") // object keys are not resolved
}

// TestListSecretRefs_NoClientName verifies a chain any worker may pick up is
// reported as unresolvable with an explanation, since no single
// timetable.secret row can be proven to be the one that will be used.
func TestListSecretRefs_NoClientName(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	id := seedJob(t, tc, "chain-anyworker", "* * * * *")
	seedSecretTask(t, tc, id, "SELECT 1", "host=db password=${secret:shared}", "")
	tc.AddSecret(t, "worker-1", "shared", []byte("enc"))

	c := newConnectedClient(t, tc)
	refs, err := c.ListSecretRefs(context.Background(), "chain-anyworker")
	require.NoError(t, err)
	require.Len(t, refs, 1)

	assert.False(t, refs[0].Resolvable)
	assert.Contains(t, refs[0].Note, "client_name")
}

// TestListSecretRefs_AllChains verifies an empty chain reference scans every
// chain and skips those without references.
func TestListSecretRefs_AllChains(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	withSecret := seedJob(t, tc, "chain-with", "* * * * *")
	seedSecretTask(t, tc, withSecret, "SELECT 1", "host=db password=${secret:a}", "")
	seedJob(t, tc, "chain-without", "* * * * *")

	c := newConnectedClient(t, tc)
	refs, err := c.ListSecretRefs(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "chain-with", refs[0].ChainName)
}
