//go:build integration

package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lib/pq"
	redisclient "github.com/redis/go-redis/v9"
)

var (
	integrationDB          *sql.DB
	integrationEntClient   *dbent.Client
	integrationRedis       *redisclient.Client
	integrationRedisPrefix string

	redisNamespaceSeq uint64
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	cfg, err := loadIntegrationHarnessConfig(os.Getenv)
	if err != nil {
		log.Printf("integration harness configuration refused: %v; see INTEGRATION_TESTING.md", err)
		return 1
	}
	if err := timezone.Init("UTC"); err != nil {
		log.Printf("failed to init timezone: %v", err)
		return 1
	}
	return runIntegrationTestsWithExternalServices(m.Run, cfg)
}

func runIntegrationTestsWithExternalServices(run func() int, cfg integrationHarnessConfig) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		log.Printf("failed to generate integration Redis namespace")
		return 1
	}
	integrationRedisPrefix = fmt.Sprintf("%s:%x:", cfg.database, nonce)
	log.Printf("integration resources: database=%s redis_db=%d redis_prefix=%s", cfg.database, cfg.redisDB, integrationRedisPrefix)
	integrationRedis = redisclient.NewClient(&redisclient.Options{
		Addr: cfg.redisAddr, DB: cfg.redisDB, Username: cfg.redisUsername, Password: cfg.redisPassword,
		Protocol: 2, DisableIdentity: true,
		DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	})
	defer integrationRedis.Close()
	integrationRedis.AddHook(prefixHook{prefix: integrationRedisPrefix, db: cfg.redisDB, username: cfg.redisUsername, password: cfg.redisPassword})
	if err := integrationRedis.Ping(ctx).Err(); err != nil {
		log.Printf("failed to ping integration Redis; check explicit address, credentials and nonzero DB")
		return 1
	}
	var err error
	integrationDB, err = openSQLWithRetry(ctx, cfg.dsn, 30*time.Second)
	if err != nil {
		log.Printf("failed to open integration PostgreSQL; check explicit endpoint and restricted login")
		return 1
	}
	defer integrationDB.Close()
	// Register this before the pinned lock connection so it closes after the
	// lock is released, but before the shared SQL pool is closed.
	defer func() {
		if integrationEntClient != nil {
			_ = integrationEntClient.Close()
		}
	}()
	// Keep the advisory lock on one session for the entire suite, not just reset.
	lockConn, err := integrationDB.Conn(ctx)
	if err != nil {
		log.Printf("failed to acquire integration PostgreSQL connection")
		return 1
	}
	defer lockConn.Close()
	const suiteLockID int64 = 724082491865101
	var locked bool
	if err := lockConn.QueryRowContext(ctx, "SELECT pg_catalog.pg_try_advisory_lock($1)", suiteLockID).Scan(&locked); err != nil || !locked {
		log.Printf("integration database is already in use or the suite lock is unavailable")
		return 1
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		_, _ = lockConn.ExecContext(unlockCtx, "SELECT pg_catalog.pg_advisory_unlock($1)", suiteLockID)
	}()
	if err := resetIntegrationDatabase(ctx, lockConn, cfg); err != nil {
		log.Printf("failed to reset integration database: %v", err)
		return 1
	}
	if err := ApplyMigrations(ctx, integrationDB); err != nil {
		log.Printf("failed to apply db migrations: %v", err)
		return 1
	}

	drv := entsql.OpenDB(dialect.Postgres, integrationDB)
	integrationEntClient = dbent.NewClient(dbent.Driver(drv))
	return run()
}

func openSQLWithRetry(ctx context.Context, dsn string, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, errors.New("invalid integration PostgreSQL connection options")
	}

	for time.Now().Before(deadline) && ctx.Err() == nil {
		db := sql.OpenDB(connector)

		if err := pingWithTimeout(ctx, db, 2*time.Second); err != nil {
			lastErr = err
			_ = db.Close()
			time.Sleep(250 * time.Millisecond)
			continue
		}

		return db, nil
	}

	return nil, fmt.Errorf("db not ready after %s: %w", timeout, lastErr)
}

func pingWithTimeout(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

func testTx(t *testing.T) *sql.Tx {
	t.Helper()

	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err, "begin tx")
	t.Cleanup(func() {
		_ = tx.Rollback()
	})
	return tx
}

