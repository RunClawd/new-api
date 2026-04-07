package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// AdminBgUsageStats represents the aggregate metrics for the BaseGate usage dashboard KPI cards.
// Mirrors model.BgUsageStatsResult — kept local to avoid import-cycle with dto.
type AdminBgUsageStats struct {
	TotalRequests  int64   `json:"total_requests"`
	SucceededCount int64   `json:"succeeded_count"`
	FailedCount    int64   `json:"failed_count"`
	RunningCount   int64   `json:"running_count"`
	TotalCost      float64 `json:"total_cost"`
	TotalTokens    int64   `json:"total_tokens"`
}

// AdminBgResponseDetail represents the full details of a BaseGate response.
type AdminBgResponseDetail struct {
	model.BgResponse
	Attempts       []model.BgResponseAttempt `json:"attempts"`
	UsageRecords   []model.BgUsageRecord     `json:"usage_records"`
	BillingRecords []model.BgBillingRecord   `json:"billing_records"`
}

// AdminBgSessionDetail represents the full details of a BaseGate session.
type AdminBgSessionDetail struct {
	model.BgSession
	Actions []model.BgSessionAction `json:"actions"`
}

// AdminListBgResponses handles GET /api/bg/responses
func AdminListBgResponses(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	modelName := c.Query("model")
	status := c.Query("status")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	responses, total, err := model.GetBgResponsesAdmin(orgID, modelName, status, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list responses: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(responses)
	common.ApiSuccess(c, pageInfo)
}

// AdminGetBgResponse handles GET /api/bg/responses/:id
func AdminGetBgResponse(c *gin.Context) {
	responseID := c.Param("id")
	if responseID == "" {
		common.ApiErrorMsg(c, "response_id is required")
		return
	}

	bgResp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil {
		common.ApiErrorMsg(c, "Response not found: "+err.Error())
		return
	}

	detail := &AdminBgResponseDetail{
		BgResponse: *bgResp,
	}

	// Load attempts
	attempts, _ := model.GetBgAttemptsByResponseID(responseID)
	if attempts != nil {
		detail.Attempts = attempts
	} else {
		detail.Attempts = []model.BgResponseAttempt{}
	}

	// Load usage records
	var usageRecords []model.BgUsageRecord
	model.DB.Where("response_id = ?", responseID).Find(&usageRecords)
	detail.UsageRecords = usageRecords

	// Load billing records
	var billingRecords []model.BgBillingRecord
	model.DB.Where("response_id = ?", responseID).Find(&billingRecords)
	detail.BillingRecords = billingRecords

	common.ApiSuccess(c, detail)
}

// AdminListBgSessions handles GET /api/bg/sessions
func AdminListBgSessions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	status := c.Query("status")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	sessions, total, err := model.GetBgSessionsAdmin(orgID, status, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list sessions: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(sessions)
	common.ApiSuccess(c, pageInfo)
}

// AdminGetBgSession handles GET /api/bg/sessions/:id
func AdminGetBgSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		common.ApiErrorMsg(c, "session_id is required")
		return
	}

	bgSession, err := model.GetBgSessionBySessionID(sessionID)
	if err != nil {
		common.ApiErrorMsg(c, "Session not found: "+err.Error())
		return
	}

	detail := &AdminBgSessionDetail{
		BgSession: *bgSession,
	}

	// Load actions
	var actions []model.BgSessionAction
	model.DB.Where("session_id = ?", sessionID).Order("action_id asc").Find(&actions)
	detail.Actions = actions

	common.ApiSuccess(c, detail)
}

// AdminListBgCapabilities handles GET /api/bg/capabilities
func AdminListBgCapabilities(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	capabilities, total, err := model.GetBgCapabilitiesAdmin(startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list capabilities: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(capabilities)
	common.ApiSuccess(c, pageInfo)
}

// AdminGetBgUsageStats handles GET /api/bg/usage/stats
func AdminGetBgUsageStats(c *gin.Context) {
	orgID, _ := strconv.Atoi(c.Query("org_id"))

	res, err := model.GetBgUsageStatsAdmin(orgID)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to get stats: "+err.Error())
		return
	}

	// Map model result to controller DTO (avoids import cycle).
	common.ApiSuccess(c, &AdminBgUsageStats{
		TotalRequests:  res.TotalRequests,
		SucceededCount: res.SucceededCount,
		FailedCount:    res.FailedCount,
		RunningCount:   res.RunningCount,
		TotalTokens:    res.TotalTokens,
		TotalCost:      res.TotalCost,
	})
}
