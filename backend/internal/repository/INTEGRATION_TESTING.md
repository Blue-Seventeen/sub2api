# Repository integration harness

The harness only connects to explicitly supplied, existing services. It does
not invoke Docker or create/delete containers, databases, or roles. Missing or
unsafe configuration fails with exit code 1 locally and in CI; there is no
automatic provisioning or successful skip. Even `-run '^$'` with the integration
tag runs setup. Use `go test -c -tags integration` for compilation only.

## Parent-owned PostgreSQL setup

Provision a **fresh disposable logical database and fresh restricted login**
on the parent-approved PostgreSQL endpoint. Do not reuse an application role or
mark a business database as disposable. This example uses run ID `closure_123`;
choose a unique ID for each independently running suite (1-32 lowercase letters,
digits or underscores, starting with a letter or digit).

Run these commands yourself as the provisioning administrator, using your
existing authenticated `psql` connection. No password is included here:

```sql
CREATE ROLE sub2api_it_closure_123 LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
  NOREPLICATION NOBYPASSRLS NOINHERIT;
\password sub2api_it_closure_123
CREATE DATABASE sub2api_it_closure_123 OWNER sub2api_it_closure_123 TEMPLATE template0;
REVOKE ALL ON DATABASE sub2api_it_closure_123 FROM PUBLIC;
COMMENT ON DATABASE sub2api_it_closure_123 IS 'sub2api-it:closure_123';
\connect sub2api_it_closure_123
ALTER SCHEMA public OWNER TO sub2api_it_closure_123;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
```

The harness checks the actual database name, session/current user, owner, exact
database comment, role flags, absence of role memberships and other owned
databases, non-template status, and absence of grants to other roles/PUBLIC.
All non-system schemas must belong to this role (`public` may also belong to
`pg_database_owner`). It then resets those schemas in one transaction and applies
migrations. A session advisory lock prevents concurrent suites using the same
database. The database and marker remain after the run for parent-managed reuse
or teardown. Every subsequent run resets its data again.

## Parent-owned Redis selection

Reserve an existing **nonzero logical Redis DB** for integration tests, for
example DB 9, on the parent-approved Redis endpoint. DB 0 is rejected. No Redis
server or logical database is provisioned by the harness. The selected DB must
already be supported by the server's `databases` setting; Redis Cluster is not
supported because it only provides DB 0.

Keys use `sub2api_it_<runid>:<random-128-bit-process-id>:<test-sequence>:`. The
process prefix is logged without credentials. Cleanup scans/unlinks only that
test's literal prefix, rejects foreign results, and never uses FLUSHDB/FLUSHALL.
Foreign keys in the selected DB are left alone. A killed process can leave
namespaced keys for the parent to inspect and clean up; never flush the DB to
remove them.

The test-client hook rejects unknown commands, DB switches, global/admin
commands, unscoped SCAN, unknown Lua and script-management commands before
dispatch (including pipelines). Approved repository Lua scripts have their
KEYS and known key-prefix ARGV parameters scoped. Adding/changing an approved
script requires reviewing its key derivation. Do not bypass `testRedis` with a
new raw client or use PubSub subscriptions, which are outside go-redis command
hooks. This is an accidental-destruction guard for trusted test code, not a
sandbox for malicious code. A dedicated Redis ACL user restricted to
`~sub2api_it_<runid>:*`, with global/admin commands denied, is recommended as
additional server-side containment.

## Environment and execution

PowerShell example from `backend`, with the administrator's endpoints replaced
by the approved addresses. Supply PostgreSQL authentication using a password
file (`PGPASSFILE`) or an existing secret environment, and Redis credentials
using the optional test-specific secret variables. Do not print passwords:

```powershell
$env:SUB2API_TEST_RUN_ID = 'closure_123'
$env:SUB2API_TEST_POSTGRES_DSN = 'postgres://sub2api_it_closure_123@127.0.0.1:5432/sub2api_it_closure_123?sslmode=disable'
$env:SUB2API_TEST_REDIS_ADDR = '127.0.0.1:6379'
$env:SUB2API_TEST_REDIS_DB = '9'
# Optional: SUB2API_TEST_REDIS_USERNAME, SUB2API_TEST_REDIS_PASSWORD
# Use TLS URL options for PostgreSQL on endpoints that require them.
go test -tags integration ./internal/repository -count=1 -timeout 20m
```

Only PostgreSQL URL DSNs are accepted, with explicit host, user and database.
Allowed query options are `sslmode`, `sslrootcert`, `sslcert`, `sslkey`, and
`connect_timeout`. Duplicate options and connection/role/database overrides
are rejected. The harness forces UTC, `search_path=public`, and a five-second
connection timeout. Never pass the provisioning administrator's credentials to
the test process. `SUB2API_TEST_POSTGRES_IMAGE` and Ryuk settings are no longer
used: PostgreSQL version selection belongs to the parent's service setup.

CI uses the same contract: its provisioning step prepares and marks the logical
database with an administrator connection, then passes **restricted** login
credentials and explicit Redis settings to this test command. This harness does
not provision CI services or change existing CI workflows.

When reusing an existing PostgreSQL container with no published ports, the parent
can compile a static Linux test binary on the host and copy it into a unique
directory under `/tmp` in that existing container. Match `GOARCH` to that
container; this example targets amd64:

```powershell
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go test -c -tags integration ./internal/repository -o "$env:TEMP/repo.test"
```

Inside the existing container use PostgreSQL host `127.0.0.1:5432` and the
existing Redis service's reachable hostname/IP, not Redis `127.0.0.1` unless
Redis really shares that network namespace. Pass the same test environment
variables and restricted credentials. Run the copied binary as:

```sh
/tmp/<parent-unique-directory>/repo.test -test.run '<approved-test-regexp>' -test.count=1 -test.timeout=20m -test.v
```

Neither binary compilation nor this harness creates a container. Parent-owned
provisioning, copying, execution and teardown remain separate operations.

Safety tests require no integration tag or service endpoints:

```powershell
go test ./internal/repository -run '^TestIntegrationHarness' -count=1
go test -c -tags integration ./internal/repository -o "$env:TEMP/sub2api-repository-integration.test.exe"
```
