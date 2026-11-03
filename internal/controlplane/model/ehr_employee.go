package model

import (
	"time"

	"github.com/google/uuid"
)

// EHREmployee EHR 人员档案镜像（表 ehr_employees，由迁移 057 建立、后续迁移补列）。
//
// 为什么只有这些人：EHR 只授权了 /employees/batch（按 id 查），没有"列全部人员"的路由，
// 清单是从部门节点的 9 类岗位里捡 id 回查出来的。所以这是"被点名的人"的档案，
// 不是全组织花名册。字段取值见 ehrclient.Employee。
//
// join_date / on_board_date / ehr_modify_date 上游是毫秒时间戳，线上列是 varchar，
// 这里原样存字符串，同步链路不做时区转换。
//
// ponytail: 这张表是 legacy migrations/057 建的，没有纳入 atlas（atlas-migrations 里没有它），
// 所以也没把它加进 tools/atlas-loader —— 一旦纳入，atlas 会按这里的字段子集去 diff 线上表，
// 反而想 DROP 掉 position_*/dismiss_date 等线上已有列。代价：全新环境从 atlas 迁移起库时
// 不会有这张表（人员同步只告警、组织树照常）。要正式纳入时，先把 057 全量列搬到 atlas 基线再加载模型。
type EHREmployee struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID      string    `gorm:"column:org_id;type:varchar(64);not null;uniqueIndex:uniq_ehr_employees_org_employee,priority:1" json:"org_id"`
	EmployeeID string    `gorm:"column:employee_id;type:varchar(64);not null;uniqueIndex:uniq_ehr_employees_org_employee,priority:2" json:"employee_id"`
	PersonID   string    `gorm:"column:person_id;type:varchar(64);not null;default:''" json:"person_id"`
	Passport   string    `gorm:"column:passport;type:varchar(128);not null;default:''" json:"passport"`
	// PersonName 姓名；GroupENumber 是工号（BF00014 这种），能与 users.employee_id 对上。
	PersonName   string `gorm:"column:person_name;type:varchar(128);not null;default:''" json:"person_name"`
	GroupENumber string `gorm:"column:group_e_number;type:varchar(64);not null;default:''" json:"group_e_number"`
	Email        string `gorm:"column:email;type:varchar(256);not null;default:''" json:"email"`
	// Office 是职务名（项目管理/副总裁），不是办公地点；办公地点在 Location。
	Office         string `gorm:"column:office;type:varchar(128);not null;default:''" json:"office"`
	AppointName    string `gorm:"column:appoint_name;type:varchar(255);not null;default:''" json:"appoint_name"`
	AppointCode    string `gorm:"column:appoint_code;type:varchar(255);not null;default:''" json:"appoint_code"`
	DepartmentID   string `gorm:"column:department_id;type:varchar(64);not null;default:''" json:"department_id"`
	DepartmentName string `gorm:"column:department_name;type:varchar(255);not null;default:''" json:"department_name"`
	// PersonStatus 1=在职；查不到档案的 id（离职/失效）根本不会出现在结果里。
	PersonStatus int32 `gorm:"column:person_status;not null;default:0" json:"person_status"`
	PersonType   int32 `gorm:"column:person_type;not null;default:0" json:"person_type"`
	Gender       int32 `gorm:"column:gender;not null;default:0" json:"gender"`
	IsDefault    bool  `gorm:"column:is_default;not null;default:false" json:"is_default"`
	// Location 办公地点（浙数文化科技园=杭州 / 广州），区分地域就看它。
	Location          string    `gorm:"column:location;type:varchar(128);not null;default:''" json:"location"`
	ManagerEmployeeID string    `gorm:"column:manager_employee_id;type:varchar(64);not null;default:''" json:"manager_employee_id"`
	ManagerPersonID   string    `gorm:"column:manager_person_id;type:varchar(64);not null;default:''" json:"manager_person_id"`
	ManagerPassport   string    `gorm:"column:manager_passport;type:varchar(128);not null;default:''" json:"manager_passport"`
	ManagerName       string    `gorm:"column:manager_name;type:varchar(128);not null;default:''" json:"manager_name"`
	JoinDate          string    `gorm:"column:join_date;type:varchar(64);not null;default:''" json:"join_date"`
	OnBoardDate       string    `gorm:"column:on_board_date;type:varchar(64);not null;default:''" json:"on_board_date"`
	EHRModifyDate     string    `gorm:"column:ehr_modify_date;type:varchar(64);not null;default:''" json:"ehr_modify_date"`
	SyncedAt          time.Time `gorm:"column:synced_at;not null;default:now()" json:"synced_at"`
	CreatedAt         time.Time `gorm:"column:created_at;not null;default:now()" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null;default:now()" json:"updated_at"`
}

func (EHREmployee) TableName() string { return "ehr_employees" }
