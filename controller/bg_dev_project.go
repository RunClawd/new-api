package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxProjectsPerOrg = 20

// DevListBgProjects handles GET /api/bg/dev/projects
// Returns a paginated list of projects for the current user's org.
func DevListBgProjects(c *gin.Context) {
	orgID := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)

	projects, total, err := model.ListBgProjectsByOrgID(orgID, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiErrorMsg(c, "failed to list projects: "+err.Error())
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(projects)
	common.ApiSuccess(c, pageInfo)
}

// DevCreateBgProject handles POST /api/bg/dev/projects
// Creates a new project, enforcing per-org project limits.
func DevCreateBgProject(c *gin.Context) {
	orgID := c.GetInt("id")

	// Check per-org project limit
	count, err := model.CountBgProjectsByOrgID(orgID)
	if err != nil {
		common.ApiErrorMsg(c, "failed to check project count")
		return
	}
	if count >= maxProjectsPerOrg {
		common.ApiErrorMsg(c, fmt.Sprintf("project limit reached (max %d)", maxProjectsPerOrg))
		return
	}

	var project model.BgProject
	if err := c.ShouldBindJSON(&project); err != nil {
		common.ApiErrorMsg(c, "invalid request: "+err.Error())
		return
	}

	// Force OrgID to current user
	project.OrgID = orgID

	if err := model.CreateBgProject(&project); err != nil {
		common.ApiErrorMsg(c, "failed to create project: "+err.Error())
		return
	}

	common.ApiSuccess(c, project)
}
