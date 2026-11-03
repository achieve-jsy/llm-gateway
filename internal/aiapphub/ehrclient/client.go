package ehrclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EHR 接口中心的错误分类。同步服务靠它们区分"没权限"和"上游故障"，
// 前端也据此提示"接口中心未授权该路由"。
var (
	ErrUnauthorized = errors.New("ehr unauthorized")
	ErrUpstream     = errors.New("ehr upstream")
	ErrEmptyResult  = errors.New("ehr empty result")
)

// Config EHR 接口中心连接参数。
//
// Prefix 必须是完整的路由前缀（/open-apis/ehr-apic/open-apis/resource/v1），
// 少一层路径会返回 403 ROUTE_NOT_ALLOWED。
type Config struct {
	BaseURL string
	Prefix  string
	Token   string
	OrgID   string
	Timeout time.Duration
}

// missingFields 返回构造请求所必需但为空字段。
func (c Config) missingFields() []string {
	missing := make([]string, 0, 4)
	if c.BaseURL == "" {
		missing = append(missing, "BaseURL")
	}
	if c.Prefix == "" {
		missing = append(missing, "Prefix")
	}
	if c.Token == "" {
		missing = append(missing, "Token")
	}
	if c.OrgID == "" {
		missing = append(missing, "OrgID")
	}
	return missing
}

// Department EHR 部门节点。ID 是 departmentId（字符串，不是 uuid）。
//
// 节点上带 9 类岗位的人（负责人/主管/各 BP）。EHR 没有"列出全部人员"的授权路由，
// 人员档案只能从这些字段里捡到 employeeId 再回查，见 EmployeeIDs。
type Department struct {
	ID                       string `json:"departmentId"`
	Name                     string `json:"departmentName"`
	ParentID                 string `json:"parentId"`
	Path                     string `json:"departmentPath"`
	LeaderName               string `json:"leaderName"`
	LeaderPassport           string `json:"leaderPassport"`
	LeaderEmployeeID         string `json:"leaderEmployeeId"`
	ManagerEmployeeID        string `json:"managerEmployeeId"`
	HrBpEmployeeID           string `json:"hrBpEmployeeId"`
	FinanceBpEmployeeID      string `json:"financeBpEmployeeId"`
	LegalBpEmployeeID        string `json:"legalBpEmployeeId"`
	LegalSecondBpEmployeeID  string `json:"legalSecondBpEmployeeId"`
	AuditBpEmployeeID        string `json:"auditBpEmployeeId"`
	RecruitHrEmployeeID      string `json:"recruitHrEmployeeId"`
	RecruitManagerEmployeeID string `json:"recruitManagerEmployeeId"`
	Type                     int    `json:"departmentType"`
}

// EmployeeIDs 汇总节点上出现过的 employeeId（去重、去空）。
func (d Department) EmployeeIDs() []string {
	return nonEmptyUnique([]string{
		d.LeaderEmployeeID, d.ManagerEmployeeID, d.HrBpEmployeeID, d.FinanceBpEmployeeID,
		d.LegalBpEmployeeID, d.LegalSecondBpEmployeeID, d.AuditBpEmployeeID,
		d.RecruitHrEmployeeID, d.RecruitManagerEmployeeID,
	})
}

// Employee EHR 人员档案。时间字段上游是毫秒时间戳（数字），本地 ehr_employees 存字符串，
// 因此这里用 int64 接收、落库时格式化。
type Employee struct {
	OrgID             string `json:"-"` // 由 client 按配置回填，上游不返回
	EmployeeID        string `json:"employeeId"`
	PersonID          string `json:"personId"`
	Passport          string `json:"passport"`
	PersonName        string `json:"personName"`
	AppointName       string `json:"appointName"`
	AppointCode       string `json:"appointCode"`
	Office            string `json:"office"`
	Email             string `json:"email"`
	PersonStatus      int32  `json:"personStatus"`
	PersonType        int32  `json:"personType"`
	Gender            int32  `json:"gender"`
	IsDefault         bool   `json:"isDefault"`
	DepartmentID      string `json:"departmentId"`
	DepartmentName    string `json:"departmentName"`
	ManagerEmployeeID string `json:"managerEmployeeId"`
	ManagerPersonID   string `json:"managerPersonId"`
	ManagerPassport   string `json:"managerPassport"`
	ManagerName       string `json:"managerName"`
	GroupENumber      string `json:"groupENumber"`
	JoinDate          int64  `json:"joinDate"`
	OnBoardDate       int64  `json:"onBoardDate"`
	Location          string `json:"location"`
	LastModifyDate    int64  `json:"lastModifyDate"`
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: timeout}}
}

