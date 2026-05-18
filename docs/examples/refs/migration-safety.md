<migration-safety>
Schema / contract migrations are the highest-risk class of changes — irreversible, affect concurrent writers, can leave the system in a wedged state. Defaults: **online-safe, reversible, staged**.

**Classification (declare upfront):**

- **Trivial** — add nullable column, add index CONCURRENTLY, add new method/endpoint. No coordination needed.
- **Online-safe with care** — backfill column, drop unused column (after deploy that stops reading it), rename via shim. Multi-step deploy.
- **Requires downtime / coordination** — change column type, drop column still in use, change endpoint signature without versioning, change distributed lock semantics.
- **Forbidden without explicit user sign-off** — destructive without backup, drop table with data, change a primary key in place.

**Rules:**
1. Adding columns: must be nullable OR have a server-side default. Adding NOT NULL without default to a non-empty table requires backfill first.
2. Dropping columns/fields: only after a deploy that stopped reading them. Verify via `grep` that no code references survive.
3. Renames: shim period — both old and new names work until all callers migrated. No flag-day renames.
4. Indexes: `CREATE INDEX CONCURRENTLY` on Postgres. Plain `CREATE INDEX` locks the table.
5. Backfill: chunked, with progress logging, idempotent. Single `UPDATE table SET x = y` on a large table is a guaranteed long lock.
6. Migrations are versioned, monotonic, and individually revertible. `up` and `down` SQL both present.
7. Test the migration on a dataset of representative size before claiming "online-safe". Locking semantics change with row count.

**Multi-service contract changes:**
- Add the new field/endpoint first, deployed everywhere, before any caller uses it.
- Remove old field/endpoint last, after every caller stopped using it.
- Use explicit versioning when a breaking change is unavoidable (`/v2/...`).

**API breakage rules (this project):**
- JSON-RPC method renames / param changes → bump `ProtocolVersion` AND keep the old name as alias for one minor version with deprecation log.
- Ops format changes → bump `OpsVersion`. Old ops MUST still apply.
- Tool name/parameter changes → bump `ToolsVersion`.
</migration-safety>
