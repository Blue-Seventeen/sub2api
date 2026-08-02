package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userSubscriptionRepository struct {
	client *dbent.Client
}

func NewUserSubscriptionRepository(client *dbent.Client) service.UserSubscriptionRepository {
	return &userSubscriptionRepository{client: client}
}

func (r *userSubscriptionRepository) Create(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.Create().
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetExpiresAt(sub.ExpiresAt).
		SetNillableDailyWindowStart(sub.DailyWindowStart).
		SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).
		SetNillableCustomWindowStart(sub.CustomWindowStart).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetCustomUsageUsd(sub.CustomUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy).
		SetNillableSourceType(sub.SourceType).
		SetNillableSourceRefID(sub.SourceRefID).
		SetNillableSourceRedeemCodeID(sub.SourceRedeemCodeID).
		SetNillableRedeemCodeSnapshot(sub.RedeemCodeSnapshot).
		SetNillableGroupNameSnapshot(sub.GroupNameSnapshot).
		SetNillableGroupPlatformSnapshot(sub.GroupPlatformSnapshot).
		SetNillableGroupRateMultiplierSnapshot(sub.GroupRateMultiplierSnapshot).
		SetNillableDailyLimitUsdSnapshot(sub.DailyLimitUSDSnapshot).
		SetNillableWeeklyLimitUsdSnapshot(sub.WeeklyLimitUSDSnapshot).
		SetNillableMonthlyLimitUsdSnapshot(sub.MonthlyLimitUSDSnapshot).
		SetNillableCustomLimitHoursSnapshot(sub.CustomLimitHoursSnapshot).
		SetNillableCustomLimitUsdSnapshot(sub.CustomLimitUSDSnapshot)

	if sub.StartsAt.IsZero() {
		builder.SetStartsAt(time.Now())
	} else {
		builder.SetStartsAt(sub.StartsAt)
	}
	if sub.Status != "" {
		builder.SetStatus(sub.Status)
	}
	if !sub.AssignedAt.IsZero() {
		builder.SetAssignedAt(sub.AssignedAt)
	}
	// Keep compatibility with historical behavior: always store notes as a string value.
	builder.SetNotes(sub.Notes)

	created, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, created)
	}
	return translatePersistenceError(err, nil, service.ErrSubscriptionAlreadyExists)
}

