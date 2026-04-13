package controller

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// DevGetBgUsage handles GET /api/bg/dev/usage
// Returns usage statistics for the current user's org.
// Reuses the same query logic as GetBgUsage but with UserAuth session context.
func DevGetBgUsage(c *gin.Context) {
	orgID := c.GetInt("id") // From UserAuth, same context key as TokenAuth

	// When the caller provides usage query parameters, return bucketed trend data
	// for charts instead of the summary KPI payload.
	if c.Query("start_date") != "" || c.Query("end_date") != "" || c.Query("model") != "" ||
		c.Query("granularity") != "" || c.Query("limit") != "" || c.Query("offset") != "" ||
		c.Query("include_cost") != "" {
		devListBgUsageBuckets(c, orgID)
		return
	}

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	modelName := c.Query("model")
	granularity := c.DefaultQuery("granularity", "day")
	includeCost := c.DefaultQuery("include_cost", "false") == "true"

	var startTimestamp, endTimestamp int64
	if startDate != "" {
		startTimestamp, _ = strconv.ParseInt(startDate, 10, 64)
	}
	if endDate != "" {
		endTimestamp, _ = strconv.ParseInt(endDate, 10, 64)
	}

	// Reuse admin stats query with forced org scope
	stats, err := model.GetBgUsageStatsAdmin(orgID)
	if err != nil {
		common.ApiErrorMsg(c, "failed to get usage stats: "+err.Error())
		return
	}

	result := gin.H{
		"total_requests":  stats.TotalRequests,
		"succeeded_count": stats.SucceededCount,
		"failed_count":    stats.FailedCount,
		"running_count":   stats.RunningCount,
		"total_tokens":    stats.TotalTokens,
		"granularity":     granularity,
		"model":           modelName,
		"start_date":      startTimestamp,
		"end_date":        endTimestamp,
	}
	if includeCost {
		result["total_cost"] = stats.TotalCost
	}

	common.ApiSuccess(c, result)
}

func devListBgUsageBuckets(c *gin.Context, orgID int) {
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	modelName := c.Query("model")
	granularity := c.DefaultQuery("granularity", "day")
	includeCost := c.Query("include_cost") == "true"

	switch granularity {
	case "day", "hour", "month":
	default:
		common.ApiErrorMsg(c, fmt.Sprintf("invalid granularity %q", granularity))
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	var startTS, endTS int64
	if startDateStr != "" {
		t, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			common.ApiErrorMsg(c, "invalid start_date, use YYYY-MM-DD")
			return
		}
		startTS = t.UTC().Unix()
	}
	if endDateStr != "" {
		t, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			common.ApiErrorMsg(c, "invalid end_date, use YYYY-MM-DD")
			return
		}
		endTS = t.UTC().AddDate(0, 0, 1).Unix() - 1
	}

	q := model.DB.Model(&model.BgUsageRecord{}).
		Where("bg_usage_records.org_id = ?", orgID).
		Where("bg_usage_records.status = ?", "finalized")

	if modelName != "" {
		q = q.Where("bg_usage_records.model = ?", modelName)
	}
	if startTS > 0 {
		q = q.Where("bg_usage_records.created_at >= ?", startTS)
	}
	if endTS > 0 {
		q = q.Where("bg_usage_records.created_at <= ?", endTS)
	}

	dialector := model.DB.Dialector.Name()
	timeTruncExpr := timeTruncSQL(dialector, granularity)
	selectStr := fmt.Sprintf(
		`bg_usage_records.model,
		 %s AS time_bucket,
		 COUNT(*) AS total_requests,
		 sum(bg_usage_records.billable_units) AS total_units,
		 sum(bg_usage_records.input_units) AS total_input,
		 sum(bg_usage_records.output_units) AS total_output`,
		timeTruncExpr,
	)

	if includeCost {
		selectStr += ", COALESCE(sum(bg_billing_records.amount), 0) AS total_cost"
		q = q.Joins(
			"LEFT JOIN bg_billing_records ON bg_usage_records.response_id = bg_billing_records.response_id AND bg_billing_records.org_id = ?",
			orgID,
		)
	}

	q = q.Select(selectStr).
		Group(fmt.Sprintf("bg_usage_records.model, %s", timeTruncExpr)).
		Order("time_bucket DESC").
		Limit(limit).
		Offset(offset)

	type UsageBucket struct {
		Model         string  `json:"model"`
		TimeBucket    string  `json:"time_bucket"`
		TotalRequests int64   `json:"total_requests"`
		TotalUnits    float64 `json:"total_units"`
		TotalInput    float64 `json:"total_input"`
		TotalOutput   float64 `json:"total_output"`
		TotalCost     float64 `json:"total_cost,omitempty"`
	}

	var results []UsageBucket
	if err := q.Find(&results).Error; err != nil {
		common.ApiErrorMsg(c, "failed to query usage: "+err.Error())
		return
	}
	if results == nil {
		results = make([]UsageBucket, 0)
	}

	common.ApiSuccess(c, gin.H{
		"items":       results,
		"limit":       limit,
		"offset":      offset,
		"granularity": granularity,
	})
}
