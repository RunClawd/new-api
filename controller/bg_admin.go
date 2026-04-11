package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// AdminBgUsageStats represents the aggregate metrics for the BaseGate usage dashboard KPI cards.
// Mirrors model.BgUsageStatsResult — kept local to avoid import-cycle with dto.
type AdminBgUsageStats struct {
	TotalRequests  int64                        `json:"total_requests"`
	SucceededCount int64                        `json:"succeeded_count"`
	FailedCount    int64                        `json:"failed_count"`
	RunningCount   int64                        `json:"running_count"`
	TotalCost      float64                      `json:"total_cost"`
	TotalTokens    int64                        `json:"total_tokens"`
	Adapters       []basegate.AdapterCircuitInfo `json:"adapters,omitempty"`
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
	keyword := c.Query("q")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	responses, total, err := model.GetBgResponsesAdmin(orgID, modelName, status, keyword, startTimestamp, endTimestamp, startIdx, num)
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

// AdminCancelBgResponse handles POST /api/bg/responses/:id/cancel
func AdminCancelBgResponse(c *gin.Context) {
	responseID := c.Param("id")
	if responseID == "" {
		common.ApiErrorMsg(c, "response_id is required")
		return
	}

	resp, err := service.CancelResponse(responseID)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to cancel response: "+err.Error())
		return
	}

	common.ApiSuccess(c, resp)
}

// AdminListBgSessions handles GET /api/bg/sessions
func AdminListBgSessions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	modelName := c.Query("model")
	status := c.Query("status")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	sessions, total, err := model.GetBgSessionsAdmin(orgID, modelName, status, startTimestamp, endTimestamp, startIdx, num)
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

// AdminCloseBgSession handles POST /api/bg/sessions/:id/close
func AdminCloseBgSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		common.ApiErrorMsg(c, "session_id is required")
		return
	}

	_, err := service.CloseSession(sessionID)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to close session: "+err.Error())
		return
	}

	common.ApiSuccess(c, "ok")
}

// CapabilityWithPricing extends BgCapability with live pricing info from ratio_setting.
type CapabilityWithPricing struct {
	model.BgCapability
	PricingMode string  `json:"pricing_mode"` // "ratio" | "price" | "none"
	UnitPrice   float64 `json:"unit_price"`
}

// AdminListBgCapabilities handles GET /api/bg/capabilities
// Returns capabilities enriched with current pricing from the ratio_setting system.
func AdminListBgCapabilities(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	capabilities, total, err := model.GetBgCapabilitiesAdmin(startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list capabilities: "+err.Error())
		return
	}

	// Enrich with live pricing
	enriched := make([]CapabilityWithPricing, len(capabilities))
	for i, cap := range capabilities {
		enriched[i].BgCapability = *cap
		pricing := service.LookupPricing(cap.CapabilityName, "hosted")
		if pricing != nil && pricing.UnitPrice > 0 {
			if pricing.BillingMode == "per_call" {
				enriched[i].PricingMode = "price"
			} else {
				enriched[i].PricingMode = "ratio"
			}
			enriched[i].UnitPrice = pricing.UnitPrice
		} else {
			enriched[i].PricingMode = "none"
		}
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(enriched)
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
		Adapters:       basegate.ListCircuitStates(),
	})
}

