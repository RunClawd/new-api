package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// DevListBgApiKeys handles GET /api/bg/dev/apikeys
// Returns a paginated list of API keys for the current user.
// Supports ?project_id=proj_xxx (public identifier) for filtering.
func DevListBgApiKeys(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	projectIDStr := c.Query("project_id")
	if projectIDStr != "" {
		// Resolve public project_id to internal ID + org ownership check
		project, err := model.GetBgProjectByProjectID(projectIDStr)
		if err != nil || project.OrgID != userId {
			common.ApiErrorMsg(c, "project not found or not owned")
			return
		}

		tokens, err := model.GetUserTokensByBgProjectID(userId, project.ID, startIdx, num)
		if err != nil {
			common.ApiErrorMsg(c, "failed to list keys: "+err.Error())
			return
		}
		total, _ := model.CountUserTokensByBgProjectID(userId, project.ID)

		// Mask keys
		for _, t := range tokens {
			t.Key = t.GetMaskedKey()
		}

		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(tokens)
		common.ApiSuccess(c, pageInfo)
		return
	}

	// No project filter — all tokens for user
	tokens, err := model.GetAllUserTokens(userId, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "failed to list keys: "+err.Error())
		return
	}

	// Mask keys
	for _, t := range tokens {
		t.Key = t.GetMaskedKey()
	}

	// Count total for pagination
	var total int64
	model.DB.Model(&model.Token{}).Where("user_id = ?", userId).Count(&total)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tokens)
	common.ApiSuccess(c, pageInfo)
}

// DevCreateBgApiKey handles POST /api/bg/dev/apikeys
// Creates a new API key, optionally bound to a project.
// Returns the full plaintext key ONE TIME ONLY in the response.
func DevCreateBgApiKey(c *gin.Context) {
	userId := c.GetInt("id")

	var req struct {
		Name      string `json:"name" binding:"required"`
		ProjectID string `json:"project_id"` // optional public project_id
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request: "+err.Error())
		return
	}

	var bgProjectID int64
	if req.ProjectID != "" {
		project, err := model.GetBgProjectByProjectID(req.ProjectID)
		if err != nil {
			common.ApiErrorMsg(c, "project not found: "+req.ProjectID)
			return
		}
		if project.OrgID != userId {
			common.ApiErrorMsg(c, "project does not belong to current user")
			return
		}
		bgProjectID = project.ID
	}

	// Generate key
	cleanKey, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorMsg(c, "failed to generate key")
		return
	}

	token := &model.Token{
		UserId:         userId,
		Name:           req.Name,
		Key:            cleanKey,
		Status:         1,
		CreatedTime:    time.Now().Unix(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		BgProjectID:    bgProjectID,
	}

	if err := token.Insert(); err != nil {
		common.ApiErrorMsg(c, "failed to create key: "+err.Error())
		return
	}

	// Return the plaintext key ONE TIME
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":            token.Id,
			"name":          token.Name,
			"key":           cleanKey, // plaintext, one-time reveal
			"bg_project_id": bgProjectID,
			"created_time":  token.CreatedTime,
		},
	})
}

// DevDeleteBgApiKey handles DELETE /api/bg/dev/apikeys/:id
// Deletes an API key, only if owned by the current user.
func DevDeleteBgApiKey(c *gin.Context) {
	userId := c.GetInt("id")
	idStr := c.Param("id")

	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		common.ApiErrorMsg(c, "invalid key ID")
		return
	}

	// Verify ownership
	token, err := model.GetTokenById(id)
	if err != nil {
		common.ApiErrorMsg(c, "key not found")
		return
	}
	if token.UserId != userId {
		common.ApiErrorMsg(c, "key not found")
		return
	}

	err = model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiErrorMsg(c, "failed to delete key: "+err.Error())
		return
	}

	common.ApiSuccess(c, "ok")
}

// DevRevealBgApiKey handles POST /api/bg/dev/apikeys/:id/reveal
// Returns the full plaintext key. Protected by CriticalRateLimit + DisableCache middleware.
func DevRevealBgApiKey(c *gin.Context) {
	userId := c.GetInt("id")
	idStr := c.Param("id")

	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		common.ApiErrorMsg(c, "invalid key ID")
		return
	}

	token, err := model.GetTokenById(id)
	if err != nil {
		common.ApiErrorMsg(c, "key not found")
		return
	}
	if token.UserId != userId {
		common.ApiErrorMsg(c, "key not found")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"key": token.Key,
		},
	})
}
