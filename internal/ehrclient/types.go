package ehrclient

// EHR OpenAPI 响应数据模型（按 OpenAPI 规范定义）

type ListOrganizationReply struct {
	Items []Organization `json:"items"`
}

type Organization struct {
	OrgID          string `json:"orgId"`
	OrgName        string `json:"orgName"`
	OrgCode        string `json:"orgCode"`
	CreateDate     string `json:"createDate"`
	LastModifyDate string `json:"lastModifyDate"`
}

type ListCompanyReply struct {
	Items []Company `json:"items"`
}

type Company struct {
	CompanyID      string `json:"companyId"`
	CompanyCode    string `json:"companyCode"`
	CompanyName    string `json:"companyName"`
	ParentID       string `json:"parentId"`
	LastModifyDate string `json:"lastModifyDate"`
}

type ListDepartmentReply struct {
	Items []Department `json:"items"`
}

type Department struct {
	DepartmentID         string `json:"departmentId"`
	DepartmentCode       string `json:"departmentCode"`
	DepartmentName       string `json:"departmentName"`
	DepartmentType       int32  `json:"departmentType"`
	DepartmentTypeName   string `json:"departmentTypeName"`
	ParentID             string `json:"parentId"`
	LastModifyDate       string `json:"lastModifyDate"`
	LeaderName           string `json:"leaderName"`
	LeaderPassport       string `json:"leaderPassport"`
	LeaderPersonID       string `json:"leaderPersonId"`
	LeaderEmployeeID     string `json:"leaderEmployeeId"`
	ManagerName          string `json:"managerName"`
	ManagerPassport      string `json:"managerPassport"`
	ManagerPersonID      string `json:"managerPersonId"`
	ManagerEmployeeID    string `json:"managerEmployeeId"`
	HrBpName             string `json:"hrBpName"`
	HrBpPassport         string `json:"hrBpPassport"`
	HrBpPersonID         string `json:"hrBpPersonId"`
	HrBpEmployeeID       string `json:"hrBpEmployeeId"`
	FinanceBpName        string `json:"financeBpName"`
	FinanceBpPassport    string `json:"financeBpPassport"`
	FinanceBpPersonID    string `json:"financeBpPersonId"`
	FinanceBpEmployeeID  string `json:"financeBpEmployeeId"`
	LegalBpName          string `json:"legalBpName"`
	LegalBpPassport      string `json:"legalBpPassport"`
	LegalBpPersonID      string `json:"legalBpPersonId"`
	LegalBpEmployeeID    string `json:"legalBpEmployeeId"`
	LegalSecondBpName    string `json:"legalSecondBpName"`
	LegalSecondBpPassport string `json:"legalSecondBpPassport"`
	LegalSecondBpPersonID string `json:"legalSecondBpPersonId"`
	LegalSecondBpEmployeeID string `json:"legalSecondBpEmployeeId"`
	AuditBpName          string `json:"auditBpName"`
	AuditBpPassport      string `json:"auditBpPassport"`
	AuditBpPersonID      string `json:"auditBpPersonId"`
	AuditBpEmployeeID    string `json:"auditBpEmployeeId"`
	RecruitHrName        string `json:"recruitHrName"`
	RecruitHrPassport    string `json:"recruitHrPassport"`
	RecruitHrPersonID    string `json:"recruitHrPersonId"`
	RecruitHrEmployeeID  string `json:"recruitHrEmployeeId"`
	RecruitManagerName   string `json:"recruitManagerName"`
	RecruitManagerPassport string `json:"recruitManagerPassport"`
	RecruitManagerPersonID string `json:"recruitManagerPersonId"`
	RecruitManagerEmployeeID string `json:"recruitManagerEmployeeId"`
}

type ListEmployeeReply struct {
	Items []Employee `json:"items"`
}

type Employee struct {
	EmployeeID         string          `json:"employeeId"`
	PersonID           string          `json:"personId"`
	Passport           string          `json:"passport"`
	AppointName        string          `json:"appointName"`
	AppointCode        string          `json:"appointCode"`
	Office             string          `json:"office"`
	Email              string          `json:"email"`
	PersonStatus       int32           `json:"personStatus"`
	PersonType         int32           `json:"personType"`
	IsDefault          bool            `json:"isDefault"`
	CompanyID          string          `json:"companyId"`
	CompanyName        string          `json:"companyName"`
	DepartmentID       string          `json:"departmentId"`
	DepartmentName     string          `json:"departmentName"`
	Ranks              []EmployeeRank  `json:"ranks"`
	PositionID         string          `json:"positionId"`
	PositionName       string          `json:"positionName"`
	PositionCategoryID string          `json:"positionCategoryId"`
	ManagerEmployeeID  string          `json:"managerEmployeeId"`
	ManagerPersonID    string          `json:"managerPersonId"`
	ManagerPassport    string          `json:"managerPassport"`
	ManagerName        string          `json:"managerName"`
	PersonName         string          `json:"personName"`
	GroupENumber       string          `json:"groupENumber"`
	Gender             int32           `json:"gender"`
	JoinDate           string          `json:"joinDate"`
	OnBoardDate        string          `json:"onBoardDate"`
	Location           string          `json:"location"`
	DismissDate        string          `json:"dismissDate"`
	LastModifyDate     string          `json:"lastModifyDate"`
}

type EmployeeRank struct {
	RanksID   string `json:"ranksId"`
	RanksName string `json:"ranksName"`
	RanksType int32  `json:"ranksType"`
}

type ListPositionReply struct {
	Items []PositionNode `json:"items"`
}

// PositionTree 节点
type PositionNode struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     int32          `json:"type"`
	NamePath string         `json:"namePath"`
	Items    []PositionNode `json:"items"`
}
