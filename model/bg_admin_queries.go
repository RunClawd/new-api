package model

// GetBgResponsesAdmin fetches a paginated list of BgResponses.
func GetBgResponsesAdmin(orgID int, modelName, status string, startTimestamp, endTimestamp int64, startIdx, num int) (responses []*BgResponse, total int64, err error) {
	tx := DB.Model(&BgResponse{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}
	if modelName != "" {
		tx = tx.Where("model = ?", modelName)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if startTimestamp > 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp > 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}

	err = tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&responses).Error
	return responses, total, err
}

// GetBgSessionsAdmin fetches a paginated list of BgSessions.
func GetBgSessionsAdmin(orgID int, status string, startTimestamp, endTimestamp int64, startIdx, num int) (sessions []*BgSession, total int64, err error) {
	tx := DB.Model(&BgSession{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if startTimestamp > 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp > 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}

	err = tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&sessions).Error
	return sessions, total, err
}

// GetBgCapabilitiesAdmin fetches a paginated list of BgCapabilities.
func GetBgCapabilitiesAdmin(startIdx, num int) (capabilities []*BgCapability, total int64, err error) {
	tx := DB.Model(&BgCapability{})

	err = tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = tx.Order("capability_name asc").Limit(num).Offset(startIdx).Find(&capabilities).Error
	return capabilities, total, err
}

// BgUsageStatsResult holds the aggregated stats returned by GetBgUsageStatsAdmin.
type BgUsageStatsResult struct {
	TotalRequests  int64   `json:"total_requests"`
	SucceededCount int64   `json:"succeeded_count"`
	FailedCount    int64   `json:"failed_count"`
	// RunningCount covers all non-terminal states: queued, accepted, running.
	RunningCount int64   `json:"running_count"`
	TotalCost    float64 `json:"total_cost"`  // SUM from bg_billing_records
	TotalTokens  int64   `json:"total_tokens"` // SUM from bg_usage_records WHERE billable_unit='token'
}

// GetBgUsageStatsAdmin calculates aggregate dashboard statistics.
// Request-level counts come from bg_responses; cost/tokens from billing/usage tables.
func GetBgUsageStatsAdmin(orgID int) (*BgUsageStatsResult, error) {
	tx := DB.Model(&BgResponse{})
	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}

	var res BgUsageStatsResult

	// Single pass: request-level aggregation from bg_responses.
	err := tx.Select(`
		COUNT(*) as total_requests,
		SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END) as succeeded_count,
		SUM(CASE WHEN status = 'failed'    THEN 1 ELSE 0 END) as failed_count,
		SUM(CASE WHEN status NOT IN ('succeeded', 'failed', 'canceled', 'expired') THEN 1 ELSE 0 END) as running_count
	`).Scan(&res).Error
	if err != nil {
		return nil, err
	}

	// Total tokens: SUM(billable_units) from bg_usage_records WHERE billable_unit='token'.
	usageTx := DB.Model(&BgUsageRecord{}).Where("billable_unit = 'token'")
	if orgID > 0 {
		usageTx = usageTx.Where("org_id = ?", orgID)
	}
	if err := usageTx.Select("COALESCE(SUM(billable_units), 0)").Scan(&res.TotalTokens).Error; err != nil {
		return nil, err
	}

	// Total cost: SUM(amount) from bg_billing_records.
	billingTx := DB.Model(&BgBillingRecord{})
	if orgID > 0 {
		billingTx = billingTx.Where("org_id = ?", orgID)
	}
	if err := billingTx.Select("COALESCE(SUM(amount), 0)").Scan(&res.TotalCost).Error; err != nil {
		return nil, err
	}

	return &res, nil
}
