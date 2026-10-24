//go:build unit && integration

package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
)

// This adapter keeps the existing unit fixture's persistence methods, but uses
// PostgreSQL row locks and actual timestamp roundtrips rather than rounding in Go.
type postgresRefundPrecisionSubscriptionRepo struct {
	*pendingRefundSubscriptionRepo
}

func (r *postgresRefundPrecisionSubscriptionRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	row, err := r.db(ctx).UserSubscription.Query().Where(usersubscription.IDEQ(id)).ForUpdate().Only(ctx)
	if err != nil {
		return nil, err
	}
	return pendingRefundSubscription(row), nil
}

func (r *postgresRefundPrecisionSubscriptionRepo) ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]UserSubscription, error) {
	query := r.db(ctx).UserSubscription.Query().Where(
		usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID),
		usersubscription.StatusEQ(SubscriptionStatusActive), usersubscription.ExpiresAtGT(time.Now()),
	).Order(usersubscription.ByStartsAt(), usersubscription.ByID())
	if dbent.TxFromContext(ctx) != nil {
		query = query.ForUpdate()
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]UserSubscription, 0, len(rows))
	for _, row := range rows {
		result = append(result, *pendingRefundSubscription(row))
	}
	return result, nil
}

func TestPendingSubscriptionRefundPostgresPrecisionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, db := newRefundPrecisionPostgresClient(t, ctx)
	now := time.Date(2030, 3, 1, 12, 0, 0, 123456789, time.UTC)
	var storedNow time.Time
	require.NoError(t, db.QueryRowContext(ctx, "SELECT $1::timestamptz", now).Scan(&storedNow))
	require.NotEqual(t, now.Nanosecond(), storedNow.Nanosecond(), "fixture must exercise PostgreSQL precision loss")
	require.Zero(t, storedNow.Nanosecond()%1000)

	order := createCompletedRefundableOrderForTest(t, ctx, client, "postgres-precision-refund")
	group, err := client.Group.Create().SetName("postgres-precision-refund-group").
		SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).SetSubscriptionDays(3).Save(ctx)
	require.NoError(t, err)
	before := make([]*dbent.UserSubscription, 0, 2)
	for i := 0; i < 2; i++ {
		card, createErr := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
			SetStartsAt(time.Date(2030, 3, 1, 11, 0, 0, 0, time.UTC)).
			SetExpiresAt(time.Date(2030, 3, 4-i, 12, 0, 0, 0, time.UTC)).
			SetStatus(SubscriptionStatusActive).SetNotes("original PostgreSQL note").
			SetDailyUsageUsd(7).SetWeeklyUsageUsd(8).SetMonthlyUsageUsd(9).SetCustomUsageUsd(10).Save(ctx)
		require.NoError(t, createErr)
		persisted, getErr := client.UserSubscription.Get(ctx, card.ID)
		require.NoError(t, getErr)
		before = append(before, persisted)
	}
	repo := &postgresRefundPrecisionSubscriptionRepo{&pendingRefundSubscriptionRepo{client: client}}
	newService := func() *PaymentService {
		svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
			subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
		svc.subscriptionSvc.now = func() time.Time { return now }
		return svc
	}
	svc := newService()
	t.Cleanup(replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed}))
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "PostgreSQL precision regression", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	require.Zero(t, plan.SubscriptionID, "fixture must exercise the stacked ID=0 path")
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "pending")

	detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
	require.NoError(t, err)
	require.Len(t, detail.SubscriptionDeductionAfter, 2)
	for _, term := range detail.SubscriptionDeductionAfter {
		persisted, getErr := client.UserSubscription.Get(ctx, term.ID)
		require.NoError(t, getErr)
		require.True(t, term.ExpiresAt.Equal(persisted.ExpiresAt), "deduction fact must equal the persisted expiry for card %d", term.ID)
		require.Equal(t, persisted.Status, term.Status)
	}

	// A fresh service must recover exclusively from committed terms and audit.
	svc = newService()
	result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.NotContains(t, result.Warning, "rollback failed")
	for _, original := range before {
		after, getErr := client.UserSubscription.Get(ctx, original.ID)
		require.NoError(t, getErr)
		require.Equal(t, SubscriptionStatusActive, after.Status, "card %d must be usable after compensation", original.ID)
		require.True(t, original.ExpiresAt.Equal(after.ExpiresAt), "card %d must recover its exact persisted term", original.ID)
		require.Equal(t, original.DailyUsageUsd, after.DailyUsageUsd)
		require.Equal(t, original.WeeklyUsageUsd, after.WeeklyUsageUsd)
		require.Equal(t, original.MonthlyUsageUsd, after.MonthlyUsageUsd)
		require.Equal(t, original.CustomUsageUsd, after.CustomUsageUsd)
	}
	active, err := svc.subscriptionSvc.GetActiveSubscription(ctx, order.UserID, group.ID)
	require.NoError(t, err)
	require.Equal(t, 2, active.SubscriptionCount)
	settled, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, settled.Status)
	audit, err := client.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ(refundSubscriptionTermsReturned),
	).Only(ctx)
	require.NoError(t, err)
	var returned refundTermReturnDetail
	require.NoError(t, json.Unmarshal([]byte(audit.Detail), &returned))
	require.Len(t, returned.Terms, 2)
	for _, term := range returned.Terms {
		persisted, getErr := client.UserSubscription.Get(ctx, term.After.ID)
		require.NoError(t, getErr)
		require.True(t, term.After.ExpiresAt.Equal(persisted.ExpiresAt), "return fact must equal the persisted expiry")
		require.Equal(t, persisted.Status, term.After.Status)
	}
}