func (r *userSubscriptionRepository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Only(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToServicePreserveStatus(m), nil
}

func (r *userSubscriptionRepository) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) ListByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt), dbent.Desc(usersubscription.FieldID)).
		All(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		Order(dbent.Asc(usersubscription.FieldStartsAt), dbent.Asc(usersubscription.FieldID)).
		First(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		Order(dbent.Asc(usersubscription.FieldStartsAt), dbent.Asc(usersubscription.FieldID))
	if dbent.TxFromContext(ctx) != nil {
		query = query.ForUpdate()
	}
	subs, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListActiveUserIDsByGroupID(ctx context.Context, groupID int64) ([]int64, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.UserSubscription.Query().
		Where(
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		Unique(true).
		Select(usersubscription.FieldUserID).
		Ints(ctx)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(rows))
	for _, id := range rows {
		if id > 0 {
			userIDs = append(userIDs, int64(id))
		}
	}
	return userIDs, nil
}

func (r *userSubscriptionRepository) GetBySource(ctx context.Context, sourceType, sourceRefID string) (*service.UserSubscription, error) {
	sourceType = strings.TrimSpace(sourceType)
	sourceRefID = strings.TrimSpace(sourceRefID)
	if sourceType == "" || sourceRefID == "" {
		return nil, service.ErrSubscriptionNotFound
	}
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.SourceTypeEQ(sourceType), usersubscription.SourceRefIDEQ(sourceRefID)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		First(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) Update(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(sub.ID).
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetStartsAt(sub.StartsAt).
		SetExpiresAt(sub.ExpiresAt).
		SetStatus(sub.Status).
		SetNillableDailyWindowStart(sub.DailyWindowStart).
		SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).
		SetNillableCustomWindowStart(sub.CustomWindowStart).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetCustomUsageUsd(sub.CustomUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy).
		SetAssignedAt(sub.AssignedAt).
		SetNotes(sub.Notes).
		SetNillableSourceType(sub.SourceType).
		SetNillableSourceRefID(sub.SourceRefID).
		SetNillableSourceRedeemCodeID(sub.SourceRedeemCodeID).
		SetNillableRedeemCodeSnapshot(sub.RedeemCodeSnapshot).
		SetNillableGroupNameSnapshot(sub.GroupNameSnapshot).
		SetNillableGroupPlatformSnapshot(sub.GroupPlatformSnapshot).
		SetNillableGroupRateMultiplierSnapshot(sub.GroupRateMultiplierSnapshot).
		SetNillableDailyLimitUsdSnapshot(sub.DailyLimitUSDSnapshot).
		SetNillableWeeklyLimitUsdSnapshot(sub.WeeklyLimitUSDSnapshot).
		SetNillableMonthlyLimitUsdSnapshot(sub.MonthlyLimitUSDSnapshot).
		SetNillableCustomLimitHoursSnapshot(sub.CustomLimitHoursSnapshot).
		SetNillableCustomLimitUsdSnapshot(sub.CustomLimitUSDSnapshot)

	updated, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, updated)
		return nil
	}
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func (r *userSubscriptionRepository) UpdateMutableFields(ctx context.Context, subscriptionID int64, fields service.UserSubscriptionMutableFields) error {
	if subscriptionID <= 0 {
		return service.ErrSubscriptionNotFound
	}
	if fields.ExpiresAt == nil &&
		fields.Status == nil &&
		fields.Notes == nil &&
		fields.DailyUsageUSD == nil &&
		fields.WeeklyUsageUSD == nil &&
		fields.MonthlyUsageUSD == nil &&
		fields.CustomUsageUSD == nil {
		return nil
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(subscriptionID)
	if fields.ExpiresAt != nil {
		builder.SetExpiresAt(*fields.ExpiresAt)
	}
	if fields.Status != nil {
		builder.SetStatus(*fields.Status)
	}
	if fields.Notes != nil {
		builder.SetNotes(*fields.Notes)
	}
	if fields.DailyUsageUSD != nil {
		builder.SetDailyUsageUsd(*fields.DailyUsageUSD)
	}
	if fields.WeeklyUsageUSD != nil {
		builder.SetWeeklyUsageUsd(*fields.WeeklyUsageUSD)
	}
	if fields.MonthlyUsageUSD != nil {
		builder.SetMonthlyUsageUsd(*fields.MonthlyUsageUSD)
	}
	if fields.CustomUsageUSD != nil {
		builder.SetCustomUsageUsd(*fields.CustomUsageUSD)
	}
	_, err := builder.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateGroupSnapshot(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil || sub.ID <= 0 {
		return service.ErrSubscriptionNotFound
	}
	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(sub.ID).
		SetNillableGroupNameSnapshot(sub.GroupNameSnapshot).
		SetNillableGroupPlatformSnapshot(sub.GroupPlatformSnapshot).
		SetNillableGroupRateMultiplierSnapshot(sub.GroupRateMultiplierSnapshot).
		SetNillableDailyLimitUsdSnapshot(sub.DailyLimitUSDSnapshot).
		SetNillableWeeklyLimitUsdSnapshot(sub.WeeklyLimitUSDSnapshot).
		SetNillableMonthlyLimitUsdSnapshot(sub.MonthlyLimitUSDSnapshot).
		SetNillableCustomLimitHoursSnapshot(sub.CustomLimitHoursSnapshot).
		SetNillableCustomLimitUsdSnapshot(sub.CustomLimitUSDSnapshot)
	_, err := builder.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) Delete(ctx context.Context, id int64) error {
	// Match GORM semantics: deleting a missing row is not an error.
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.Delete().Where(usersubscription.IDEQ(id)).Exec(ctx)
	return err
}

func (r *userSubscriptionRepository) Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(restoredStatus).
		ClearDeletedAt().
		SetUpdatedAt(time.Now()).
		Save(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionRestoreConflict)
	}
	return r.GetByID(ctx, subscriptionID)
}

func (r *userSubscriptionRepository) HardDelete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Delete().
		Where(usersubscription.IDEQ(id)).
		Exec(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrSubscriptionNotFound
	}
	return nil
}

func (r *userSubscriptionRepository) ListByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID)).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListByUserIDIncludeDeleted(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID)).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	out := userSubscriptionEntitiesToService(subs)
	return out, nil
}

