package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecretCmd_NoWriteSubcommands is the guard on pgtt's side of the Secret
// Store trust boundary: pgtt reads the store's metadata and never writes,
// decrypts or prints a value, so no write verb may be added without revisiting
// that decision (see newSecretCmd).
func TestSecretCmd_NoWriteSubcommands(t *testing.T) {
	var names []string
	for _, sub := range newSecretCmd().Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{"list", "refs"}, names)
}

// TestSecretCmd_Registered verifies `pgtt secret` is reachable from the root
// command tree.
func TestSecretCmd_Registered(t *testing.T) {
	var found bool
	for _, sub := range newRootCmd().Commands() {
		if sub.Name() == "secret" {
			found = true
		}
	}
	assert.True(t, found, "secret command must be registered on the root command")
}

// TestSecretRefsCmd_AcceptsOptionalChain verifies `secret refs` takes at most
// one positional argument: the chain, since the connection comes from --dsn,
// PGTT_CONNSTR or the libpq environment.
func TestSecretRefsCmd_AcceptsOptionalChain(t *testing.T) {
	cmd := newSecretRefsCmd()
	require.NoError(t, cmd.Args(cmd, []string{}))
	require.NoError(t, cmd.Args(cmd, []string{"my-chain"}))
	assert.Error(t, cmd.Args(cmd, []string{"my-chain", "extra"}))
}

// TestSecretCmdHelp_StatesNoPlaintextPath verifies the help text tells
// operators why pgtt cannot show them a value, rather than leaving them to
// discover the missing verb.
func TestSecretCmdHelp_StatesNoPlaintextPath(t *testing.T) {
	long := strings.ToLower(newSecretCmd().Long)
	assert.Contains(t, long, "never reads, writes or prints secret values")
	assert.Contains(t, long, "pgp_sym_encrypt")
}
