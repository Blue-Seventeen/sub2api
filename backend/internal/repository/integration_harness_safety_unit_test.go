package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func harnessTestEnv() map[string]string {
	return map[string]string{
		"SUB2API_TEST_RUN_ID":       "closure_123",
		"SUB2API_TEST_POSTGRES_DSN": "postgres://sub2api_it_closure_123@127.0.0.1:5432/sub2api_it_closure_123?sslmode=disable",
		"SUB2API_TEST_REDIS_ADDR":   "127.0.0.1:6379",
		"SUB2API_TEST_REDIS_DB":     "9",
	}
}

func TestIntegrationHarnessConfig(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing run", "SUB2API_TEST_RUN_ID", ""},
		{"glob run", "SUB2API_TEST_RUN_ID", "*"},
		{"long run", "SUB2API_TEST_RUN_ID", strings.Repeat("x", 40)},
		{"missing postgres", "SUB2API_TEST_POSTGRES_DSN", ""},
		{"business database", "SUB2API_TEST_POSTGRES_DSN", "postgres://sub2api_it_closure_123@localhost/sub2api"},
		{"other run", "SUB2API_TEST_POSTGRES_DSN", "postgres://sub2api_it_other@localhost/sub2api_it_other"},
		{"superuser", "SUB2API_TEST_POSTGRES_DSN", "postgres://postgres@localhost/sub2api_it_closure_123"},
		{"keyword DSN", "SUB2API_TEST_POSTGRES_DSN", "dbname=sub2api user=postgres"},
		{"override database", "SUB2API_TEST_POSTGRES_DSN", "postgres://sub2api_it_closure_123@localhost/sub2api_it_closure_123?dbname=sub2api"},
		{"override role", "SUB2API_TEST_POSTGRES_DSN", "postgres://sub2api_it_closure_123@localhost/sub2api_it_closure_123?options=-c%20role=postgres"},
		{"duplicate option", "SUB2API_TEST_POSTGRES_DSN", "postgres://sub2api_it_closure_123@localhost/sub2api_it_closure_123?sslmode=disable&sslmode=require"},
		{"missing redis", "SUB2API_TEST_REDIS_ADDR", ""},
		{"missing redis db", "SUB2API_TEST_REDIS_DB", ""},
		{"redis zero", "SUB2API_TEST_REDIS_DB", "0"},
		{"negative redis db", "SUB2API_TEST_REDIS_DB", "-1"},
		{"invalid redis db", "SUB2API_TEST_REDIS_DB", "1oops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := harnessTestEnv()
			env[tc.key] = tc.value
			_, err := loadIntegrationHarnessConfig(func(k string) string { return env[k] })
			require.Error(t, err)
		})
	}
	env := harnessTestEnv()
	cfg, err := loadIntegrationHarnessConfig(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, "sub2api_it_closure_123", cfg.database)
	require.Equal(t, "sub2api-it:closure_123", cfg.marker)
	require.Equal(t, 9, cfg.redisDB)
	env["SUB2API_TEST_POSTGRES_DSN"] = "postgres://name:do-not-log-secret@%invalid"
	_, err = loadIntegrationHarnessConfig(func(k string) string { return env[k] })
	require.Error(t, err)
	require.NotContains(t, err.Error(), "do-not-log-secret")
}

func harnessSafeIdentity() integrationDatabaseIdentity {
	return integrationDatabaseIdentity{
		database: "sub2api_it_closure_123", user: "sub2api_it_closure_123",
		sessionUser: "sub2api_it_closure_123", owner: "sub2api_it_closure_123",
		marker: "sub2api-it:closure_123", canLogin: true,
	}
}