func (r *userSubscriptionRepository) ListActiveByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	subs, err := q.
		WithUser().
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return userSubscriptionEntitiesToService(subs), paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query()
	includeSoftDeleted := status == "" || status == service.SubscriptionStatusRevoked
	if userID != nil {
		q = q.Where(usersubscription.UserIDEQ(*userID))
	}
	if groupID != nil {
		q = q.Where(usersubscription.GroupIDEQ(*groupID))
	}
	if platform != "" {
		q = q.Where(usersubscription.Or(
			usersubscription.HasGroupWith(group.PlatformEQ(platform)),
			usersubscription.GroupPlatformSnapshotEQ(platform),
		))
	}

	// Status filtering with real-time expiration check
	now := time.Now()
	switch status {
	case service.SubscriptionStatusActive:
		// Active: status is active AND not yet expired
		q = q.Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(now),
		)
	case service.SubscriptionStatusExpired:
		// Expired: non-deleted active/expired records whose expiration time has passed.
		q = q.Where(
			usersubscription.StatusIn(service.SubscriptionStatusActive, service.SubscriptionStatusExpired),
			usersubscription.ExpiresAtLTE(now),
		)
	case service.SubscriptionStatusRevoked:
		q = q.Where(usersubscription.Or(
			usersubscription.StatusEQ(service.SubscriptionStatusRevoked),
			usersubscription.DeletedAtNotNil(),
		))
	case "":
		// No filter. Use SkipSoftDelete below so admin "all status" includes revoked history.
	default:
		// Other persisted status.
		q = q.Where(usersubscription.StatusEQ(status))
	}

	queryCtx := ctx
	if includeSoftDeleted {
		queryCtx = mixins.SkipSoftDelete(ctx)
	}

	total, err := q.Clone().Count(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	if !includeSoftDeleted {
		q = q.WithUser().WithGroup().WithAssignedByUser()
	}

	// Determine sort field
	var field string
	switch sortBy {
	case "starts_at":
		field = usersubscription.FieldStartsAt
	case "expires_at":
		field = usersubscription.FieldExpiresAt
	case "status":
		field = usersubscription.FieldStatus
	default:
		field = usersubscription.FieldCreatedAt
	}

	// Determine sort order (default: desc)
	if sortOrder == "asc" && sortBy != "" {
		q = q.Order(dbent.Asc(field))
	} else {
		q = q.Order(dbent.Desc(field))
	}

	subs, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	result := userSubscriptionEntitiesToService(subs)
	if includeSoftDeleted {
		if err := r.attachUserSubscriptionRelations(ctx, result); err != nil {
			return nil, nil, err
		}
	}

	return result, paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		Exist(ctx)
}

func (r *userSubscriptionRepository) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.ExistsByUserIDAndGroupID(ctx, userID, groupID)
}

func (r *userSubscriptionRepository) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetExpiresAt(newExpiresAt).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateStatus(ctx context.Context, subscriptionID int64, status string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(status).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetNotes(notes).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ActivateWindows(ctx context.Context, id int64, start time.Time) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Update().
		Where(
			usersubscription.IDEQ(id),
			usersubscription.DailyWindowStartIsNil(),
			usersubscription.WeeklyWindowStartIsNil(),
			usersubscription.MonthlyWindowStartIsNil(),
		).
		SetDailyWindowStart(start).
		SetWeeklyWindowStart(start).
		SetMonthlyWindowStart(start).
		SetCustomWindowStart(start).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	update := client.UserSubscription.UpdateOneID(id)
	if resetDaily {
		update.SetDailyUsageUsd(0).SetDailyWindowStart(newWindowStart)
	}
	if resetWeekly {
		update.SetWeeklyUsageUsd(0).SetWeeklyWindowStart(newWindowStart)
	}
	if resetMonthly {
		update.SetMonthlyUsageUsd(0).SetMonthlyWindowStart(newWindowStart)
	}
	_, err := update.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.DailyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.DailyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetDailyUsageUsd(0).
		SetDailyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.WeeklyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.WeeklyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetWeeklyUsageUsd(0).
		SetWeeklyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.MonthlyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.MonthlyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetMonthlyUsageUsd(0).
		SetMonthlyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) translateConditionalWindowReset(ctx context.Context, client *dbent.Client, id int64, affected int, err error) error {
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if affected > 0 {
		return nil
	}

	// A stale reset is an expected no-op: another request already advanced the
	// window. Preserve not-found semantics for callers that target a missing row.
	exists, err := client.UserSubscription.Query().Where(usersubscription.IDEQ(id)).Exist(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if !exists {
		return service.ErrSubscriptionNotFound
	}
	return nil
}

func (r *userSubscriptionRepository) ResetCustomUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetCustomUsageUsd(0).
		SetCustomWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) RollDailyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	return r.rollUsageWindow(ctx, id, "daily_usage_usd", "daily_window_start", oldWindowStart, newWindowStart, previousUsage, expectedUpdatedAt)
}

