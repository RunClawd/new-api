package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Capability Policies — Admin CRUD
// ---------------------------------------------------------------------------

// AdminListBgCapabilityPolicies handles GET /api/bg/policies/capabilities
func AdminListBgCapabilityPolicies(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	scope := c.Query("scope")
	scopeID, _ := strconv.Atoi(c.Query("scope_id"))

	policies, total, err := model.ListBgCapabilityPolicies(scope, scopeID, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list capability policies: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(policies)
	common.ApiSuccess(c, pageInfo)
}

// AdminCreateBgCapabilityPolicy handles POST /api/bg/policies/capabilities
func AdminCreateBgCapabilityPolicy(c *gin.Context) {
	var policy model.BgCapabilityPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}

	if err := model.CreateBgCapabilityPolicy(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "validation_error", "message": err.Error()},
		})
		return
	}

	// Audit log: written immediately after successful mutation
	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "capability_policy_created", map[string]interface{}{
		"policy_id": policy.ID,
		"scope":     policy.Scope,
		"scope_id":  policy.ScopeID,
		"pattern":   policy.CapabilityPattern,
		"action":    policy.Action,
	})

	// Sync cache
	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after create: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy saved but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	c.JSON(http.StatusCreated, policy)
}

// AdminUpdateBgCapabilityPolicy handles PUT /api/bg/policies/capabilities/:id
func AdminUpdateBgCapabilityPolicy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "invalid id"},
		})
		return
	}

	var policy model.BgCapabilityPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	policy.ID = id

	if err := model.UpdateBgCapabilityPolicy(&policy); err != nil {
		if err.Error() == "not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "not_found", "message": "policy not found"},
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"code": "validation_error", "message": err.Error()},
			})
		}
		return
	}

	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "capability_policy_updated", map[string]interface{}{
		"policy_id": id,
		"scope":     policy.Scope,
		"pattern":   policy.CapabilityPattern,
		"action":    policy.Action,
	})

	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after update: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy saved but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	common.ApiSuccess(c, policy)
}

// AdminDeleteBgCapabilityPolicy handles DELETE /api/bg/policies/capabilities/:id
func AdminDeleteBgCapabilityPolicy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "invalid id"},
		})
		return
	}

	if err := model.DeleteBgCapabilityPolicy(id); err != nil {
		if err.Error() == "not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "not_found", "message": "policy not found"},
			})
		} else {
			common.ApiErrorMsg(c, "Failed to delete policy: "+err.Error())
		}
		return
	}

	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "capability_policy_deleted", map[string]interface{}{
		"policy_id": id,
	})

	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after delete: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy deleted but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	common.ApiSuccess(c, "ok")
}

// ---------------------------------------------------------------------------
// Routing Policies — Admin CRUD
// ---------------------------------------------------------------------------

// AdminListBgRoutingPolicies handles GET /api/bg/policies/routing
func AdminListBgRoutingPolicies(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	scope := c.Query("scope")
	scopeID, _ := strconv.Atoi(c.Query("scope_id"))

	policies, total, err := model.ListBgRoutingPolicies(scope, scopeID, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list routing policies: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(policies)
	common.ApiSuccess(c, pageInfo)
}

// AdminCreateBgRoutingPolicy handles POST /api/bg/policies/routing
func AdminCreateBgRoutingPolicy(c *gin.Context) {
	var policy model.BgRoutingPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}

	if err := model.CreateBgRoutingPolicy(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "validation_error", "message": err.Error()},
		})
		return
	}

	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "routing_policy_created", map[string]interface{}{
		"policy_id": policy.ID,
		"scope":     policy.Scope,
		"scope_id":  policy.ScopeID,
		"pattern":   policy.CapabilityPattern,
		"strategy":  policy.Strategy,
	})

	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after create: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy saved but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	c.JSON(http.StatusCreated, policy)
}

// AdminUpdateBgRoutingPolicy handles PUT /api/bg/policies/routing/:id
func AdminUpdateBgRoutingPolicy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "invalid id"},
		})
		return
	}

	var policy model.BgRoutingPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": err.Error()},
		})
		return
	}
	policy.ID = id

	if err := model.UpdateBgRoutingPolicy(&policy); err != nil {
		if err.Error() == "not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "not_found", "message": "policy not found"},
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"code": "validation_error", "message": err.Error()},
			})
		}
		return
	}

	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "routing_policy_updated", map[string]interface{}{
		"policy_id": id,
		"scope":     policy.Scope,
		"pattern":   policy.CapabilityPattern,
		"strategy":  policy.Strategy,
	})

	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after update: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy saved but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	common.ApiSuccess(c, policy)
}

// AdminDeleteBgRoutingPolicy handles DELETE /api/bg/policies/routing/:id
func AdminDeleteBgRoutingPolicy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "invalid_request", "message": "invalid id"},
		})
		return
	}

	if err := model.DeleteBgRoutingPolicy(id); err != nil {
		if err.Error() == "not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "not_found", "message": "policy not found"},
			})
		} else {
			common.ApiErrorMsg(c, "Failed to delete policy: "+err.Error())
		}
		return
	}

	_ = model.RecordBgAuditLog(c.GetInt("id"), "", "", "routing_policy_deleted", map[string]interface{}{
		"policy_id": id,
	})

	if err := service.InvalidatePolicyCache(); err != nil {
		common.SysError("policy cache reload failed after delete: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "cache_sync_failed",
				"type":    "api_error",
				"message": "policy deleted but cache reload failed; retry or wait 60s for auto-refresh",
			},
		})
		return
	}

	common.ApiSuccess(c, "ok")
}