func TestIntegrationHarnessDatabaseGuard(t *testing.T) {
	env := harnessTestEnv()
	cfg, err := loadIntegrationHarnessConfig(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NoError(t, validateIntegrationDatabaseIdentity(cfg, harnessSafeIdentity()))
	for _, tc := range []struct {
		name string
		edit func(*integrationDatabaseIdentity)
	}{
		{"business DB", func(i *integrationDatabaseIdentity) { i.database = "sub2api" }},
		{"wrong role", func(i *integrationDatabaseIdentity) { i.user = "postgres" }},
		{"set role", func(i *integrationDatabaseIdentity) { i.sessionUser = "postgres" }},
		{"wrong owner", func(i *integrationDatabaseIdentity) { i.owner = "postgres" }},
		{"no marker", func(i *integrationDatabaseIdentity) { i.marker = "" }},
		{"old marker", func(i *integrationDatabaseIdentity) { i.marker = "sub2api-it:other" }},
		{"superuser", func(i *integrationDatabaseIdentity) { i.superuser = true }},
		{"create DB", func(i *integrationDatabaseIdentity) { i.createDB = true }},
		{"create role", func(i *integrationDatabaseIdentity) { i.createRole = true }},
		{"replication", func(i *integrationDatabaseIdentity) { i.replication = true }},
		{"bypass RLS", func(i *integrationDatabaseIdentity) { i.bypassRLS = true }},
		{"inherit", func(i *integrationDatabaseIdentity) { i.inherit = true }},
		{"no login", func(i *integrationDatabaseIdentity) { i.canLogin = false }},
		{"membership", func(i *integrationDatabaseIdentity) { i.memberOfRole = true }},
		{"other owned DB", func(i *integrationDatabaseIdentity) { i.ownsOtherDB = true }},
		{"template", func(i *integrationDatabaseIdentity) { i.template = true }},
		{"shared access", func(i *integrationDatabaseIdentity) { i.sharedAccess = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := harnessSafeIdentity()
			tc.edit(&i)
			require.Error(t, validateIntegrationDatabaseIdentity(cfg, i))
		})
	}
}

func TestIntegrationHarnessResetRejectsBeforeDDL(t *testing.T) {
	env := harnessTestEnv()
	cfg, err := loadIntegrationHarnessConfig(func(k string) string { return env[k] })
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT current_database").WillReturnError(errors.New("guard unavailable"))
	mock.ExpectRollback()
	require.Error(t, resetIntegrationDatabase(context.Background(), db, cfg))
	require.NoError(t, mock.ExpectationsWereMet())
}

func harnessIdentityRows(i integrationDatabaseIdentity) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"database", "user", "session_user", "owner", "marker", "superuser", "create_db", "create_role",
		"replication", "bypass_rls", "inherit", "login", "membership", "other_database", "template", "shared_access",
	}).AddRow(i.database, i.user, i.sessionUser, i.owner, i.marker, i.superuser, i.createDB, i.createRole,
		i.replication, i.bypassRLS, i.inherit, i.canLogin, i.memberOfRole, i.ownsOtherDB, i.template, i.sharedAccess)
}

