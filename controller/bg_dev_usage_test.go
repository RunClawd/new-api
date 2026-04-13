package controller

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevGetBgUsage_TrendBuckets(t *testing.T) {
	cleanup := setupE2ETestEnv(t)
	defer cleanup()

	now := time.Now().UTC()
	ts := now.Unix()

	require.NoError(t, model.DB.Create(&model.BgUsageRecord{
		UsageID:       "usage_trend_1",
		ResponseID:    "resp_usage_trend_1",
		OrgID:         1,
		Model:         "bg.llm.trend",
		Status:        "finalized",
		BillableUnit:  "token",
		BillableUnits: 128,
		InputUnits:    64,
		OutputUnits:   64,
		CreatedAt:     ts,
	}).Error)
	require.NoError(t, model.DB.Create(&model.BgUsageRecord{
		UsageID:       "usage_trend_2",
		ResponseID:    "resp_usage_trend_2",
		OrgID:         1,
		Model:         "bg.llm.trend",
		Status:        "finalized",
		BillableUnit:  "token",
		BillableUnits: 256,
		InputUnits:    128,
		OutputUnits:   128,
		CreatedAt:     ts,
	}).Error)

	path := "/api/bg/dev/usage?start_date=" + now.Format("2006-01-02") +
		"&end_date=" + now.Format("2006-01-02") + "&granularity=hour&limit=100&offset=0"
	ctx, rec := newE2ECtx(t, http.MethodGet, path, nil)
	ctx.Request.URL.RawQuery = "start_date=" + now.Format("2006-01-02") +
		"&end_date=" + now.Format("2006-01-02") + "&granularity=hour&limit=100&offset=0"

	DevGetBgUsage(ctx)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				Model         string  `json:"model"`
				TimeBucket    string  `json:"time_bucket"`
				TotalRequests int64   `json:"total_requests"`
				TotalUnits    float64 `json:"total_units"`
			} `json:"items"`
			Granularity string `json:"granularity"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data.Items, 1)
	assert.Equal(t, "hour", resp.Data.Granularity)
	assert.Equal(t, "bg.llm.trend", resp.Data.Items[0].Model)
	assert.Equal(t, int64(2), resp.Data.Items[0].TotalRequests)
	assert.Equal(t, 384.0, resp.Data.Items[0].TotalUnits)
}
