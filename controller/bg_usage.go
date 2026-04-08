package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetBgUsage returns aggregated usage metrics from BgUsageRecord.
// Route: GET /v1/bg/usage
//
// Query Parameters:
//   - start_date: ISO 8601 date "2024-01-01" (filters created_at >= start of that day UTC)
//   - end_date:   ISO 8601 date "2024-01-31" (filters created_at <= end of that day UTC)
//   - model:      optional model name filter
//   - granularity: "day" (default) | "hour" | "month"
//   - include_cost: "true" to join billing records and sum cost
//   - limit:  max rows per page (default 100, max 500)
//   - offset: pagination offset (default 0)
//
// NOTE: c.GetInt("id") returns the user ID, which is used as org scope in the current
// single-user-per-org identity model (user == org). This will need to be updated
// when proper organization tables are introduced.
func GetBgUsage(c *gin.Context) {
	// user ID doubles as org scope in the current identity model
	userID := c.GetInt("id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid auth context"})
		return
	}

	// --- Parameter parsing ---
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	modelName := c.Query("model")
	granularity := c.DefaultQuery("granularity", "day")
	includeCost := c.Query("include_cost") == "true"

	// Validate granularity
	switch granularity {
	case "day", "hour", "month":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid granularity %q: must be one of day, hour, month", granularity),
		})
		return
	}

	// Parse limit/offset
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

	// Parse date range → UNIX timestamps
	var startTS, endTS int64
	if startDateStr != "" {
		t, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date, use YYYY-MM-DD"})
			return
		}
		startTS = t.UTC().Unix()
	}
	if endDateStr != "" {
		t, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date, use YYYY-MM-DD"})
			return
		}
		// inclusive end: use start of next day minus 1 second
		endTS = t.UTC().AddDate(0, 0, 1).Unix() - 1
	}

	// --- Build query ---
	q := model.DB.Model(&model.BgUsageRecord{}).
		Where("bg_usage_records.org_id = ?", userID).
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

	// DB-specific date truncation
	dialector := model.DB.Dialector.Name()
	timeTruncExpr := timeTruncSQL(dialector, granularity)

	selectStr := fmt.Sprintf(
		`bg_usage_records.model,
		 %s AS time_bucket,
		 sum(bg_usage_records.billable_units) AS total_units,
		 sum(bg_usage_records.input_units) AS total_input,
		 sum(bg_usage_records.output_units) AS total_output`,
		timeTruncExpr,
	)

	if includeCost {
		selectStr += ", COALESCE(sum(bg_billing_records.amount), 0) AS total_cost"
		// Scope the join on both response_id AND org_id for defense-in-depth
		q = q.Joins(
			"LEFT JOIN bg_billing_records ON bg_usage_records.response_id = bg_billing_records.response_id AND bg_billing_records.org_id = ?",
			userID,
		)
	}

	q = q.Select(selectStr).
		Group(fmt.Sprintf("bg_usage_records.model, %s", timeTruncExpr)).
		Order("time_bucket DESC").
		Limit(limit).
		Offset(offset)

	type UsageResult struct {
		Model       string  `json:"model"`
		TimeBucket  string  `json:"time_bucket"`
		TotalUnits  float64 `json:"total_units"`
		TotalInput  float64 `json:"total_input"`
		TotalOutput float64 `json:"total_output"`
		TotalCost   float64 `json:"total_cost,omitempty"`
	}

	var results []UsageResult
	if err := q.Find(&results).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query usage: " + err.Error()})
		return
	}
	if results == nil {
		results = make([]UsageResult, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   results,
		"limit":  limit,
		"offset": offset,
	})
}

// timeTruncSQL returns a database-specific SQL expression that truncates a Unix timestamp
// column (bg_usage_records.created_at) to the requested granularity bucket.
func timeTruncSQL(dialector, granularity string) string {
	switch dialector {
	case "mysql":
		switch granularity {
		case "hour":
			return "DATE_FORMAT(FROM_UNIXTIME(bg_usage_records.created_at), '%Y-%m-%d %H:00:00')"
		case "month":
			return "DATE_FORMAT(FROM_UNIXTIME(bg_usage_records.created_at), '%Y-%m')"
		default: // day
			return "DATE_FORMAT(FROM_UNIXTIME(bg_usage_records.created_at), '%Y-%m-%d')"
		}
	case "postgres":
		switch granularity {
		case "hour":
			return "to_char(to_timestamp(bg_usage_records.created_at), 'YYYY-MM-DD HH24:00:00')"
		case "month":
			return "to_char(to_timestamp(bg_usage_records.created_at), 'YYYY-MM')"
		default:
			return "to_char(to_timestamp(bg_usage_records.created_at), 'YYYY-MM-DD')"
		}
	default: // sqlite (and explicit "sqlite" dialect)
		switch granularity {
		case "hour":
			return "strftime('%Y-%m-%d %H:00:00', bg_usage_records.created_at, 'unixepoch')"
		case "month":
			return "strftime('%Y-%m', bg_usage_records.created_at, 'unixepoch')"
		default:
			return "strftime('%Y-%m-%d', bg_usage_records.created_at, 'unixepoch')"
		}
	}
}