func (r *userSubscriptionRepository) RollWeeklyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	return r.rollUsageWindow(ctx, id, "weekly_usage_usd", "weekly_window_start", oldWindowStart, newWindowStart, previousUsage, expectedUpdatedAt)
}

func (r *userSubscriptionRepository) RollMonthlyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	return r.rollUsageWindow(ctx, id, "monthly_usage_usd", "monthly_window_start", oldWindowStart, newWindowStart, previousUsage, expectedUpdatedAt)
}

func (r *userSubscriptionRepository) RollCustomUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	return r.rollUsageWindow(ctx, id, "custom_usage_usd", "custom_window_start", oldWindowStart, newWindowStart, previousUsage, expectedUpdatedAt)
}

func (r *userSubscriptionRepository) rollUsageWindow(ctx context.Context, id int64, usageColumn, windowColumn string, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	updateSQL := fmt.Sprintf(`
		UPDATE user_subscriptions
		SET
			%s = CASE
				WHEN updated_at > $5 THEN GREATEST(%s - $4, 0)
				ELSE 0
			END,
			%s = $2,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
			AND %s = $3
	`, usageColumn, usageColumn, windowColumn, windowColumn)
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, id, newWindowStart, oldWindowStart, previousUsage, expectedUpdatedAt)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// IncrementUsage 原子性地累加订阅用量。
// 限额检查已在请求前由 BillingCacheService.CheckBillingEligibility 完成，
// 此处仅负责记录实际消费，确保消费数据的完整性。
func (r *userSubscriptionRepository) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			custom_usage_usd = us.custom_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, costUSD, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected > 0 {
		return nil
	}

	// affected == 0：订阅不存在或已删除
	return service.ErrSubscriptionNotFound
}

func (r *userSubscriptionRepository) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {
	client := clientFromContext(ctx, r.client)
	const updateSQL = `
		WITH expired AS (
			SELECT
				us.id,
				g.id AS group_snapshot_id,
				g.name AS group_name,
				g.platform AS group_platform,
				g.rate_multiplier AS group_rate_multiplier,
				g.daily_limit_usd,
				g.weekly_limit_usd,
				g.monthly_limit_usd,
				g.custom_limit_hours,
				g.custom_limit_usd
			FROM user_subscriptions us
			LEFT JOIN groups g ON g.id = us.group_id
			WHERE us.deleted_at IS NULL
				AND us.status = $2
				AND us.expires_at <= $3
		)
		UPDATE user_subscriptions us
		SET
			status = $1,
			group_name_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.group_name_snapshot ELSE expired.group_name END,
			group_platform_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.group_platform_snapshot ELSE expired.group_platform END,
			group_rate_multiplier_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.group_rate_multiplier_snapshot ELSE expired.group_rate_multiplier END,
			daily_limit_usd_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.daily_limit_usd_snapshot ELSE COALESCE(expired.daily_limit_usd, 0) END,
			weekly_limit_usd_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.weekly_limit_usd_snapshot ELSE COALESCE(expired.weekly_limit_usd, 0) END,
			monthly_limit_usd_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.monthly_limit_usd_snapshot ELSE COALESCE(expired.monthly_limit_usd, 0) END,
			custom_limit_hours_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.custom_limit_hours_snapshot ELSE COALESCE(expired.custom_limit_hours, 0) END,
			custom_limit_usd_snapshot = CASE WHEN expired.group_snapshot_id IS NULL THEN us.custom_limit_usd_snapshot ELSE COALESCE(expired.custom_limit_usd, 0) END,
			updated_at = NOW()
		FROM expired
		WHERE us.id = expired.id
	`
	result, err := client.ExecContext(ctx, updateSQL, service.SubscriptionStatusExpired, service.SubscriptionStatusActive, time.Now())
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// Extra repository helpers (currently used only by integration tests).

func (r *userSubscriptionRepository) ListExpired(ctx context.Context) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtLTE(time.Now()),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID)).Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) CountActiveByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().
		Where(
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) DeleteByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Delete().Where(usersubscription.GroupIDEQ(groupID)).Exec(ctx)
	return int64(n), err
}