func TestIntegrationHarnessResetTransaction(t *testing.T) {
	for _, scenario := range []string{"business database", "missing marker", "foreign schema", "DDL failure", "success"} {
		t.Run(scenario, func(t *testing.T) {
			env := harnessTestEnv()
			cfg, err := loadIntegrationHarnessConfig(func(k string) string { return env[k] })
			require.NoError(t, err)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			i := harnessSafeIdentity()
			if scenario == "business database" {
				i.database = "sub2api"
			}
			if scenario == "missing marker" {
				i.marker = ""
			}
			mock.ExpectQuery("SELECT current_database").WillReturnRows(harnessIdentityRows(i))
			if scenario != "business database" && scenario != "missing marker" {
				rows := sqlmock.NewRows([]string{"nspname", "owner"}).AddRow("public", "pg_database_owner")
				if scenario == "foreign schema" {
					rows.AddRow("foreign", "business_user")
				}
				mock.ExpectQuery("SELECT nspname").WillReturnRows(rows).RowsWillBeClosed()
				if scenario == "DDL failure" {
					mock.ExpectExec(`DROP SCHEMA "public" CASCADE`).WillReturnError(errors.New("DDL refused"))
				}
				if scenario == "success" {
					mock.ExpectExec(`DROP SCHEMA "public" CASCADE`).WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec("CREATE SCHEMA public AUTHORIZATION CURRENT_USER").WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectExec("REVOKE ALL ON SCHEMA public FROM PUBLIC").WillReturnResult(sqlmock.NewResult(0, 0))
				}
			}
			if scenario == "success" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err = resetIntegrationDatabase(context.Background(), db, cfg)
			if scenario == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestIntegrationHarnessRedisRejectsBeforeDispatch(t *testing.T) {
	h := prefixHook{prefix: "sub2api_it_closure_123:nonce:1:", db: 9}
	for _, args := range [][]any{
		{"flushdb"}, {"FLUSHALL", "ASYNC"}, {"select", 0}, {"swapdb", 9, 0},
		{"config", "set", "dir", "/tmp"}, {"acl", "setuser", "default", "on"},
		{"migrate", "elsewhere", 6379, "key", 0, 1000}, {"rename", "mine", "foreign"},
		{"sort", "mine", "store", "foreign"}, {"zunionstore", "foreign", 1, "mine"},
		{"get"}, {"get", 123}, {"scan", 0}, {"scan", 0, "count", 10},
		{"scan", 0, "match", "*", "match", "*"}, {"script", "flush"},
		{"eval", "return redis.call('FLUSHDB')", 1, "mine"},
		{"evalsha", strings.Repeat("a", 40), 1, "mine"},
		{"eval", "return 1", -1}, {"unknown-command", "foreign"},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			called := false
			err := h.ProcessHook(func(context.Context, redis.Cmder) error { called = true; return nil })(context.Background(), redis.NewCmd(context.Background(), args...))
			require.Error(t, err)
			require.False(t, called, "unsafe command reached Redis transport")
		})
	}
	called := false
	cmds := []redis.Cmder{redis.NewCmd(context.Background(), "set", "mine", "value"), redis.NewCmd(context.Background(), "flushdb")}
	err := h.ProcessPipelineHook(func(context.Context, []redis.Cmder) error { called = true; return nil })(context.Background(), cmds)
	require.Error(t, err)
	require.False(t, called)
	for _, cmd := range cmds {
		require.Error(t, cmd.Err())
	}
}

func TestIntegrationHarnessRedisPrefixesAllKeys(t *testing.T) {
	const prefix = "sub2api_it_closure_123:nonce:1:"
	h := prefixHook{prefix: prefix, db: 9}
	for _, tc := range []struct{ input, want []any }{
		{[]any{"get", ""}, []any{"get", prefix}},
		{[]any{"set", []byte("key"), "value"}, []any{"set", []byte(prefix + "key"), "value"}},
		{[]any{"exists", "a", "b"}, []any{"exists", prefix + "a", prefix + "b"}},
		{[]any{"del", "a", prefix + "b"}, []any{"del", prefix + "a", prefix + "b"}},
		{[]any{"sadd", "key", "member"}, []any{"sadd", prefix + "key", "member"}},
		{[]any{"scan", 0, "match", "*", "count", 10}, []any{"scan", 0, "match", prefix + "*", "count", 10}},
		{[]any{"select", 9}, []any{"select", 9}},
		{[]any{"ping"}, []any{"ping"}},
	} {
		cmd := redis.NewCmd(context.Background(), tc.input...)
		require.NoError(t, h.prefixCmd(cmd))
		require.Equal(t, tc.want, cmd.Args())
	}
}

func TestIntegrationHarnessRedisHandshakeAndNamespaceGuard(t *testing.T) {
	h := prefixHook{prefix: "sub2api_it_closure_123:nonce:", db: 9, username: "it-user", password: "test-only"}
	for _, tc := range []struct {
		args    []any
		allowed bool
	}{
		{[]any{"hello", 2, "auth", "it-user", "test-only"}, true},
		{[]any{"auth", "it-user", "test-only"}, true},
		{[]any{"select", 9}, true},
		{[]any{"hello", 2, "auth", "default", "test-only"}, false},
		{[]any{"auth", "default", "test-only"}, false},
		{[]any{"hello", 2}, false},
		{[]any{"select", 0}, false},
		{[]any{"client", "kill", "type", "normal"}, false},
	} {
		err := h.prefixCmd(redis.NewCmd(context.Background(), tc.args...))
		if tc.allowed {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	for _, prefix := range []string{"", "*", "it:[]:", "it:?", "it:\\"} {
		h.prefix = prefix
		require.Error(t, h.prefixCmd(redis.NewCmd(context.Background(), "get", "key")))
	}
}

func TestIntegrationHarnessRedisApprovedScripts(t *testing.T) {
	const prefix = "sub2api_it_closure_123:nonce:1:"
	h := prefixHook{prefix: prefix, db: 9}
	cmd := redis.NewCmd(context.Background(), "evalsha", deductBalanceScript.Hash(), 1, "balance", 10, 60)
	require.NoError(t, h.prefixCmd(cmd))
	require.Equal(t, prefix+"balance", cmd.Args()[3])
	cmd = redis.NewCmd(context.Background(), "eval", setUserPlatformQuotaCacheScript, 2, "cache", "fence", 60, "field", "value")
	require.NoError(t, h.prefixCmd(cmd))
	require.Equal(t, prefix+"cache", cmd.Args()[3])
	require.Equal(t, prefix+"fence", cmd.Args()[4])
	cmd = redis.NewCmd(context.Background(), "evalsha", retireBucketScript.Hash(), 5, "epoch", "retired", "buckets", "fresh", "active", "bucket", "snapshot:", 60)
	require.NoError(t, h.prefixCmd(cmd))
	require.Equal(t, prefix+"snapshot:", cmd.Args()[9], "script-derived key prefix must be scoped too")
	cmd = redis.NewCmd(context.Background(), "evalsha", deductBalanceScript.Hash(), 4, "only-one-key")
	require.Error(t, h.prefixCmd(cmd))
}

// This terminal hook replaces only the Redis transport. The real Script.Run,
// EVAL command builders and prefix hooks still execute, without a live service.
type harnessScriptTransport struct {
	commands    [][]any
	missingHash bool
}

func (h *harnessScriptTransport) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *harnessScriptTransport) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.commands = append(h.commands, append([]any(nil), cmd.Args()...))
		if h.missingHash && cmd.Name() == "evalsha" {
			cmd.SetErr(redis.ErrNoScript)
			return redis.ErrNoScript
		}
		cmd.(*redis.Cmd).SetVal(int64(1)) //nolint:errcheck // SetVal mutates the test command and has no return value.
		return nil
	}
}

func (h *harnessScriptTransport) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.ProcessHook(nil)(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestIntegrationHarnessRedisSubscriptionGenerationScripts(t *testing.T) {
	const prefix = "sub2api_it_closure_123:nonce:1:"
	for _, tc := range []struct {
		name   string
		script *redis.Script
		argv   []any
	}{
		{name: "invalidate", script: invalidateSubscriptionScript},
		{
			name: "set if generation", script: setSubscriptionIfGenerationScript,
			argv: []any{7, 60, "active", "1800000000", "1.5", "2.5", "3.5", "4.5", "2",
				"10", "20", "30", "40", "24", "8.5", "1700000000", "1700000000", "1700000000", "1700000000", "1700000060"},
		},
	} {
		for _, mode := range []string{"evalsha", "noscript fallback", "eval pipeline"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				rdb := redis.NewClient(&redis.Options{Addr: "unused.invalid:6379", DB: 9})
				t.Cleanup(func() { _ = rdb.Close() })
				transport := &harnessScriptTransport{missingHash: mode == "noscript fallback"}
				rdb.AddHook(prefixHook{prefix: prefix, db: 9})
				rdb.AddHook(transport)
				keys := []string{"billing:sub:42:7", "billing:sub_generation:42:7"}
				if mode == "eval pipeline" {
					pipe := rdb.Pipeline()
					tc.script.Eval(ctx, pipe, keys, tc.argv...)
					_, err := pipe.Exec(ctx)
					require.NoError(t, err)
				} else {
					require.NoError(t, tc.script.Run(ctx, rdb, keys, tc.argv...).Err())
				}
				wantCommands := []string{"evalsha"}
				if mode == "noscript fallback" {
					wantCommands = []string{"evalsha", "eval"}
				}
				if mode == "eval pipeline" {
					wantCommands = []string{"eval"}
				}
				require.Len(t, transport.commands, len(wantCommands))
				for i, args := range transport.commands {
					require.Equal(t, wantCommands[i], args[0])
					require.Equal(t, 2, args[2])
					require.Equal(t, prefix+"billing:sub:42:7", args[3])
					require.Equal(t, prefix+"billing:sub_generation:42:7", args[4])
					require.Equal(t, len(tc.argv)+5, len(args))
					for j, value := range tc.argv {
						require.Equal(t, value, args[j+5], "ARGV values must not be treated as keys")
					}
				}
				require.Equal(t, []string{"billing:sub:42:7", "billing:sub_generation:42:7"}, keys)
			})
		}
	}
}

