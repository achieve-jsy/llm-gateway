package ehrclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/stretchr/testify/require"
)

const itemsJSON = "{\"items\":[{\"departmentId\":\"d1\",\"departmentName\":\"AI-Lab\",\"parentId\":\"00000000-0000-0000-0000-000000000000\",\"departmentType\":2,\"leaderName\":\"何治国\",\"leaderPassport\":\"hezhiguo\",\"leaderEmployeeId\":\"e1\"}]}"

func newTestClient(baseURL string) *ehrclient.Client {
	return ehrclient.New(ehrclient.Config{
		BaseURL: baseURL,
		Prefix:  "/open-apis/ehr-apic/open-apis/resource/v1",
		Token:   "tok",
		OrgID:   "1428195512246472704",
	})
}

func TestFetchDepartmentsParsesItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/open-apis/ehr-apic/open-apis/resource/v1/departments", r.URL.Path)
		require.Equal(t, "1428195512246472704", r.URL.Query().Get("orgId"))
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		io.WriteString(w, itemsJSON)
	}))
	defer srv.Close()

	got, err := newTestClient(srv.URL).FetchDepartments(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "d1", got[0].ID)
	require.Equal(t, "AI-Lab", got[0].Name)
	require.Equal(t, 2, got[0].Type)
	require.Equal(t, "hezhiguo", got[0].LeaderPassport)
	require.Equal(t, "e1", got[0].LeaderEmployeeID)
}

func TestFetchDepartmentsErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"forbidden", http.StatusForbidden, "{\"code\":\"ROUTE_NOT_ALLOWED\"}", ehrclient.ErrUnauthorized},
		{"unauthorized", http.StatusUnauthorized, "", ehrclient.ErrUnauthorized},
		{"server error", http.StatusInternalServerError, "boom", ehrclient.ErrUpstream},
		{"bad json", http.StatusOK, "not json", ehrclient.ErrUpstream},
		{"empty items", http.StatusOK, "{\"items\":[]}", ehrclient.ErrEmptyResult},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := newTestClient(srv.URL).FetchDepartments(context.Background())
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.want), "got %v, want %v", err, tc.want)
		})
	}
}

// employeeJSON 只用线上真实字段名（personName / groupENumber / location / personStatus），
// 时间戳是毫秒数字——上游就是这么返回的。
const employeeJSON = `{"items":[{"employeeId":"e1","personId":"p1","passport":"zhoutian","personName":"周甜","groupENumber":"BF00014","location":"浙数文化科技园","personStatus":1,"joinDate":1127664000000,"managerName":"何治国"}]}`

func TestFetchEmployeesChunksAndRepeatsIDs(t *testing.T) {
	var calls [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/open-apis/ehr-apic/open-apis/resource/v1/employees/batch", r.URL.Path)
		require.Equal(t, "1428195512246472704", r.URL.Query().Get("orgId"))
		calls = append(calls, r.URL.Query()["employeeIds"])
		io.WriteString(w, employeeJSON)
	}))
	defer srv.Close()

	ids := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		ids = append(ids, "id-"+string(rune('a'+i)))
	}
	got, err := newTestClient(srv.URL).FetchEmployees(context.Background(), ids)
	require.NoError(t, err)

	require.Len(t, calls, 2, "11 个 id 按每批 10 个分两次请求")
	require.Len(t, calls[0], 10)
	require.Len(t, calls[1], 1)
	require.Equal(t, ids[:10], calls[0], "必须重复同名参数，逗号分隔上游不认")
	require.Len(t, got, 2, "每批各返回 1 人")
	require.Equal(t, "1428195512246472704", got[0].OrgID, "orgId 由 client 回填")
	require.Equal(t, "BF00014", got[0].GroupENumber)
	require.Equal(t, int64(1127664000000), got[0].JoinDate)
}

func TestFetchEmployeesToleratesEmptyChunks(t *testing.T) {
	// 离职/失效的 id 查不到档案，返回空 items；这不是错误，不能中断整轮同步。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv.URL).FetchEmployees(context.Background(), []string{"gone"})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestFetchEmployeesSkipsWhenNoIDs(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()

	got, err := newTestClient(srv.URL).FetchEmployees(context.Background(), []string{"", ""})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Zero(t, hits, "没有可查的 id 时不该发请求")
}

func TestDepartmentEmployeeIDsDedupes(t *testing.T) {
	d := ehrclient.Department{
		LeaderEmployeeID:    "e1",
		ManagerEmployeeID:   "e1",
		HrBpEmployeeID:      "e2",
		FinanceBpEmployeeID: "",
	}
	require.Equal(t, []string{"e1", "e2"}, d.EmployeeIDs())
}

func TestFetchDepartmentsRejectsIncompleteConfig(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	c := ehrclient.New(ehrclient.Config{BaseURL: srv.URL, Token: "t", OrgID: "o"}) // 故意不填 Prefix
	_, err := c.FetchDepartments(context.Background())
	require.ErrorIs(t, err, ehrclient.ErrUpstream)
	require.Contains(t, err.Error(), "Prefix")
	require.Zero(t, hits, "配置不完整时不得真的发出请求")
}