func (r *userSubscriptionRepository) attachUserSubscriptionRelations(ctx context.Context, subs []service.UserSubscription) error {
	if len(subs) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(subs))
	groupIDs := make([]int64, 0, len(subs))
	assignedByIDs := make([]int64, 0, len(subs))
	for i := range subs {
		userIDs = append(userIDs, subs[i].UserID)
		groupIDs = append(groupIDs, subs[i].GroupID)
		if subs[i].AssignedBy != nil {
			assignedByIDs = append(assignedByIDs, *subs[i].AssignedBy)
		}
	}

	client := clientFromContext(ctx, r.client)
	users, err := client.User.Query().Where(user.IDIn(uniqueInt64s(userIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	userByID := make(map[int64]*service.User, len(users))
	for _, u := range users {
		userByID[u.ID] = userEntityToService(u)
	}

	groups, err := client.Group.Query().Where(group.IDIn(uniqueInt64s(groupIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	groupByID := make(map[int64]*service.Group, len(groups))
	for _, g := range groups {
		groupByID[g.ID] = groupEntityToService(g)
	}

	assignedByID := map[int64]*service.User{}
	if len(assignedByIDs) > 0 {
		assignedUsers, err := client.User.Query().Where(user.IDIn(uniqueInt64s(assignedByIDs)...)).All(ctx)
		if err != nil {
			return err
		}
		assignedByID = make(map[int64]*service.User, len(assignedUsers))
		for _, u := range assignedUsers {
			assignedByID[u.ID] = userEntityToService(u)
		}
	}

	for i := range subs {
		subs[i].User = userByID[subs[i].UserID]
		subs[i].Group = groupByID[subs[i].GroupID]
		if subs[i].AssignedBy != nil {
			subs[i].AssignedByUser = assignedByID[*subs[i].AssignedBy]
		}
	}
	return nil
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func userSubscriptionEntityToService(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, true)
}

func userSubscriptionEntityToServicePreserveStatus(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, false)
}

func userSubscriptionEntityToServiceWithStatusMapping(m *dbent.UserSubscription, mapDeletedToRevoked bool) *service.UserSubscription {
	if m == nil {
		return nil
	}
	status := m.Status
	if mapDeletedToRevoked && m.DeletedAt != nil {
		status = service.SubscriptionStatusRevoked
	}
	out := &service.UserSubscription{
		ID:                          m.ID,
		UserID:                      m.UserID,
		GroupID:                     m.GroupID,
		StartsAt:                    m.StartsAt,
		ExpiresAt:                   m.ExpiresAt,
		Status:                      status,
		DailyWindowStart:            m.DailyWindowStart,
		WeeklyWindowStart:           m.WeeklyWindowStart,
		MonthlyWindowStart:          m.MonthlyWindowStart,
		CustomWindowStart:           m.CustomWindowStart,
		DailyUsageUSD:               m.DailyUsageUsd,
		WeeklyUsageUSD:              m.WeeklyUsageUsd,
		MonthlyUsageUSD:             m.MonthlyUsageUsd,
		CustomUsageUSD:              m.CustomUsageUsd,
		AssignedBy:                  m.AssignedBy,
		AssignedAt:                  m.AssignedAt,
		Notes:                       derefString(m.Notes),
		SourceType:                  m.SourceType,
		SourceRefID:                 m.SourceRefID,
		SourceRedeemCodeID:          m.SourceRedeemCodeID,
		RedeemCodeSnapshot:          m.RedeemCodeSnapshot,
		GroupNameSnapshot:           m.GroupNameSnapshot,
		GroupPlatformSnapshot:       m.GroupPlatformSnapshot,
		GroupRateMultiplierSnapshot: m.GroupRateMultiplierSnapshot,
		DailyLimitUSDSnapshot:       m.DailyLimitUsdSnapshot,
		WeeklyLimitUSDSnapshot:      m.WeeklyLimitUsdSnapshot,
		MonthlyLimitUSDSnapshot:     m.MonthlyLimitUsdSnapshot,
		CustomLimitHoursSnapshot:    m.CustomLimitHoursSnapshot,
		CustomLimitUSDSnapshot:      m.CustomLimitUsdSnapshot,
		CreatedAt:                   m.CreatedAt,
		UpdatedAt:                   m.UpdatedAt,
		DeletedAt:                   m.DeletedAt,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	if m.Edges.AssignedByUser != nil {
		out.AssignedByUser = userEntityToService(m.Edges.AssignedByUser)
	}
	return out
}

func userSubscriptionEntitiesToService(models []*dbent.UserSubscription) []service.UserSubscription {
	out := make([]service.UserSubscription, 0, len(models))
	for i := range models {
		if s := userSubscriptionEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func applyUserSubscriptionEntityToService(dst *service.UserSubscription, src *dbent.UserSubscription) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
