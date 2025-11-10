package model

type SystemConfig struct {
	Key         string `gorm:"primaryKey;column:key;type:varchar(100)" json:"key"`
	Value       string `gorm:"column:value;type:text" json:"value"`
	Encrypted   bool   `gorm:"column:encrypted" json:"encrypted"`
	Description string `gorm:"column:description;type:varchar(500)" json:"description"`
	CreatedAt   string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   string `gorm:"column:updated_at" json:"updated_at"`
}

func (SystemConfig) TableName() string {
	return "system_configs"
}

type SystemSettings struct {
	SiteName     string `json:"site_name"`
	SiteLogo     string `json:"site_logo"`
	SiteSubtitle string `json:"site_subtitle"`
	APIBaseURL   string `json:"api_base_url"`
}

type PublicSettings struct {
	SiteName     string `json:"site_name"`
	SiteLogo     string `json:"site_logo"`
	SiteSubtitle string `json:"site_subtitle"`
	APIBaseURL   string `json:"api_base_url"`
}