package repository

// These test-only helpers deliberately have no integration build tag so their
// rejection paths can be tested without Docker, PostgreSQL, or Redis.
import (
	"context"
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/lib/pq"
	redis "github.com/redis/go-redis/v9"
)

type integrationHarnessConfig struct {
	runID, database, marker, dsn            string
	redisAddr, redisUsername, redisPassword string
	redisDB                                 int
}

func loadIntegrationHarnessConfig(getenv func(string) string) (integrationHarnessConfig, error) {
	c := integrationHarnessConfig{runID: getenv("SUB2API_TEST_RUN_ID")}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`).MatchString(c.runID) {
		return c, errors.New("SUB2API_TEST_RUN_ID must be 1-32 lowercase letters, digits or underscores, starting with a letter or digit")
	}
	c.database = "sub2api_it_" + c.runID
	c.marker = "sub2api-it:" + c.runID
	u, err := url.Parse(getenv("SUB2API_TEST_POSTGRES_DSN"))
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" ||
		u.User == nil || u.User.Username() != c.database || u.Path != "/"+c.database || u.Fragment != "" || u.Opaque != "" {
		return c, errors.New("SUB2API_TEST_POSTGRES_DSN must be an explicit PostgreSQL URL whose user and database both equal sub2api_it_<runid>")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return c, errors.New("invalid PostgreSQL URL query")
	}
	for key, values := range q {
		switch key {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "connect_timeout":
		default:
			return c, errors.New("PostgreSQL URL supports only TLS and connect_timeout query parameters; connection overrides are forbidden")
		}
		if len(values) != 1 {
			return c, errors.New("duplicate PostgreSQL URL option")
		}
	}
	// Override ambient PGOPTIONS/PGPORT and freeze all lib/pq options once at open.
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "5432")
	}
	q.Set("options", "-c search_path=public -c timezone=UTC")
	q.Set("search_path", "public")
	q.Set("TimeZone", "UTC")
	q.Set("connect_timeout", "5")
	u.RawQuery = q.Encode()
	c.dsn = u.String()
	c.redisAddr = getenv("SUB2API_TEST_REDIS_ADDR")
	host, port, err := net.SplitHostPort(c.redisAddr)
	p, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host == "" || p < 1 || p > 65535 {
		return c, errors.New("SUB2API_TEST_REDIS_ADDR must be an explicit host:port")
	}
	c.redisDB, err = strconv.Atoi(getenv("SUB2API_TEST_REDIS_DB"))
	if err != nil || c.redisDB <= 0 {
		return c, errors.New("SUB2API_TEST_REDIS_DB must explicitly select a dedicated nonzero Redis database")
	}
	c.redisUsername = getenv("SUB2API_TEST_REDIS_USERNAME")
	c.redisPassword = getenv("SUB2API_TEST_REDIS_PASSWORD")
	return c, nil
}

type integrationDatabaseIdentity struct {
	database, user, sessionUser, owner, marker                                 string
	superuser, createDB, createRole, replication, bypassRLS, inherit, canLogin bool
	memberOfRole, ownsOtherDB, template, sharedAccess                          bool
}

func validateIntegrationDatabaseIdentity(c integrationHarnessConfig, i integrationDatabaseIdentity) error {
	if i.database != c.database || i.user != c.database || i.sessionUser != c.database || i.owner != c.database || i.marker != c.marker {
		return errors.New("database guard refused reset: expected run-scoped database, login, owner and disposable database comment marker")
	}
	if i.superuser || i.createDB || i.createRole || i.replication || i.bypassRLS || i.inherit || !i.canLogin ||
		i.memberOfRole || i.ownsOtherDB || i.template || i.sharedAccess {
		return errors.New("database guard refused reset: role must be a dedicated restricted NOINHERIT login with no memberships, other owned databases, or shared database grants")
	}
	return nil
}

const integrationDatabaseGuardQuery = `SELECT current_database(), current_user, session_user,
	pg_catalog.pg_get_userbyid(d.datdba), COALESCE(pg_catalog.shobj_description(d.oid, 'pg_database'), ''),
	r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolreplication, r.rolbypassrls, r.rolinherit, r.rolcanlogin,
	EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member = r.oid),
	EXISTS (SELECT 1 FROM pg_catalog.pg_database other WHERE other.datdba = r.oid AND other.oid <> d.oid),
	d.datistemplate,
	EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(d.datacl, pg_catalog.acldefault('d', d.datdba))) a WHERE a.grantee <> r.oid)
FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles r ON r.rolname = current_user
WHERE d.datname = current_database()`

type integrationSQLBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func resetIntegrationDatabase(ctx context.Context, db integrationSQLBeginner, cfg integrationHarnessConfig) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var i integrationDatabaseIdentity
	err = tx.QueryRowContext(ctx, integrationDatabaseGuardQuery).Scan(
		&i.database, &i.user, &i.sessionUser, &i.owner, &i.marker,
		&i.superuser, &i.createDB, &i.createRole, &i.replication, &i.bypassRLS, &i.inherit, &i.canLogin,
		&i.memberOfRole, &i.ownsOtherDB, &i.template, &i.sharedAccess,
	)
	if err != nil {
		return fmt.Errorf("read database guard: %w", err)
	}
	if err := validateIntegrationDatabaseIdentity(cfg, i); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT nspname, pg_catalog.pg_get_userbyid(nspowner)
FROM pg_catalog.pg_namespace WHERE nspname !~ '^pg_' AND nspname <> 'information_schema' ORDER BY nspname`)
	if err != nil {
		return err
	}
	var schemas []string
	for rows.Next() {
		var name, owner string
		if err := rows.Scan(&name, &owner); err != nil {
			_ = rows.Close()
			return err
		}
		if owner != cfg.database && (name != "public" || owner != "pg_database_owner") {
			_ = rows.Close()
			return errors.New("database guard refused reset: a non-system schema is not owned by the dedicated role")
		}
		schemas = append(schemas, name)
	}
	err = rows.Err()
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("close schema rows: %w", closeErr)
	}
	if err != nil {
		return err
	}
	// Validate the entire schema list before any DROP, then reset atomically.
	for _, schema := range schemas {
		if _, err := tx.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA public AUTHORIZATION CURRENT_USER"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "REVOKE ALL ON SCHEMA public FROM PUBLIC"); err != nil {
		return err
	}
	return tx.Commit()
}

type prefixHook struct {
	prefix             string
	db                 int
	username, password string
}

func (h prefixHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h prefixHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.prefixCmd(cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}

func (h prefixHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.prefixCmd(cmd); err != nil {
				for _, c := range cmds {
					c.SetErr(err)
				}
				return err
			}
		}
		return next(ctx, cmds)
	}
}

func (h prefixHook) prefixCmd(cmd redis.Cmder) error {
	args := cmd.Args()
	refused := errors.New("integration Redis guard: command or arguments are not safely namespaced")
	if len(args) == 0 || h.db <= 0 || h.prefix == "" || strings.ContainsAny(h.prefix, "*?[]\\") {
		return refused
	}
	prefixOne := func(index int) error {
		if index >= len(args) {
			return refused
		}
		switch key := args[index].(type) {
		case string:
			if !strings.HasPrefix(key, h.prefix) {
				args[index] = h.prefix + key
			}
		case []byte:
			if !strings.HasPrefix(string(key), h.prefix) {
				args[index] = []byte(h.prefix + string(key))
			}
		default:
			return refused
		}
		return nil
	}
	name, ok := args[0].(string)
	if !ok {
		return refused
	}
	name = strings.ToLower(name)
	switch name {
	case "ping", "time", "multi", "exec", "discard":
		if len(args) != 1 {
			return refused
		}
		return nil
	case "select":
		if len(args) != 2 || fmt.Sprint(args[1]) != strconv.Itoa(h.db) {
			return refused
		}
		return nil
	case "hello":
		if len(args) < 2 || (fmt.Sprint(args[1]) != "2" && fmt.Sprint(args[1]) != "3") {
			return refused
		}
		if len(args) == 2 && h.password == "" {
			return nil
		}
		user := h.username
		if user == "" {
			user = "default"
		}
		if len(args) != 5 || !strings.EqualFold(fmt.Sprint(args[2]), "auth") || args[3] != user || args[4] != h.password {
			return refused
		}
		return nil
	case "auth":
		if h.username == "" && len(args) == 2 && args[1] == h.password {
			return nil
		}
		if len(args) == 3 && args[1] == h.username && args[2] == h.password {
			return nil
		}
		return refused
	case "get", "set", "setnx", "setex", "psetex", "incr", "decr", "incrby", "decrby", "incrbyfloat",
		"expire", "pexpire", "expireat", "pexpireat", "ttl", "pttl", "persist", "type",
		"hgetall", "hget", "hmget", "hset", "hmset", "hdel", "hincrby", "hincrbyfloat", "hexists", "hlen",
		"sadd", "srem", "smembers", "scard", "sismember", "spop",
		"zadd", "zcard", "zrange", "zrangebyscore", "zrem", "zremrangebyscore", "zrevrange", "zrevrangebyscore", "zscore",
		"lpush", "rpush", "lpop", "rpop", "llen", "lrange", "lrem", "publish":
		return prefixOne(1)
	case "mget", "exists", "del", "unlink":
		if len(args) < 2 {
			return refused
		}
		for i := 1; i < len(args); i++ {
			if err := prefixOne(i); err != nil {
				return err
			}
		}
		return nil
	case "scan":
		if len(args) < 4 || len(args)%2 != 0 {
			return refused
		}
		match := -1
		seen := map[string]bool{}
		for i := 2; i < len(args); i += 2 {
			option := strings.ToLower(fmt.Sprint(args[i]))
			if seen[option] {
				return refused
			}
			seen[option] = true
			switch option {
			case "match":
				match = i + 1
			case "count", "type":
			default:
				return refused
			}
		}
		if match < 0 {
			return refused
		}
		return prefixOne(match)
	case "eval", "evalsha", "eval_ro", "evalsha_ro":
		if len(args) < 4 {
			return refused
		}
		n, err := strconv.Atoi(fmt.Sprint(args[2]))
		if err != nil || n <= 0 || n > len(args)-3 {
			return refused
		}
		script, ok := args[1].(string)
		if !ok {
			return refused
		}
		hash := script
		if name == "eval" || name == "eval_ro" {
			hash = fmt.Sprintf("%x", sha1.Sum([]byte(script)))
		}
		argvKey, ok := integrationRedisScripts()[hash]
		if !ok {
			return refused
		}
		for i := 3; i < 3+n; i++ {
			if err := prefixOne(i); err != nil {
				return err
			}
		}
		if argvKey > 0 {
			return prefixOne(3 + n + argvKey - 1)
		}
		return nil
	default:
		return refused
	}
}