// AdminListBgUsage is the admin analogue for GetBgUsage.
// Route: GET /api/bg/usage
func AdminListBgUsage(c *gin.Context) {
	orgID, _ := strconv.Atoi(c.Query("org_id"))

	// --- Parameter parsing ---
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	modelName := c.Query("model")
	granularity := c.DefaultQuery("granularity", "day")
	includeCost := c.Query("include_cost") == "true"

	// Validate granularity
	switch granularity {
	case "day", "hour", "month":
		// valid
	default:
		common.ApiErrorMsg(c, "invalid granularity")
		return
	}

	// Parse limit/offset
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

	// Parse date range → UNIX timestamps
	var startTS, endTS int64
	if startDateStr != "" {
		t, err := time.Parse("2006-01-02", startDateStr)
		if err == nil {
			startTS = t.UTC().Unix()
		}
	}
	if endDateStr != "" {
		t, err := time.Parse("2006-01-02", endDateStr)
		if err == nil {
			endTS = t.UTC().AddDate(0, 0, 1).Unix() - 1
		}
	}

	// --- Build query ---
	q := model.DB.Model(&model.BgUsageRecord{}).
		Where("bg_usage_records.status = ?", "finalized")

	if orgID > 0 {
		q = q.Where("bg_usage_records.org_id = ?", orgID)
	}

	if modelName != "" {
		q = q.Where("bg_usage_records.model = ?", modelName)
	}
	if startTS > 0 {
		q = q.Where("bg_usage_records.created_at >= ?", startTS)
	}
	if endTS > 0 {
		q = q.Where("bg_usage_records.created_at <= ?", endTS)
	}

	// DB-specific date truncation
	dialector := model.DB.Dialector.Name()
	timeTruncExpr := timeTruncSQL(dialector, granularity)

	selectStr := fmt.Sprintf(
		`bg_usage_records.model,
		 %s AS time_bucket,
		 sum(bg_usage_records.billable_units) AS total_units,
		 sum(bg_usage_records.input_units) AS total_input,
		 sum(bg_usage_records.output_units) AS total_output`,
		timeTruncExpr,
	)

	if includeCost {
		selectStr += ", COALESCE(sum(bg_billing_records.amount), 0) AS total_cost"
		if orgID > 0 {
			q = q.Joins("LEFT JOIN bg_billing_records ON bg_usage_records.response_id = bg_billing_records.response_id AND bg_billing_records.org_id = ?", orgID)
		} else {
			q = q.Joins("LEFT JOIN bg_billing_records ON bg_usage_records.response_id = bg_billing_records.response_id")
		}
	}

	q = q.Select(selectStr).
		Group(fmt.Sprintf("bg_usage_records.model, %s", timeTruncExpr))

	var total int64
	if err := q.Count(&total).Error; err != nil {
		common.ApiErrorMsg(c, "failed to count usage: "+err.Error())
		return
	}

	q = q.Order("time_bucket DESC").
		Limit(limit).
		Offset(offset)

	type UsageResult struct {
		Model       string  `json:"model"`
		TimeBucket  string  `json:"time_bucket"`
		TotalUnits  float64 `json:"total_units"`
		TotalInput  float64 `json:"total_input"`
		TotalOutput float64 `json:"total_output"`
		TotalCost   float64 `json:"total_cost,omitempty"`
	}

	var results []UsageResult
	if err := q.Find(&results).Error; err != nil {
		common.ApiErrorMsg(c, "failed to query usage: "+err.Error())
		return
	}
	if results == nil {
		results = make([]UsageResult, 0)
	}

	common.ApiSuccess(c, gin.H{
		"items":  results,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
