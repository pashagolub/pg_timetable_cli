// Package testutils spins up throwaway PostgreSQL containers for integration
// tests and seeds them with the timetable.* schema.
//
// Unlike the pg_timetable repository (where the in-tree engine creates the
// schema), this standalone repository deliberately has no scheduler code
// (REQ-016: pgtt never creates the schema). Instead the official
// pg_timetable Docker image is run once with --init, which initializes the
// schema and exits. This guarantees the test schema always matches a real
// scheduler deployment.
//
// The released image is one migration behind pgtt's pinned dbSchema, so
// migrations/00820.sql is applied on top; see applyPendingMigrations.
package testutils

import (
	"context"
	_ "embed"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SchedulerImage is the pg_timetable image used to initialize the schema.
var SchedulerImage = "cybertecpostgresql/pg_timetable:latest"

// migration00820 is pg_timetable's migrations/00820.sql, copied verbatim.
//
// pgtt pins dbSchema to 00820 (the Secret Store), which is on pg_timetable
// master but not in any release, so the :latest image initializes the database
// to 00797. Applying the real migration here — rather than a hand-written
// subset — keeps the test database identical to what a master scheduler
// produces. Drop this file once a release ships 00820 and the image catches up.
//
//go:embed 00820.sql
var migration00820 string

// PostgresTestContainer wraps a postgres container whose database has the
// timetable.* schema installed.
type PostgresTestContainer struct {
	Container *postgres.PostgresContainer
	// Pool is an open pgx pool for seeding/asserting test data.
	Pool *pgxpool.Pool
	// ConnStr is the connection string reachable from the host.
	ConnStr string
}

// SetupPostgresContainer creates a new PostgreSQL container with the
// timetable schema for testing and returns it together with a cleanup func.
func SetupPostgresContainer(t *testing.T) (*PostgresTestContainer, func()) {
	t.Helper()

	ctx := context.Background()

	postgresContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("timetable"),
		postgres.WithUsername("scheduler"),
		postgres.WithPassword("somestrong"),
		testcontainers.WithWaitStrategyAndDeadline(
			60*time.Second,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}
	cleanup := func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to get connection string: %v", err)
	}

	if err := initSchema(ctx, postgresContainer); err != nil {
		cleanup()
		t.Fatalf("Failed to initialize timetable schema: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to open pgx pool: %v", err)
	}

	if err := applyPendingMigrations(ctx, pool); err != nil {
		pool.Close()
		cleanup()
		t.Fatalf("Failed to apply pending migrations: %v", err)
	}

	tc := &PostgresTestContainer{
		Container: postgresContainer,
		Pool:      pool,
		ConnStr:   connStr,
	}
	return tc, func() {
		pool.Close()
		cleanup()
	}
}

// initSchema runs the official pg_timetable image once with --init against
// the given postgres container. The scheduler creates the timetable.* schema
// and exits (exit code 0).
func initSchema(ctx context.Context, pg *postgres.PostgresContainer) error {
	// Reach postgres from a sibling container via the docker bridge IP and
	// the container-internal port.
	ip, err := pg.ContainerIP(ctx)
	if err != nil {
		return fmt.Errorf("resolving postgres container IP: %w", err)
	}
	dsn := fmt.Sprintf("postgresql://scheduler:somestrong@%s:5432/timetable?sslmode=disable", ip)

	req := testcontainers.ContainerRequest{
		Image: SchedulerImage,
		Cmd:   []string{"--clientname=schema_init", "--connstr=" + dsn, "--init"},
		WaitingFor: wait.ForExit().
			WithExitTimeout(90 * time.Second),
	}
	initContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("running %s --init: %w", SchedulerImage, err)
	}
	defer func() { _ = initContainer.Terminate(ctx) }()

	state, err := initContainer.State(ctx)
	if err != nil {
		return fmt.Errorf("inspecting init container: %w", err)
	}
	if state.ExitCode != 0 {
		return fmt.Errorf("%s --init exited with code %d", SchedulerImage, state.ExitCode)
	}
	return nil
}

// applyPendingMigrations brings the database up to the schema version pgtt is
// pinned to, for migrations the released scheduler image does not yet ship.
// It is a no-op once the image itself initializes to that version.
func applyPendingMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	var have bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM timetable.migration WHERE version LIKE '00820%')`,
	).Scan(&have); err != nil {
		return fmt.Errorf("reading timetable.migration: %w", err)
	}
	if have {
		return nil
	}
	if _, err := pool.Exec(ctx, migration00820); err != nil {
		return fmt.Errorf("applying migration 00820: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO timetable.migration (id, version)
		 VALUES ((SELECT MAX(id) + 1 FROM timetable.migration), '00820 Add timetable.secret store')`,
	); err != nil {
		return fmt.Errorf("recording migration 00820: %w", err)
	}
	return nil
}

// AddSecret inserts one timetable.secret row. The value is opaque bytes: pgtt
// never decrypts, so the tests need no pgcrypto and no real ciphertext.
func (tc *PostgresTestContainer) AddSecret(t *testing.T, clientName, secretName string, valueEnc []byte) {
	t.Helper()
	if _, err := tc.Pool.Exec(context.Background(),
		`INSERT INTO timetable.secret (client_name, secret_name, value_enc) VALUES ($1, $2, $3)`,
		clientName, secretName, valueEnc,
	); err != nil {
		t.Fatalf("Failed to insert secret %s/%s: %v", clientName, secretName, err)
	}
}