// AdminListBgAdapters handles GET /api/bg/adapters
// Returns all registered BaseGate adapters with their capabilities and circuit breaker state.
func AdminListBgAdapters(c *gin.Context) {
	adapters := basegate.ListRegisteredAdapters()
	circuits := basegate.ListCircuitStates()

	// Build a circuit state lookup for merging
	circuitMap := make(map[string]basegate.AdapterCircuitInfo)
	for _, ci := range circuits {
		circuitMap[ci.Name] = ci
	}

	type AdapterDetail struct {
		basegate.AdapterInfo
		CircuitState         string `json:"circuit_state"`
		FailureCount         int    `json:"failure_count"`
		CooldownRemainingSec int64  `json:"cooldown_remaining_sec,omitempty"`
	}

	result := make([]AdapterDetail, 0, len(adapters))
	for _, a := range adapters {
		d := AdapterDetail{
			AdapterInfo:  a,
			CircuitState: basegate.CircuitClosed,
		}
		if ci, ok := circuitMap[a.Name]; ok {
			d.CircuitState = ci.State
			d.FailureCount = ci.FailureCount
			d.CooldownRemainingSec = ci.CooldownRemainingSec
		}
		result = append(result, d)
	}

	common.ApiSuccess(c, result)
}

// AdminReloadBgAdapters handles POST /api/bg/adapters/reload
// Clears and re-registers all adapters from the Channel table. No restart required.
func AdminReloadBgAdapters(c *gin.Context) {
	relay.ReloadNativeAdapters()
	count := basegate.RegisteredAdapterCount()
	common.ApiSuccess(c, gin.H{
		"message":       "adapter registry reloaded",
		"adapter_count": count,
	})
}

// AdminResetBgCircuit handles POST /api/bg/adapters/:name/reset
// Manually resets an adapter's circuit breaker to CLOSED.
func AdminResetBgCircuit(c *gin.Context) {
	adapterName := c.Param("name")
	if adapterName == "" {
		common.ApiErrorMsg(c, "adapter name is required")
		return
	}
	basegate.ResetCircuit(adapterName)
	common.ApiSuccess(c, gin.H{
		"message": "circuit reset to closed",
		"adapter": adapterName,
	})
}

// AdminListBgBillingRecords handles GET /api/bg/billing/records
func AdminListBgBillingRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	modelName := c.Query("model")
	responseID := c.Query("response_id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	records, total, err := model.GetBgBillingRecordsAdmin(orgID, modelName, responseID, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list billing records: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

// AdminListBgLedgerEntries handles GET /api/bg/billing/ledger
func AdminListBgLedgerEntries(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	entries, total, err := model.GetBgLedgerEntriesAdmin(orgID, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list ledger entries: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(entries)
	common.ApiSuccess(c, pageInfo)
}

// AdminListBgAuditLogs handles GET /api/bg/audit
func AdminListBgAuditLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	eventType := c.Query("event_type")
	responseID := c.Query("response_id")
	requestID := c.Query("request_id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	logs, total, err := model.GetBgAuditLogsAdmin(orgID, eventType, responseID, requestID, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list audit logs: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

// AdminListBgWebhookEvents handles GET /api/bg/webhooks
func AdminListBgWebhookEvents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))
	deliveryStatus := c.Query("delivery_status")
	responseID := c.Query("response_id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	events, total, err := model.GetBgWebhookEventsAdmin(orgID, deliveryStatus, responseID, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list webhook events: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

// AdminGetBgWebhookStats handles GET /api/bg/webhooks/stats
func AdminGetBgWebhookStats(c *gin.Context) {
	orgID, _ := strconv.Atoi(c.Query("org_id"))

	stats, err := model.GetBgWebhookStatsAdmin(orgID)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to get webhook stats: "+err.Error())
		return
	}

	common.ApiSuccess(c, stats)
}

// AdminRetryBgWebhookEvent handles POST /api/bg/webhooks/:id/retry
func AdminRetryBgWebhookEvent(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		common.ApiErrorMsg(c, "event_id is required")
		return
	}

	result := model.DB.Model(&model.BgWebhookEvent{}).
		Where("event_id = ?", eventID).
		Updates(map[string]interface{}{
			"delivery_status": model.WebhookStatusPending,
			"next_retry_at":   0,
		})
	if result.Error != nil {
		common.ApiErrorMsg(c, "Failed to retry webhook: "+result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		common.ApiErrorMsg(c, "webhook event not found")
		return
	}

	common.ApiSuccess(c, "ok")
}