// FetchDepartments 拉取组织树全量节点（约 100 条）。EHR 没有单节点接口，
// 所有同步范围都从这一次结果里过滤。
func (c *Client) FetchDepartments(ctx context.Context) ([]Department, error) {
	var payload struct {
		Items []Department `json:"items"`
	}
	if err := c.getJSON(ctx, "/departments", url.Values{"orgId": {c.cfg.OrgID}}, &payload); err != nil {
		return nil, err
	}
	if len(payload.Items) == 0 {
		return nil, ErrEmptyResult
	}
	return payload.Items, nil
}

// employeeBatchSize 单次 batch 请求带的 id 数。实测 10 个稳定；
// 再多会把 URL 撑长，且没有任何上限文档可依。
const employeeBatchSize = 10

// FetchEmployees 按 employeeId 批量拉人员档案。
//
// EHR 只授权了 /employees/batch（按 id 查），没有"列全部人员"的路由，
// 所以调用方必须先拿到 id（见 Department.EmployeeIDs）。
// 某一块返回空是正常现象——离职或失效的 id 查不到，不当成错误。
func (c *Client) FetchEmployees(ctx context.Context, employeeIDs []string) ([]Employee, error) {
	return c.fetchEmployeesByKey(ctx, "employeeIds", employeeIDs)
}

// FetchEmployeesByGroupENumber 按工号（BF00014 这种）批量查档案。
//
// 工号就是本地 users.employee_id，所以这条能把"系统里已有的每一个人"反查成完整档案——
// 在没有枚举路由的前提下，这是覆盖全量的唯一办法。
func (c *Client) FetchEmployeesByGroupENumber(ctx context.Context, numbers []string) ([]Employee, error) {
	return c.fetchEmployeesByKey(ctx, "groupENumbers", numbers)
}

// FetchEmployeesByPassport 按 EHR 账号（本地 users.sub）批量查档案。
func (c *Client) FetchEmployeesByPassport(ctx context.Context, passports []string) ([]Employee, error) {
	return c.fetchEmployeesByKey(ctx, "passports", passports)
}

// fetchEmployeesByKey 唯一的批量查询实现：key 只能是 employeeIds / groupENumbers / passports。
func (c *Client) fetchEmployeesByKey(ctx context.Context, key string, values []string) ([]Employee, error) {
	ids := nonEmptyUnique(values)
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]Employee, 0, len(ids))
	for start := 0; start < len(ids); start += employeeBatchSize {
		end := start + employeeBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		q := url.Values{"orgId": {c.cfg.OrgID}}
		for _, id := range ids[start:end] {
			q.Add(key, id) // 必须重复同名参数，逗号分隔上游不认
		}
		var payload struct {
			Items []Employee `json:"items"`
		}
		if err := c.getJSON(ctx, "/employees/batch", q, &payload); err != nil {
			return nil, err
		}
		for _, e := range payload.Items {
			e.OrgID = c.cfg.OrgID
			out = append(out, e)
		}
	}
	return out, nil
}

// getJSON 统一发 GET 并把非 200 归类成 ErrUnauthorized（401/403）/ ErrUpstream。
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	// 少任何一项都会变成一个没人看得懂的 404/403，所以在发请求之前先明确报出来。
	if missing := c.cfg.missingFields(); len(missing) > 0 {
		return fmt.Errorf("%w: 配置不完整，缺少 %s", ErrUpstream, strings.Join(missing, ", "))
	}
	endpoint := c.cfg.BaseURL + c.cfg.Prefix + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", ErrUpstream, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: status %d: %s", ErrUnauthorized, resp.StatusCode, snippet(body))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d: %s", ErrUpstream, resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	return nil
}

// nonEmptyUnique 去空去重，保留首次出现顺序（便于日志与分块稳定）。
func nonEmptyUnique(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max])
	}
	return string(b)
}
