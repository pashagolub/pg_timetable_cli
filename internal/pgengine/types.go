// Package pgengine contains the timetable.* schema data structures and YAML
// chain-definition parsing shared with the pg_timetable scheduler.
//
// These types are vendored from github.com/cybertec-postgresql/pg_timetable
// (internal/pgengine) so this repository has no compile-time dependency on the
// scheduler. Keep field names and db/yaml tags in sync with the upstream
// schema version declared in cmd/root.go (dbSchema).
package pgengine

import (
	"strconv"
	"strings"
	"time"
)

// logIdent combines a numeric ID and a human-readable name for log fields,
// e.g. "42|Import Chain From S3". If the name is empty only the ID is returned.
func logIdent(id int, name string) string {
	if name == "" {
		return strconv.Itoa(id)
	}
	return strconv.Itoa(id) + "|" + name
}

// Chain structure used to represent tasks chains
type Chain struct {
	ChainID            int    `db:"chain_id" yaml:"-"`
	ChainName          string `db:"chain_name" yaml:"name"`
	SelfDestruct       bool   `db:"self_destruct" yaml:"self_destruct,omitempty"`
	ExclusiveExecution bool   `db:"exclusive_execution" yaml:"exclusive,omitempty"`
	MaxInstances       int    `db:"max_instances" yaml:"max_instances,omitempty"`
	Timeout            int    `db:"timeout" yaml:"timeout,omitempty"`
	OnError            string `db:"on_error" yaml:"on_error,omitempty"`
}

// String returns a log-friendly identifier, e.g. "42|Import Chain From S3".
func (chain Chain) String() string {
	return logIdent(chain.ChainID, chain.ChainName)
}

// ChainTask structure describes each chain task
type ChainTask struct {
	ChainID       int       `db:"-" yaml:"-"`
	TaskID        int       `db:"task_id" yaml:"-"`
	TaskName      string    `db:"task_name" yaml:"-"`
	Command       string    `db:"command" yaml:"command"`
	Kind          string    `db:"kind" yaml:"kind,omitempty"`
	RunAs         string    `db:"run_as" yaml:"run_as,omitempty"`
	IgnoreError   bool      `db:"ignore_error" yaml:"ignore_error,omitempty"`
	Autonomous    bool      `db:"autonomous" yaml:"autonomous,omitempty"`
	ConnectString string    `db:"database_connection" yaml:"connect_string,omitempty"`
	Timeout       int       `db:"timeout" yaml:"timeout,omitempty"` // in milliseconds
	StartedAt     time.Time `db:"-" yaml:"-"`
	Vxid          int64     `db:"-" yaml:"-"`
}

// IsRemote returns true if the task must run against a remote database.
func (task *ChainTask) IsRemote() bool {
	return strings.TrimSpace(task.ConnectString) != ""
}

// String returns a log-friendly identifier, e.g. "49|Check_if_file_exist".
func (task ChainTask) String() string {
	return logIdent(task.TaskID, task.TaskName)
}
