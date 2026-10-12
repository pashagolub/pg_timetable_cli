package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable_cli/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChainStart_WorkerRequired verifies that `chain start` without --worker
// returns errWorkerRequired immediately, before any DB connection is made
// (REQ-005, REQ-006 / AC-002b).
func TestChainStart_WorkerRequired(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"chain", "start", "42"}) // no --worker, no connstring
	err := root.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, errWorkerRequired)
}

// TestChainStop_WorkerRequired mirrors the above for `chain stop`.
func TestChainStop_WorkerRequired(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"chain", "stop", "42"})
	err := root.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, errWorkerRequired)
}

// TestChainDelete_NonTTYFailSafe verifies that `chain delete` without --yes on a
// non-interactive stream fails safe without contacting the DB (AC-008 / SEC-003).
func TestChainDelete_NonTTYFailSafe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	root := newRootCmd()
	root.SetArgs([]string{"chain", "delete", "my-chain"}) // no --yes, no --dsn → safe abort
	err = root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted")
}

// TestPrintSecretRefs_FlagsUnresolvable verifies the `chain show` footer names
// every referenced secret once and calls out only the ones that will fail to
// resolve at execution time.
func TestPrintSecretRefs_FlagsUnresolvable(t *testing.T) {
	orig := opts.output
	t.Cleanup(func() { opts.output = orig })

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	// newRootCmd binds --output to opts.output and resets it to the flag
	// default, so the format must be chosen after the tree is built.
	opts.output = string(outputTable)

	printSecretRefs(cmd, []client.SecretRef{
		{SecretName: "db_pass", TaskID: 7, Location: "connection", Resolvable: true},
		{SecretName: "smtp", TaskID: 8, Location: "parameter", Note: "no timetable.secret row for client \"worker-1\""},
	})

	got := out.String()
	assert.Contains(t, got, "Secrets: db_pass, smtp")
	assert.Contains(t, got, "! smtp (task 8, parameter)")
	assert.Contains(t, got, "worker-1")
	assert.NotContains(t, got, "! db_pass", "a resolvable reference needs no warning")
}

// TestPrintSecretRefs_QuietWhenNothingToSay verifies the footer is omitted for
// chains without references, and in JSON output where the refs are already
// part of the document.
func TestPrintSecretRefs_QuietWhenNothingToSay(t *testing.T) {
	orig := opts.output
	t.Cleanup(func() { opts.output = orig })

	refs := []client.SecretRef{{SecretName: "smtp", TaskID: 1, Location: "command"}}

	// newRootCmd resets opts.output to the --output default, so each format is
	// selected after its command tree is built.
	var noRefs bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&noRefs)
	opts.output = string(outputTable)
	printSecretRefs(cmd, nil)
	assert.Empty(t, noRefs.String())

	var asJSON bytes.Buffer
	cmd = newRootCmd()
	cmd.SetOut(&asJSON)
	opts.output = string(outputJSON)
	printSecretRefs(cmd, refs)
	assert.Empty(t, asJSON.String())
}
