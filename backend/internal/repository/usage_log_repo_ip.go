package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

func optionalIPs(sets [][]string) []string {
	if len(sets) == 0 {
		return nil
	}
	return sets[0]
}
func appendIPQueryFilter(query string, args []any, ips []string, column string) (string, []any) {
	conditions, args := usagestats.AppendIPWhere(nil, args, ips, column)
	if len(conditions) > 0 {
		query += " AND " + conditions[0]
	}
	return query, args
}
func (r *usageLogRepository) GetIPOptionsWithFilters(ctx context.Context, filters UsageLogFilters, search string, offset, limit int) (*usagestats.UsageIPOptions, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must be nonnegative")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	filters.IPAddresses = nil
	conditions, args := usageLogFilterConditions(filters)
	conditions = append(conditions, "ip_address IS NOT NULL", "TRIM(ip_address) <> ''")
	if search = strings.TrimSpace(search); search != "" {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(search)
		conditions = append(conditions, fmt.Sprintf("ip_address ILIKE $%d ESCAPE E'\\\\'", len(args)+1))
		args = append(args, "%"+escaped+"%")
	}
	query := fmt.Sprintf("SELECT DISTINCT ip_address COLLATE \"C\" AS ip_address FROM usage_logs %s ORDER BY ip_address LIMIT $%d OFFSET $%d", buildWhere(conditions), len(args)+1, len(args)+2)
	args = append(args, limit+1, offset)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &usagestats.UsageIPOptions{IPs: make([]string, 0, limit)}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		result.IPs = append(result.IPs, ip)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(result.IPs) > limit {
		result.HasMore = true
		result.IPs = result.IPs[:limit]
	}
	return result, nil
}
