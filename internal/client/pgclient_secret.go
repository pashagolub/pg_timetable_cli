package client

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// secretRefPattern matches a ${secret:name} reference. The character class
// mirrors the secret_name_format CHECK constraint on timetable.secret and the
// pattern used by the scheduler (pg_timetable internal/pgengine/secrets.go).
var secretRefPattern = regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.-]+)\}`)

// ListSecrets returns the metadata of every row in timetable.secret, ordered
// by client and name.
//
// It deliberately never selects value_enc: the Secret Store has no plaintext
// read path by design (pg_timetable migrations/00820.sql), and pgtt does not
// add one. Only the encrypted column's size is reported, as ValueBytes.
func (c *PgClient) ListSecrets(ctx context.Context) ([]SecretInfo, error) {
	const q = `
SELECT
    client_name,
    secret_name,
    octet_length(value_enc)                      AS value_bytes,
    to_char(created_at, 'YYYY-MM-DD HH24:MI:SS') AS created_at,
    to_char(updated_at, 'YYYY-MM-DD HH24:MI:SS') AS updated_at,
    updated_by
FROM timetable.secret
ORDER BY client_name, secret_name`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[SecretInfo])
}

// ListSecretRefs returns every ${secret:name} reference found in the chain
// definitions, one row per (task, location, secret name).
//
// chainRef selects a single chain by numeric id or name; "" scans all chains.
// References are read from the two places the scheduler resolves them:
// task.database_connection and the string leaves of parameter.value.
// task.command is never resolved, so placeholders there are ignored.
//
// Each reference is marked Resolvable when a timetable.secret row exists for
// the owning chain's client_name. A chain with a NULL/empty client_name can be
// picked up by any worker, so no single row can be proven to match; those are
// reported with Resolvable false and Note explaining why.
func (c *PgClient) ListSecretRefs(ctx context.Context, chainRef string) ([]SecretRef, error) {
	const q = `
SELECT
    c.chain_id,
    c.chain_name,
    COALESCE(c.client_name, '')          AS client_name,
    t.task_id,
    COALESCE(t.task_name, '')            AS task_name,
    src.location,
    src.text
FROM timetable.chain c
JOIN timetable.task t ON t.chain_id = c.chain_id
CROSS JOIN LATERAL (
    SELECT 'connection' AS location, t.database_connection AS text
    UNION ALL
    SELECT 'parameter'  AS location, p.value::text         AS text
      FROM timetable.parameter p WHERE p.task_id = t.task_id
) src
WHERE src.text LIKE '%%${secret:%%'
  AND %s
ORDER BY c.chain_id, t.task_order, src.location`

	var (
		rows pgx.Rows
		err  error
	)
	switch chainRef {
	case "":
		rows, err = c.pool.Query(ctx, fmt.Sprintf(q, "TRUE"))
	default:
		if id, convErr := strconv.Atoi(chainRef); convErr == nil {
			rows, err = c.pool.Query(ctx, fmt.Sprintf(q, "c.chain_id = $1"), id)
		} else {
			rows, err = c.pool.Query(ctx, fmt.Sprintf(q, "c.chain_name = $1"), chainRef)
		}
	}
	if err != nil {
		return nil, err
	}

	type refRow struct {
		ChainID    int    `db:"chain_id"`
		ChainName  string `db:"chain_name"`
		ClientName string `db:"client_name"`
		TaskID     int64  `db:"task_id"`
		TaskName   string `db:"task_name"`
		Location   string `db:"location"`
		Text       string `db:"text"`
	}
	found, err := pgx.CollectRows(rows, pgx.RowToStructByName[refRow])
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}

	// Which (client_name, secret_name) pairs actually exist.
	secrets, err := c.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	stored := make(map[string]bool, len(secrets))
	for _, s := range secrets {
		stored[s.ClientName+"\x00"+s.SecretName] = true
	}

	var out []SecretRef
	seen := map[string]bool{}
	for _, r := range found {
		text := r.Text
		if r.Location == "parameter" {
			text = jsonStringLeaves(text)
		}
		for _, name := range extractSecretRefs(text) {
			key := strconv.Itoa(r.ChainID) + "\x00" + strconv.FormatInt(r.TaskID, 10) +
				"\x00" + r.Location + "\x00" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			ref := SecretRef{
				ChainID:    r.ChainID,
				ChainName:  r.ChainName,
				ClientName: r.ClientName,
				TaskID:     r.TaskID,
				TaskName:   r.TaskName,
				Location:   r.Location,
				SecretName: name,
			}
			switch {
			case r.ClientName == "":
				ref.Note = "chain has no client_name; any worker may run it, so the owning secret row cannot be determined"
			case stored[r.ClientName+"\x00"+name]:
				ref.Resolvable = true
			default:
				ref.Note = "no timetable.secret row for client " + strconv.Quote(r.ClientName)
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

// jsonStringLeaves joins the string values of a JSON document, skipping object
// keys and non-string content the scheduler never resolves.
func jsonStringLeaves(doc string) string {
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		return ""
	}
	var leaves []string
	var walk func(any)
	walk = func(n any) {
		switch x := n.(type) {
		case string:
			leaves = append(leaves, x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return strings.Join(leaves, "\n")
}

// extractSecretRefs returns the unique ${secret:name} identifiers in s, in
// order of first appearance.
func extractSecretRefs(s string) []string {
	if !strings.Contains(s, "${secret:") {
		return nil
	}
	var (
		names []string
		seen  = map[string]bool{}
	)
	for _, m := range secretRefPattern.FindAllStringSubmatch(s, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		names = append(names, m[1])
	}
	return names
}

// SecretRefNames returns the distinct secret names referenced by refs, sorted.
func SecretRefNames(refs []SecretRef) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if seen[r.SecretName] {
			continue
		}
		seen[r.SecretName] = true
		out = append(out, r.SecretName)
	}
	sort.Strings(out)
	return out
}
