package ehrclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// Department EHR 部门节点。ID 是 departmentId（字符串，不是 uuid）。
type Department struct {
	ID               string `json:"departmentId"`
	Name             string `json:"departmentName"`
	ParentID         string `json:"parentId"`
	Path             string `json:"departmentPath"`
	LeaderName       string `json:"leaderName"`
	LeaderPassport   string `json:"leaderPassport"`
	LeaderEmployeeID string `json:"leaderEmployeeId"`
	Type             int    `json:"departmentType"`
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

// FetchDepartments 拉取组织树全量节点（约 99 条）。EHR 没有单节点接口，
// 所有同步范围都从这一次结果里过滤。
func (c *Client) FetchDepartments(ctx context.Context) ([]Department, error) {
	endpoint := c.cfg.BaseURL + c.cfg.Prefix + "/departments?orgId=" + url.QueryEscape(c.cfg.OrgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrUpstream, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: status %d: %s", ErrUnauthorized, resp.StatusCode, snippet(body))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrUpstream, resp.StatusCode, snippet(body))
	}

	var payload struct {
		Items []Department `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	if len(payload.Items) == 0 {
		return nil, ErrEmptyResult
	}
	return payload.Items, nil
}

func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max])
	}
	return string(b)
}
