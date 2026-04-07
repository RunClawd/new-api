package model

import "gorm.io/gorm"

// BgProject represents a tenant project in the BaseGate identity model.
// Identity hierarchy: OrgID (=UserID) -> ProjectID -> ApiKeyID.
type BgProject struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ProjectID   string `json:"project_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	OrgID       int    `json:"org_id" gorm:"index;not null;default:0"`
	Name        string `json:"name" gorm:"type:varchar(191);not null"`
	Description string `json:"description" gorm:"type:text"`
	Status      string `json:"status" gorm:"type:varchar(20);default:'active'"` // active | archived
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (BgProject) TableName() string {
	return "bg_projects"
}

// CreateBgProject inserts a new project record.
func CreateBgProject(project *BgProject) error {
	return DB.Create(project).Error
}

// GetBgProjectByProjectID looks up a project by its public project_id string.
func GetBgProjectByProjectID(projectID string) (*BgProject, error) {
	var project BgProject
	err := DB.Where("project_id = ?", projectID).First(&project).Error
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// ListBgProjectsByOrgID returns a paginated list of projects.
// If orgID is 0, all projects are returned (admin view).
func ListBgProjectsByOrgID(orgID, startIdx, num int) (projects []*BgProject, total int64, err error) {
	tx := DB.Model(&BgProject{})
	if orgID > 0 {
		tx = tx.Where("org_id = ?", orgID)
	}

	err = tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = tx.Order("id desc").Offset(startIdx).Limit(num).Find(&projects).Error
	if err != nil {
		return nil, 0, err
	}
	return projects, total, nil
}

// UpdateBgProject updates the Name, Description, and Status fields of a project.
func UpdateBgProject(project *BgProject) error {
	return DB.Model(project).Select("name", "description", "status").Updates(project).Error
}

// DeleteBgProject hard-deletes a project by its public project_id.
func DeleteBgProject(projectID string) error {
	result := DB.Where("project_id = ?", projectID).Delete(&BgProject{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
