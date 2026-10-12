# pgtt — pg_timetable management CLI & TUI

`pgtt` is a standalone command-line and terminal-UI management tool for
[pg_timetable](https://github.com/cybertec-postgresql/pg_timetable).
It connects directly to PostgreSQL and treats the `timetable.*` schema as the
single source of truth. It never creates or upgrades the schema — that remains
the responsibility of a pg_timetable scheduler instance.

## Build

```bash
make build            # → ./pgtt
make release          # embeds version/commit/date via ldflags
```

## Quick start

```bash
pgtt check "postgresql://user:pass@host/db"   # verify connectivity + schema version
pgtt chain list                               # PGTT_CONNSTR or libpq env vars also work
pgtt                                          # bare invocation launches the TUI
```

See [docs/pgtt.md](docs/pgtt.md) for the full command reference and
[spec/](spec/) for the design documents.

## Compatibility

Each `pgtt` release is built against a specific pg_timetable database schema
version (shown by `pgtt version` as *DB Schema*). `pgtt check` refuses to
operate against an incompatible schema.

## Testing

Integration tests use [testcontainers](https://testcontainers.com/) and require
Docker. The `timetable.*` schema is initialized in the test database by running
the official `cybertecpostgresql/pg_timetable` image once with `--init`. That
image is a release, so it stops short of the schema version pgtt pins to.
`internal/testutils` applies the remaining migrations (`00820.sql` and
`00850.sql`, copied verbatim from pg_timetable) on top. Delete each file once a
release ships it.

```bash
make test
```

## License

PostgreSQL License — see [LICENSE](LICENSE).
