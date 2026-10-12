package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractSecretRefs covers the reference syntax without a database. The
// accepted character class mirrors the secret_name_format CHECK constraint on
// timetable.secret and the scheduler's own regexp (pg_timetable
// internal/pgengine/secrets.go), so a name pgtt reports is a name the
// scheduler will try to resolve.
func TestExtractSecretRefs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"no reference", "SELECT 1", nil},
		{"plain", "SELECT '${secret:pw}'", []string{"pw"}},
		{"two in one string", "host=h user=${secret:u} password=${secret:p}", []string{"u", "p"}},
		{"deduped, first appearance order", "${secret:b} ${secret:a} ${secret:b}", []string{"b", "a"}},
		{"full character class", "${secret:My_secret.name-1}", []string{"My_secret.name-1"}},
		{"empty name is not a reference", "${secret:}", nil},
		{"illegal character ends the name", "${secret:a b}", nil},
		{"conninfo single-quoted form", "password='${secret:pw}'", []string{"pw"}},
		{"inside json", `{"pw": "${secret:in_param}"}`, []string{"in_param"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractSecretRefs(tt.in))
		})
	}
}

// TestSecretRefNames verifies the footer helper reports each referenced name
// once, sorted, however many tasks and locations it came from.
func TestSecretRefNames(t *testing.T) {
	refs := []SecretRef{
		{SecretName: "smtp", Location: "parameter", TaskID: 1},
		{SecretName: "db_pass", Location: "connection", TaskID: 1},
		{SecretName: "smtp", Location: "connection", TaskID: 2},
	}
	assert.Equal(t, []string{"db_pass", "smtp"}, SecretRefNames(refs))
	assert.Nil(t, SecretRefNames(nil))
}

func TestJSONStringLeaves(t *testing.T) {
	doc := `{"${secret:key}": "a ${secret:v}", "n": [1, "${secret:arr}", {"x": "y"}], "b": true}`
	got := extractSecretRefs(jsonStringLeaves(doc))
	assert.ElementsMatch(t, []string{"v", "arr"}, got)
	assert.Empty(t, jsonStringLeaves("not json"))
}
