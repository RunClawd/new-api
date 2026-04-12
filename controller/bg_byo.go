package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ListBYOCredentials handles GET /byo_credentials
func ListBYOCredentials(c *gin.Context) {
	if !common.IsBYOEncryptionAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "BYO credentials are not available because encryption is not configured.",
		})
		return
	}

	orgID := c.GetInt("org_id")

	credentials, err := model.GetBgBYOCredentialsByOrgID(orgID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Do not return encrypted data to client
	res := make([]map[string]interface{}, 0, len(credentials))
	for _, cred := range credentials {
		res = append(res, map[string]interface{}{
			"id":         cred.ID,
			"name":       cred.Name,
			"provider":   cred.Provider,
			"created_at": cred.CreatedAt,
			"updated_at": cred.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    res,
	})
}

// CreateBYOCredential handles POST /byo_credentials
func CreateBYOCredential(c *gin.Context) {
	if !common.IsBYOEncryptionAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "BYO credentials are not available because encryption is not configured.",
		})
		return
	}

	orgID := c.GetInt("org_id")

	var req struct {
		Name     string            `json:"name" binding:"required"`
		Provider string            `json:"provider" binding:"required"`
		Data     map[string]string `json:"data" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	cred := &model.BgBYOCredential{
		OrgID:    orgID,
		Name:     req.Name,
		Provider: req.Provider,
	}

	if err := cred.SetPlaintextData(req.Data); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Failed to encrypt credential data: " + err.Error(),
		})
		return
	}

	if err := cred.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Failed to save credential: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": map[string]interface{}{
			"id":         cred.ID,
			"name":       cred.Name,
			"provider":   cred.Provider,
			"created_at": cred.CreatedAt,
			"updated_at": cred.UpdatedAt,
		},
	})
}

// GetBYOCredential handles GET /byo_credentials/:id
func GetBYOCredential(c *gin.Context) {
	if !common.IsBYOEncryptionAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "BYO credentials are not available because encryption is not configured.",
		})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid credential ID",
		})
		return
	}

	cred, err := model.GetBgBYOCredentialByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Credential not found",
		})
		return
	}

	if cred.OrgID != c.GetInt("org_id") {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Forbidden",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": map[string]interface{}{
			"id":         cred.ID,
			"name":       cred.Name,
			"provider":   cred.Provider,
			"created_at": cred.CreatedAt,
			"updated_at": cred.UpdatedAt,
		},
	})
}

// UpdateBYOCredential handles PUT /byo_credentials/:id
func UpdateBYOCredential(c *gin.Context) {
	if !common.IsBYOEncryptionAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "BYO credentials are not available because encryption is not configured.",
		})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid credential ID",
		})
		return
	}

	cred, err := model.GetBgBYOCredentialByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Credential not found",
		})
		return
	}

	if cred.OrgID != c.GetInt("org_id") {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Forbidden",
		})
		return
	}

	var req struct {
		Name     string            `json:"name"`
		Provider string            `json:"provider"`
		Data     map[string]string `json:"data"` // If provided, overwrites current data
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if req.Name != "" {
		cred.Name = req.Name
	}
	if req.Provider != "" {
		cred.Provider = req.Provider
	}

	if req.Data != nil && len(req.Data) > 0 {
		if err := cred.SetPlaintextData(req.Data); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Failed to encrypt credential data: " + err.Error(),
			})
			return
		}
	}

	if err := cred.Update(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Failed to update credential: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": map[string]interface{}{
			"id":         cred.ID,
			"name":       cred.Name,
			"provider":   cred.Provider,
			"created_at": cred.CreatedAt,
			"updated_at": cred.UpdatedAt,
		},
	})
}

// DeleteBYOCredential handles DELETE /byo_credentials/:id
func DeleteBYOCredential(c *gin.Context) {
	if !common.IsBYOEncryptionAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "BYO credentials are not available because encryption is not configured.",
		})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid credential ID",
		})
		return
	}

	cred, err := model.GetBgBYOCredentialByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Credential not found",
		})
		return
	}

	if cred.OrgID != c.GetInt("org_id") {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Forbidden",
		})
		return
	}

	if err := cred.Delete(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Failed to delete credential",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