// testEntClient 返回全局的 ent client，用于测试需要内部管理事务的代码（如 Create/Update 方法）。
// 注意：此 client 的操作会真正写入数据库，测试结束后不会自动回滚。
func testEntClient(t *testing.T) *dbent.Client {
	t.Helper()
	return integrationEntClient
}

// testEntTx 返回一个 ent 事务，用于需要事务隔离的测试。
// 测试结束后会自动回滚，不会影响数据库状态。
func testEntTx(t *testing.T) *dbent.Tx {
	t.Helper()

	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err, "begin ent tx")
	t.Cleanup(func() {
		_ = tx.Rollback()
	})
	return tx
}

// testEntSQLTx 已弃用：不要在新测试中使用此函数。
// 基于 *sql.Tx 创建的 ent client 在调用 client.Tx() 时会 panic。
// 对于需要测试内部使用事务的代码，请使用 testEntClient。
// 对于需要事务隔离的测试，请使用 testEntTx。
//
// Deprecated: Use testEntClient or testEntTx instead.
func testEntSQLTx(t *testing.T) (*dbent.Client, *sql.Tx) {
	t.Helper()

	// 直接失败，避免旧测试误用导致的事务嵌套 panic。
	t.Fatalf("testEntSQLTx 已弃用：请使用 testEntClient 或 testEntTx")
	return nil, nil
}

func testRedis(t *testing.T) *redisclient.Client {
	t.Helper()

	prefix := fmt.Sprintf(
		"%s%d:",
		integrationRedisPrefix,
		atomic.AddUint64(&redisNamespaceSeq, 1),
	)

	opts := *integrationRedis.Options()
	rdb := redisclient.NewClient(&opts)
	rdb.AddHook(prefixHook{prefix: prefix, db: opts.DB, username: opts.Username, password: opts.Password})

	t.Cleanup(func() {
		defer rdb.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := cleanupIntegrationRedisNamespace(ctx, prefix,
			func(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
				return rdb.Scan(ctx, cursor, match, count).Result()
			},
			func(ctx context.Context, keys ...string) error { return rdb.Unlink(ctx, keys...).Err() },
		)
		require.NoError(t, err, "clean up only this test's Redis namespace")
	})

	return rdb
}

func assertTTLWithin(t *testing.T, ttl time.Duration, min, max time.Duration) {
	t.Helper()
	require.GreaterOrEqual(t, ttl, min, "ttl should be >= min")
	require.LessOrEqual(t, ttl, max, "ttl should be <= max")
}

// IntegrationRedisSuite provides a base suite for Redis integration tests.
// Embedding suites should call SetupTest to initialize ctx and rdb.
type IntegrationRedisSuite struct {
	suite.Suite
	ctx context.Context
	rdb *redisclient.Client
}

// SetupTest initializes ctx and rdb for each test method.
func (s *IntegrationRedisSuite) SetupTest() {
	s.ctx = context.Background()
	s.rdb = testRedis(s.T())
}

// RequireNoError is a convenience method wrapping require.NoError with s.T().
func (s *IntegrationRedisSuite) RequireNoError(err error, msgAndArgs ...any) {
	s.T().Helper()
	require.NoError(s.T(), err, msgAndArgs...)
}

// AssertTTLWithin asserts that ttl is within [min, max].
func (s *IntegrationRedisSuite) AssertTTLWithin(ttl, min, max time.Duration) {
	s.T().Helper()
	assertTTLWithin(s.T(), ttl, min, max)
}

// IntegrationDBSuite provides a base suite for DB integration tests.
// Embedding suites should call SetupTest to initialize ctx and client.
type IntegrationDBSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	tx     *dbent.Tx
}

// SetupTest initializes ctx and client for each test method.
func (s *IntegrationDBSuite) SetupTest() {
	s.ctx = context.Background()
	// 统一使用 ent.Tx，确保每个测试都有独立事务并自动回滚。
	tx := testEntTx(s.T())
	s.tx = tx
	s.client = tx.Client()
}

// RequireNoError is a convenience method wrapping require.NoError with s.T().
func (s *IntegrationDBSuite) RequireNoError(err error, msgAndArgs ...any) {
	s.T().Helper()
	require.NoError(s.T(), err, msgAndArgs...)
}
