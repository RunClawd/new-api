package model

import (
	"fmt"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

type BgBYOCredential struct {
	ID               int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	OrgID            int    `json:"org_id" gorm:"index;not null"`
	Name             string `json:"name" gorm:"type:varchar(100);not null"`
	DisplayName      string `json:"display_name" gorm:"type:varchar(100)"`
	Provider         string `json:"provider" gorm:"type:varchar(50);not null;index"`
	Status           string `json:"status" gorm:"type:varchar(20);default:'active'"`
	CapabilitiesJSON string `json:"capabilities_json" gorm:"type:text"`

	// Encrypted credentials map
	EncryptedData []byte `json:"-" gorm:"not null"`
	Nonce         []byte `json:"-" gorm:"not null"`
	Salt          string `json:"-" gorm:"type:varchar(64);not null"`

	LastUsedAt int64 `json:"last_used_at"`
	CreatedAt  int64 `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  int64 `json:"updated_at" gorm:"autoUpdateTime"`
}

func (BgBYOCredential) TableName() string {
	return "bg_byo_credentials"
}

// Insert creates a new BYO credential record.
func (c *BgBYOCredential) Insert() error {
	return DB.Create(c).Error
}

// Update updates an existing BYO credential record.
func (c *BgBYOCredential) Update() error {
	return DB.Save(c).Error
}

func TouchBgBYOCredentialLastUsed(id int64, ts int64) error {
	return DB.Model(&BgBYOCredential{}).
		Where("id = ?", id).
		Update("last_used_at", ts).Error
}

// Delete deletes the record.
func (c *BgBYOCredential) Delete() error {
	return DB.Delete(c).Error
}

func GetBgBYOCredentialsByOrgID(orgID int) ([]*BgBYOCredential, error) {
	var list []*BgBYOCredential
	err := DB.Where("org_id = ?", orgID).Find(&list).Error
	return list, err
}

func GetBgBYOCredentialByID(id int64) (*BgBYOCredential, error) {
	var cred BgBYOCredential
	err := DB.First(&cred, id).Error
	if err != nil {
		return nil, err
	}
	return &cred, nil
}

// SetPlaintextData encrypts and stores the credential data.
func (c *BgBYOCredential) SetPlaintextData(data map[string]string) error {
	plaintext, err := common.Marshal(data)
	if err != nil {
		return err
	}

	if c.Salt == "" {
		c.Salt = uuid.New().String()
	}

	key, err := common.DeriveBYOKey(c.Salt)
	if err != nil {
		return err
	}

	ciphertext, nonce, err := common.EncryptAESGCM(key, plaintext)
	if err != nil {
		return err
	}

	c.EncryptedData = ciphertext
	c.Nonce = nonce
	return nil
}

// GetPlaintextData decrypts and returns the credential data.
func (c *BgBYOCredential) GetPlaintextData() (map[string]string, error) {
	if c.EncryptedData == nil || c.Nonce == nil || c.Salt == "" {
		return nil, fmt.Errorf("no encrypted data or missing salt found")
	}

	key, err := common.DeriveBYOKey(c.Salt)
	if err != nil {
		return nil, err
	}

	plaintext, err := common.DecryptAESGCM(key, c.Nonce, c.EncryptedData)
	if err != nil {
		return nil, err
	}

	var data map[string]string
	if err := common.Unmarshal(plaintext, &data); err != nil {
		return nil, err
	}

	return data, nil
}

// SupportsCapability checks if this credential is bound to the given capability.
// If CapabilitiesJSON is empty, it means this credential supports all capabilities for its provider.
func (c *BgBYOCredential) SupportsCapability(capability string) bool {
	if c.CapabilitiesJSON == "" || c.CapabilitiesJSON == "null" || c.CapabilitiesJSON == "[]" {
		return true // By default support all if not constrained
	}
	var caps []string
	if err := common.Unmarshal([]byte(c.CapabilitiesJSON), &caps); err != nil {
		return false
	}
	for _, capPattern := range caps {
		matched, _ := filepath.Match(capPattern, capability)
		if matched || capPattern == capability {
			return true
		}
	}
	return false
}
