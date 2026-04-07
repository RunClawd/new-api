package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// AdminListBgProjects handles GET /api/bg/projects?org_id=&p=&page_size=
func AdminListBgProjects(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startIdx := pageInfo.GetStartIdx()
	num := pageInfo.GetPageSize()

	orgID, _ := strconv.Atoi(c.Query("org_id"))

	projects, total, err := model.ListBgProjectsByOrgID(orgID, startIdx, num)
	if err != nil {
		common.ApiErrorMsg(c, "Failed to list projects: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(projects)
	common.ApiSuccess(c, pageInfo)
}

// AdminCreateBgProject handles POST /api/bg/projects
func AdminCreateBgProject(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		OrgID       int    `json:"org_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "Invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		common.ApiErrorMsg(c, "name is required")
		return
	}

	project := &model.BgProject{
		ProjectID:   relaycommon.GenerateProjectID(),
		OrgID:       req.OrgID,
		Name:        req.Name,
		Description: req.Description,
		Status:      "active",
	}

	if err := model.CreateBgProject(project); err != nil {
		common.ApiErrorMsg(c, "Failed to create project: "+err.Error())
		return
	}

	common.ApiSuccess(c, project)
}

// AdminUpdateBgProject handles PUT /api/bg/projects/:id
func AdminUpdateBgProject(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		common.ApiErrorMsg(c, "project_id is required")
		return
	}

	existing, err := model.GetBgProjectByProjectID(projectID)
	if err != nil {
		common.ApiErrorMsg(c, "Project not found: "+err.Error())
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "Invalid request body: "+err.Error())
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Status != "" {
		existing.Status = req.Status
	}

	if err := model.UpdateBgProject(existing); err != nil {
		common.ApiErrorMsg(c, "Failed to update project: "+err.Error())
		return
	}

	common.ApiSuccess(c, existing)
}

// AdminDeleteBgProject handles DELETE /api/bg/projects/:id
func AdminDeleteBgProject(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		common.ApiErrorMsg(c, "project_id is required")
		return
	}

	if err := model.DeleteBgProject(projectID); err != nil {
		common.ApiErrorMsg(c, "Failed to delete project: "+err.Error())
		return
	}

	common.ApiSuccess(c, nil)
}
