// Package testutils spins up throwaway PostgreSQL containers for integration
// tests and seeds them with the timetable.* schema.
//
// Unlike the pg_timetable repository (where the in-tree engine creates the
// schema), this standalone repository deliberately has no scheduler code
// (REQ-016: pgtt never creates the schema). Instead the official
// pg_timetable Docker image is run once with --init, which initializes the
// schema to the latest version and exits. This guarantees the test schema
// always matches a real scheduler deployment.
package testutils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SchedulerImage is the pg_timetable image used to initialize the schema.
// Override via ldflags or keep in lockstep with the dbSchema constant in
// cmd/root.go when pinning to a released schema version.
var SchedulerImage = "cybertecpostgresql/pg_timetable:latest"

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
