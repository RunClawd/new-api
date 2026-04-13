package controller

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// resolveProjectID resolves the effective project ID for a BaseGate request.
//
// Returns (projectID int, err error):
//   - header absent       → (0, nil)      — no project scope
//   - header present+valid → (resolved, nil)
//   - header present+invalid → (0, error)  — caller should 400
//
// All paths enforce org ownership (project.OrgID == current user).
// Returns int (not int64) to match CanonicalRequest.ProjectID.
// Truncation from int64 is intentional: project counts will not overflow int.
func resolveProjectID(c *gin.Context) (int, error) {
	orgID := c.GetInt("id")

	// 1. Token binding takes absolute precedence
	if bound, exists := c.Get("bg_bound_project_id"); exists {
		boundID := bound.(int64)

		// v7: Audit mismatch — compare using both public string and numeric fallback
		//     to avoid false positives during backward-compat period.
		if headerVal := c.GetHeader("X-Project-Id"); headerVal != "" {
			headerInternalID := resolveHeaderToInternalID(headerVal)
			if headerInternalID != boundID {
				_ = model.RecordBgAuditLog(orgID, "", "", "project_binding_mismatch", map[string]interface{}{
					"bound_project_id": boundID,
					"header_value":     headerVal,
				})
			}
		}
		return int(boundID), nil
	}

	// 2. No header → no project scope (valid)
	headerVal := c.GetHeader("X-Project-Id")
	if headerVal == "" {
		return 0, nil
	}

	// 3. Try public identifier (e.g. "proj_xxxx")
	project, err := model.GetBgProjectByProjectID(headerVal)
	if err == nil {
		// Enforce org ownership
		if project.OrgID != orgID {
			return 0, fmt.Errorf("project %s does not belong to current org", headerVal)
		}
		return int(project.ID), nil
	}

	// 4. Backward-compat: numeric string, with org ownership check
	if numID, parseErr := strconv.Atoi(headerVal); parseErr == nil {
		proj, dbErr := model.GetBgProjectByID(int64(numID))
		if dbErr == nil && proj.OrgID == orgID {
			return numID, nil
		}
		return 0, fmt.Errorf("project %s not found or does not belong to current org", headerVal)
	}

	// 5. Header present but unparseable → error (not silent 0)
	return 0, fmt.Errorf("invalid X-Project-Id: %s", headerVal)
}

// resolveHeaderToInternalID is a best-effort helper for audit comparison only.
// Returns the internal project ID if resolvable, otherwise -1.
func resolveHeaderToInternalID(headerVal string) int64 {
	// Try public identifier first
	project, err := model.GetBgProjectByProjectID(headerVal)
	if err == nil {
		return project.ID
	}
	// Try numeric
	if numID, err := strconv.Atoi(headerVal); err == nil {
		return int64(numID)
	}
	return -1
}
