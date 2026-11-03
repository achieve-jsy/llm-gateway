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
