SQL migrations, embedded into the server binary (`migrations.go`) and applied
on every boot by `db.Migrate` (`server/internal/db/migrate.go`).

- Name files `NNNN_short_name.sql`; they run in name order, each once, each in
  its own transaction. `schema_migrations` records the applied ones, so
  booting again is a no-op.
- Never edit a file that is merged: add a new one.
- The schema is the contract in `docs/contract.md`; after the freeze a schema
  change needs the Tech lead's OK.