func TestIntegrationHarnessRedisCleanupRejectsForeignKeys(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			const prefix = "sub2api_it_closure_123:nonce:1:"
			var deleted []string
			calls := 0
			err := cleanupIntegrationRedisNamespace(context.Background(), prefix,
				func(_ context.Context, cursor uint64, pattern string, count int64) ([]string, uint64, error) {
					require.Equal(t, prefix+"*", pattern)
					calls++
					if foreign {
						return []string{prefix + "mine", "business:key"}, 0, nil
					}
					if cursor == 0 {
						return []string{prefix + "a"}, 7, nil
					}
					return []string{prefix + "b"}, 0, nil
				},
				func(_ context.Context, keys ...string) error { deleted = append(deleted, keys...); return nil },
			)
			if foreign {
				require.Error(t, err)
				require.Empty(t, deleted, "do not dispatch a batch containing foreign keys")
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{prefix + "a", prefix + "b"}, deleted)
				require.Equal(t, 2, calls)
			}
		})
	}
}

func TestIntegrationHarnessRedisCleanupFailsClosed(t *testing.T) {
	for _, prefix := range []string{"", "*", "it:[]:", "it:?", "it:\\"} {
		err := cleanupIntegrationRedisNamespace(context.Background(), prefix,
			func(context.Context, uint64, string, int64) ([]string, uint64, error) {
				t.Fatal("unsafe prefix reached SCAN")
				return nil, 0, nil
			},
			func(context.Context, ...string) error { t.Fatal("unsafe prefix reached UNLINK"); return nil },
		)
		require.Error(t, err)
	}
	want := errors.New("scan failed")
	err := cleanupIntegrationRedisNamespace(context.Background(), "sub2api_it_closure_123:nonce:",
		func(context.Context, uint64, string, int64) ([]string, uint64, error) { return nil, 0, want },
		func(context.Context, ...string) error { t.Fatal("scan failure reached UNLINK"); return nil },
	)
	require.ErrorIs(t, err, want)
}