// The guard follows the repository integration harness, but never resets public
// or any existing schema. Cleanup is limited to the random schema created here.
func newRefundPrecisionPostgresClient(t *testing.T, ctx context.Context) (*dbent.Client, *sql.DB) {
	t.Helper()
	runID := os.Getenv("SUB2API_TEST_RUN_ID")
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`).MatchString(runID) {
		t.Fatal("SUB2API_TEST_RUN_ID must identify an explicitly provisioned disposable PostgreSQL database")
	}
	want := "sub2api_it_" + runID
	u, err := url.Parse(os.Getenv("SUB2API_TEST_POSTGRES_DSN"))
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		u.Hostname() == "" || u.User == nil || u.User.Username() != want || u.Path != "/"+want || u.Fragment != "" || u.Opaque != "" {
		t.Fatal("SUB2API_TEST_POSTGRES_DSN must explicitly target the run-scoped restricted login and database")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		t.Fatal("invalid PostgreSQL test URL options")
	}
	for key, values := range query {
		switch key {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "connect_timeout":
		default:
			t.Fatal("PostgreSQL test URL connection overrides are forbidden")
		}
		if len(values) != 1 {
			t.Fatal("duplicate PostgreSQL test URL option")
		}
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "5432")
	}
	query.Set("options", "-c search_path=public -c timezone=UTC")
	query.Set("search_path", "public")
	query.Set("TimeZone", "UTC")
	query.Set("connect_timeout", "5")
	u.RawQuery = query.Encode()
	connector, err := pq.NewConnector(u.String())
	if err != nil {
		t.Fatal("invalid disposable PostgreSQL connection options")
	}
	guardDB := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, guardDB.Close()) })
	var database, currentUser, sessionUser, owner, marker string
	var superuser, createDB, createRole, replication, bypassRLS, inherit, canLogin, member, ownsOtherDB, template, sharedAccess bool
	err = guardDB.QueryRowContext(ctx, `SELECT current_database(), current_user, session_user,
		pg_catalog.pg_get_userbyid(d.datdba), COALESCE(pg_catalog.shobj_description(d.oid, 'pg_database'), ''),
		r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolreplication, r.rolbypassrls, r.rolinherit, r.rolcanlogin,
		EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member = r.oid),
		EXISTS (SELECT 1 FROM pg_catalog.pg_database other WHERE other.datdba = r.oid AND other.oid <> d.oid),
		d.datistemplate,
		EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(d.datacl, pg_catalog.acldefault('d', d.datdba))) a WHERE a.grantee <> r.oid)
		FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles r ON r.rolname = current_user
		WHERE d.datname = current_database()`).Scan(
		&database, &currentUser, &sessionUser, &owner, &marker,
		&superuser, &createDB, &createRole, &replication, &bypassRLS, &inherit, &canLogin,
		&member, &ownsOtherDB, &template, &sharedAccess,
	)
	if err != nil {
		t.Fatal("disposable PostgreSQL identity check failed; connection details are not logged")
	}
	if database != want || currentUser != want || sessionUser != want || owner != want || marker != "sub2api-it:"+runID ||
		superuser || createDB || createRole || replication || bypassRLS || inherit || !canLogin || member || ownsOtherDB || template || sharedAccess {
		t.Fatal("PostgreSQL guard refused a database without the exact disposable marker and restricted run-scoped ownership")
	}
	lockConn, err := guardDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lockConn.Close()) })
	const suiteLockID int64 = 724082491865101
	var locked bool
	require.NoError(t, lockConn.QueryRowContext(ctx, "SELECT pg_catalog.pg_try_advisory_lock($1)", suiteLockID).Scan(&locked))
	require.True(t, locked, "the disposable database is already in use by an integration suite")
	t.Cleanup(func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, unlockErr := lockConn.ExecContext(unlockCtx, "SELECT pg_catalog.pg_advisory_unlock($1)", suiteLockID)
		require.NoError(t, unlockErr)
	})
	var nonce [6]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	name := "refund_" + runID + "_" + hex.EncodeToString(nonce[:])
	require.NoError(t, execRefundPrecisionPostgresDDL(ctx, guardDB, "CREATE SCHEMA "+pq.QuoteIdentifier(name)+" AUTHORIZATION CURRENT_USER"))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, execRefundPrecisionPostgresDDL(cleanupCtx, guardDB, "DROP SCHEMA "+pq.QuoteIdentifier(name)+" CASCADE"))
	})
	query.Set("options", "-c search_path="+name+" -c timezone=UTC")
	query.Set("search_path", name)
	u.RawQuery = query.Encode()
	connector, err = pq.NewConnector(u.String())
	if err != nil {
		t.Fatal("invalid isolated PostgreSQL schema connection options")
	}
	db := sql.OpenDB(connector)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	var actualSchema string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&actualSchema))
	require.Equal(t, name, actualSchema)
	require.NoError(t, client.Schema.Create(ctx, schema.WithSchemaName(name)))
	return client, db
}

func execRefundPrecisionPostgresDDL(ctx context.Context, db *sql.DB, statement string) error {
	_, err := db.ExecContext(ctx, statement)
	return err
}
