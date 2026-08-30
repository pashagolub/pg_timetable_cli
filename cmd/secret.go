package cmd

import (
	"context"
	"strconv"

	"github.com/cybertec-postgresql/pg_timetable_cli/internal/client"
	"github.com/spf13/cobra"
)

// newSecretCmd builds the `pgtt secret` group: a read-only view of the
// pg_timetable Secret Store (timetable.secret).
//
// pgtt deliberately offers no write path. The store's security model
// (pg_timetable docs/explanation-secret-store-security-model.md) keeps the
// encryption key out of the database entirely and decrypts only inside the
// scheduler; a management CLI that encrypted or printed values would have to
// handle plaintext and the key, widening that boundary. Operators write
// secrets with pgp_sym_encrypt as documented upstream. These commands answer
// the questions pgtt can answer safely: what is stored, and what references it.
func newSecretCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "secret",
		Short: "Inspect the pg_timetable Secret Store (read-only)",
		Long: "Inspect timetable.secret and the ${secret:name} references in your chains.\n\n" +
			"pgtt never reads, writes or prints secret values: the Secret Store has no\n" +
			"plaintext read path by design. Use pgp_sym_encrypt to store a value, as\n" +
			"described in the pg_timetable Secret Store documentation.",
	}
	c.AddCommand(newSecretListCmd())
	c.AddCommand(newSecretRefsCmd())
	return c
}

// newSecretListCmd lists timetable.secret metadata (never values).
func newSecretListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [connstring]",
		Short: "List stored secrets (names and metadata only)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, c client.Client) error {
				secrets, err := c.ListSecrets(ctx)
				if err != nil {
					return err
				}
				headers := []string{"CLIENT", "SECRET", "BYTES", "UPDATED", "UPDATED BY"}
				rows := make([][]string, 0, len(secrets))
				for _, s := range secrets {
					rows = append(rows, []string{
						s.ClientName,
						s.SecretName,
						strconv.Itoa(s.ValueBytes),
						s.UpdatedAt,
						s.UpdatedBy,
					})
				}
				return render(cmd.OutOrStdout(), secrets, headers, rows)
			})
		},
	}
}

// newSecretRefsCmd reports where ${secret:name} references appear in chains
// and whether each one has a matching secret row.
func newSecretRefsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refs [chain-id|name]",
		Short: "Show ${secret:name} references in chains and whether they resolve",
		Long: "Show every ${secret:name} reference in the chain definitions, and whether a\n" +
			"matching timetable.secret row exists for the owning chain's client_name.\n\n" +
			"With no argument, all chains are scanned. The connection comes from --dsn,\n" +
			"PGTT_CONNSTR or the libpq environment variables.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var chainRef string
			if len(args) > 0 {
				chainRef = args[0]
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				refs, err := c.ListSecretRefs(ctx, chainRef)
				if err != nil {
					return err
				}
				headers := []string{"CHAIN", "TASK", "WHERE", "SECRET", "RESOLVES", "NOTE"}
				rows := make([][]string, 0, len(refs))
				for _, r := range refs {
					resolves := "no"
					if r.Resolvable {
						resolves = "yes"
					}
					rows = append(rows, []string{
						strconv.Itoa(r.ChainID) + " " + r.ChainName,
						strconv.FormatInt(r.TaskID, 10),
						r.Location,
						r.SecretName,
						resolves,
						r.Note,
					})
				}
				return render(cmd.OutOrStdout(), refs, headers, rows)
			})
		},
	}
}
