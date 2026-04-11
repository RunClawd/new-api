package model

// GetBgResponsesAdmin fetches a paginated list of BgResponses.
func GetBgResponsesAdmin(orgID int, modelName, status, keyword string, startTimestamp, endTimestamp int64, startIdx, num int) (responses []*BgResponse, total int64, err error) {
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
	if keyword != "" {
		tx = tx.Where("response_id = ? OR request_id = ?", keyword, keyword)
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
func GetBgSessionsAdmin(orgID int, modelName, status string, startTimestamp, endTimestamp int64, startIdx, num int) (sessions []*BgSession, total int64, err error) {
	tx := DB.Model(&BgSession{})

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

// GetBgBillingRecordsAdmin fetches a paginated list of BgBillingRecords.
func GetBgBillingRecordsAdmin(orgID int, modelName, responseID string, startTimestamp, endTimestamp int64, startIdx, num int) (records []*BgBillingRecord, total int64, err error) {
	tx := DB.Model(&BgBillingRecord{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}
	if modelName != "" {
		tx = tx.Where("model = ?", modelName)
	}
	if responseID != "" {
		tx = tx.Where("response_id = ?", responseID)
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

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&records).Error
	return records, total, err
}

// GetBgLedgerEntriesAdmin fetches a paginated list of BgLedgerEntries.
func GetBgLedgerEntriesAdmin(orgID int, startTimestamp, endTimestamp int64, startIdx, num int) (entries []*BgLedgerEntry, total int64, err error) {
	tx := DB.Model(&BgLedgerEntry{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
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

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&entries).Error
	return entries, total, err
}

// GetBgAuditLogsAdmin fetches a paginated list of BgAuditLogs.
func GetBgAuditLogsAdmin(orgID int, eventType, responseID, requestID string, startTimestamp, endTimestamp int64, startIdx, num int) (logs []*BgAuditLog, total int64, err error) {
	tx := DB.Model(&BgAuditLog{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}
	if eventType != "" {
		tx = tx.Where("event_type = ?", eventType)
	}
	if responseID != "" {
		tx = tx.Where("response_id = ?", responseID)
	}
	if requestID != "" {
		tx = tx.Where("request_id = ?", requestID)
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

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	return logs, total, err
}

// BgWebhookStatsResult holds aggregated webhook delivery statistics.
type BgWebhookStatsResult struct {
	Total     int64 `json:"total"`
	Delivered int64 `json:"delivered"`
	Pending   int64 `json:"pending"`
	Retrying  int64 `json:"retrying"`
	Dead      int64 `json:"dead"`
}

// GetBgWebhookEventsAdmin fetches a paginated list of BgWebhookEvents.
func GetBgWebhookEventsAdmin(orgID int, deliveryStatus, responseID string, startTimestamp, endTimestamp int64, startIdx, num int) (events []*BgWebhookEvent, total int64, err error) {
	tx := DB.Model(&BgWebhookEvent{})

	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}
	if deliveryStatus != "" {
		tx = tx.Where("delivery_status = ?", deliveryStatus)
	}
	if responseID != "" {
		tx = tx.Where("response_id = ?", responseID)
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

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&events).Error
	return events, total, err
}

// GetBgWebhookStatsAdmin returns aggregated delivery status counts.
func GetBgWebhookStatsAdmin(orgID int) (*BgWebhookStatsResult, error) {
	type row struct {
		DeliveryStatus string
		Count          int64
	}
	tx := DB.Model(&BgWebhookEvent{})
	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}

	var rows []row
	err := tx.Select("delivery_status, COUNT(*) as count").
		Group("delivery_status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := &BgWebhookStatsResult{}
	for _, r := range rows {
		result.Total += r.Count
		switch r.DeliveryStatus {
		case WebhookStatusDelivered:
			result.Delivered = r.Count
		case WebhookStatusPending:
			result.Pending = r.Count
		case WebhookStatusRetrying:
			result.Retrying = r.Count
		case WebhookStatusDead:
			result.Dead = r.Count
		}
	}
	return result, nil
}