func cleanupIntegrationRedisNamespace(ctx context.Context, prefix string,
	scan func(context.Context, uint64, string, int64) ([]string, uint64, error),
	unlink func(context.Context, ...string) error,
) error {
	if prefix == "" || strings.ContainsAny(prefix, "*?[]\\") {
		return errors.New("refuse Redis cleanup without a literal namespace")
	}
	var cursor uint64
	for {
		keys, next, err := scan(ctx, cursor, prefix+"*", 500)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				return errors.New("refuse cleanup of foreign Redis key")
			}
		}
		if len(keys) > 0 {
			if err := unlink(ctx, keys...); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Only repository scripts reviewed to use KEYS (or the listed ARGV key prefix)
// may execute. Unknown scripts, SCRIPT, FUNCTION and arbitrary Lua fail closed.
// Changes to these scripts must be reviewed for new key derivation as well.
func integrationRedisScripts() map[string]int {
	scripts := []*redis.Script{
		deductBalanceScript, updateRateLimitUsageScript,
		invalidateSubscriptionScript, setSubscriptionIfGenerationScript,
		redis.NewScript(setUserPlatformQuotaCacheScript), redis.NewScript(updateUserPlatformQuotaUsageScript),
		acquireScript, getCountScript, acquireLiveLeaseScript, refreshLiveLeaseScript, trackSlotScript,
		acquireOpenAIWSIngressLeaseScript, refreshOpenAIWSIngressLeaseScript, incrementWaitScript,
		incrementAccountWaitScript, decrementWaitScript, cleanupExpiredSlotsScript, startupCleanupSlotScript,
		registerSessionScript, refreshSessionScript, getActiveSessionCountScript, isSessionActiveScript,
		acquireLockScript, releaseLockScript, reconcileLockScript, incrementRedeemAttemptScript,
		updateSchedulerLastUsedScript, captureBucketWriteTokenScript, allocateSnapshotVersionScript, releaseGroupLifecycleLeaseScript,
		claimOpenAIResponsesSessionWindowScript, compareAndRefreshOpenAIResponsesSessionWindowScript,
		compareAndDeleteOpenAIResponsesSessionWindowScript, claimLiveControllerScript, markLiveCallClosedScript, releaseLiveControllerScript,
	}
	approved := make(map[string]int, len(scripts)+3)
	for _, script := range scripts {
		approved[script.Hash()] = 0
	}
	approved[retireBucketScript.Hash()] = 2
	approved[reopenBucketScript.Hash()] = 2
	approved[activateSnapshotScript.Hash()] = 3
	return approved
}
