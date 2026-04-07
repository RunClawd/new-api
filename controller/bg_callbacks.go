package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/basegate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// InboundProviderCallback handles asynchronous task completion callbacks from upstream providers.
// Route: POST /v1/bg/callbacks/:response_id
// Authentication: No TokenAuth() middleware — providers call this endpoint using the secret embedded
// in the callback URL that was supplied when the job was created. The controller verifies that the
// response_id is valid and delegates signature verification to the adapter's ParseCallback method.
func InboundProviderCallback(c *gin.Context) {
	responseID := c.Param("response_id")
	if responseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing response_id"})
		return
	}

	// 1. Fetch Response and resolve active attempt
	resp, err := model.GetBgResponseByResponseID(responseID)
	if err != nil || resp == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "response not found"})
		return
	}

	// Guard: ensure there is actually an active attempt
	if resp.ActiveAttemptID == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "response has no active attempt"})
		return
	}

	var attempt model.BgResponseAttempt
	if err = model.DB.Where("id = ?", resp.ActiveAttemptID).First(&attempt).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "active attempt not found"})
		return
	}

	// 2. Lookup Adapter
	providerAdapter := basegate.LookupAdapterByName(attempt.AdapterName)
	if providerAdapter == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "adapter not found for this attempt"})
		return
	}

	cbAdapter, ok := providerAdapter.(basegate.CallbackCapableAdapter)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the adapter handling this attempt does not support callbacks"})
		return
	}

	// 3. Delegate to Adapter for Parsing (adapter is responsible for its own signature verification)
	adapterResult, parseErr := cbAdapter.ParseCallback(c.Request)
	if parseErr != nil {
		common.SysError(fmt.Sprintf("bg_callbacks: adapter %s failed to parse callback for %s: %v", attempt.AdapterName, attempt.AttemptID, parseErr))
		c.JSON(http.StatusBadRequest, gin.H{"error": parseErr.Error()})
		return
	}

	// 4. Construct ProviderEvent — avoid double-serialize: build RawUsage map directly from struct
	var outputList []interface{}
	for _, out := range adapterResult.Output {
		outputList = append(outputList, out)
	}

	event := service.ProviderEvent{
		Status:            adapterResult.Status,
		Output:            outputList,
		PollAfterMs:       adapterResult.PollAfterMs,
		ProviderRequestID: adapterResult.ProviderRequestID,
	}

	// Build RawUsage map directly from struct fields (avoids Marshal→Unmarshal anti-pattern)
	if u := adapterResult.RawUsage; u != nil {
		rawUsageMap := map[string]interface{}{}
		if u.PromptTokens > 0 {
			rawUsageMap["prompt_tokens"] = u.PromptTokens
		}
		if u.CompletionTokens > 0 {
			rawUsageMap["completion_tokens"] = u.CompletionTokens
		}
		if u.TotalTokens > 0 {
			rawUsageMap["total_tokens"] = u.TotalTokens
		}
		if u.DurationSec > 0 {
			rawUsageMap["duration_sec"] = u.DurationSec
		}
		if u.SessionMinutes > 0 {
			rawUsageMap["session_minutes"] = u.SessionMinutes
		}
		if u.BillableUnits > 0 {
			rawUsageMap["billable_units"] = u.BillableUnits
			rawUsageMap["billable_unit"] = u.BillableUnit
		}
		event.RawUsage = rawUsageMap
	}

	if adapterResult.Error != nil {
		event.Error = map[string]interface{}{
			"code":    adapterResult.Error.Code,
			"message": adapterResult.Error.Message,
		}
	}

	// 5. Drive state machine
	if err = service.ApplyProviderEvent(attempt.ResponseID, attempt.AttemptID, event); err != nil {
		common.SysError(fmt.Sprintf("bg_callbacks: ApplyProviderEvent failed for %s: %v", attempt.AttemptID, err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal state transition failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "accepted"})
}
