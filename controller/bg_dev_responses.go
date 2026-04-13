package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// DevListBgResponses handles GET /api/bg/dev/responses
// Returns a paginated list of responses for the current user's org.
func DevListBgResponses(c *gin.Context) {
	orgID := c.GetInt("id") // Forced from UserAuth session
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	modelName := c.Query("model")
	status := c.Query("status")
	keyword := c.Query("q")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	// Reuse admin query with forced org scope
	responses, total, err := model.GetBgResponsesAdmin(orgID, modelName, status, keyword, startTimestamp, endTimestamp, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "failed to list responses: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(responses)
	common.ApiSuccess(c, pageInfo)
}
