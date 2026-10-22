//go:build integration

// PG 集成测试：验证 user_daily_stats 日聚合 SQL 在真实 PostgreSQL 上可执行，
// 且「把集合返回函数移出聚合入参」的改写不影响任何数值口径。
//
// 运行前提：有可用的 PostgreSQL（请勿指向生产库）。本用例只创建 TEMP 表并在事务内回滚，
// 不会产生持久化对象，也不会读写任何业务表。
//
// 运行方式：
//   DATABASE_URL="host=127.0.0.1 port=5432 dbname=postgres sslmode=disable" //   go test ./internal/observability/service/... -tags=integration -run Rollup -v -count=1

package service

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pgRollupDDL 只建 TEMP 表；列取自 atlas-migrations 的 call_logs / departments / dept_studio_map / user_daily_stats
var pgRollupDDL = []string{
	`CREATE TEMP TABLE call_logs (
		request_id text PRIMARY KEY,
		ts timestamptz NOT NULL,
		user_id text,
		dept_id text,
		model text NOT NULL,
		prompt_tokens bigint NOT NULL DEFAULT 0,
		cached_tokens bigint NOT NULL DEFAULT 0,
		completion_tokens bigint NOT NULL DEFAULT 0,
		reasoning_tokens bigint NOT NULL DEFAULT 0,
		status bigint,
		est_cost_usd numeric(18,6) NOT NULL DEFAULT 0,
		key_type varchar(16) NOT NULL DEFAULT 'personal'
	)`,
	`CREATE TEMP TABLE departments (id uuid NOT NULL, dept_id varchar(64) NOT NULL, dept_name varchar(128) NOT NULL)`,
	`CREATE TEMP TABLE dept_studio_map (dept_id varchar(64) PRIMARY KEY, studio_id text NOT NULL)`,
	`CREATE TEMP TABLE user_daily_stats (
		stat_date DATE NOT NULL,
		user_id TEXT NOT NULL,
		studio_id TEXT NOT NULL DEFAULT '',
		dept_id TEXT NOT NULL DEFAULT '',
		req_count INT NOT NULL DEFAULT 0,
		success_count INT NOT NULL DEFAULT 0,
		error_count INT NOT NULL DEFAULT 0,
		model_count INT NOT NULL DEFAULT 0,
		prompt_tokens BIGINT NOT NULL DEFAULT 0,
		cached_tokens BIGINT NOT NULL DEFAULT 0,
		completion_tokens BIGINT NOT NULL DEFAULT 0,
		reasoning_tokens BIGINT NOT NULL DEFAULT 0,
		est_cost_usd DECIMAL(18,6) NOT NULL DEFAULT 0,
		hourly_distribution JSONB NOT NULL DEFAULT '{}',
		PRIMARY KEY (stat_date, user_id)
	)`,
}

// pgRollupFixture 覆盖：跨小时同一模型只算一次、service key 排除、其他日期排除、
// 空模型名计为一个模型、user_id 为 NULL 归到 'unknown'。
var pgRollupFixture = []string{
	`INSERT INTO departments VALUES
		('11111111-1111-1111-1111-111111111111','D1','dept one'),
		('22222222-2222-2222-2222-222222222222','D2','dept two')`,
	`INSERT INTO dept_studio_map VALUES ('D1','S1'), ('D2','S2')`,
	`INSERT INTO call_logs (request_id, ts, user_id, dept_id, model, prompt_tokens, cached_tokens, completion_tokens, reasoning_tokens, status, est_cost_usd, key_type) VALUES
		('r01','2026-06-28 01:10:00+08','u1','D1','gpt-a',100,10,20,5,200,1.5,'personal'),
		('r02','2026-06-28 01:20:00+08','u1','D1','gpt-b',200,20,40,10,200,2.5,'personal'),
		('r03','2026-06-28 07:30:00+08','u1','D1','gpt-b',300,30,60,15,500,3.5,'personal'),
		('r04','2026-06-28 07:40:00+08','u1','D1','gpt-c',400,40,80,20,500,4.5,'personal'),
		('r05','2026-06-28 04:00:00+08','u2','D1','gpt-svc',999,99,99,99,200,9.9,'service'),
		('r06','2026-06-28 05:00:00+08','u3','D1','',10,1,2,1,200,0.1,'personal'),
		('r07','2026-06-28 23:05:00+08',NULL,'D2','gpt-a',50,5,10,2,200,0.5,'personal'),
		('r08','2026-06-27 05:00:00+08','u4','D1','gpt-a',1,1,1,1,200,0.1,'personal')`,
}

// TestRollupUserDailyStatsAggregateOnPostgres 在真实 PG 上验证：
//  1. 旧写法 cardinality(array_agg(DISTINCT unnest(...))) 复现 SQLSTATE 0A000；
//  2. 新 SQL 可执行，且连跑两次（第二次走 ON CONFLICT DO UPDATE）结果幂等；
//  3. 各列数值与预期口径一致（含跨小时模型去重、JSONB 小时分布、过滤条件）。
func TestRollupUserDailyStatsAggregateOnPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL 未设置，跳过 PG 集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("连接 PG 失败: %v", err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启事务失败: %v", tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()

	if err := tx.Exec("SET TIME ZONE 'Asia/Shanghai'").Error; err != nil {
		t.Fatalf("设置会话时区失败: %v", err)
	}
	for _, ddl := range pgRollupDDL {
		if err := tx.Exec(ddl).Error; err != nil {
			t.Fatalf("建 TEMP 表失败: %v\nSQL: %s", err, ddl)
		}
	}
	for _, ins := range pgRollupFixture {
		if err := tx.Exec(ins).Error; err != nil {
			t.Fatalf("插入测试数据失败: %v\nSQL: %s", err, ins)
		}
	}

	// 1) 旧写法必须被 PG 拒绝（复现生产日志里的 SQLSTATE 0A000）
	//    出错会让事务进入 aborted 状态，因此放在 SAVEPOINT 里执行再回滚到该点。
	if err := tx.SavePoint("sp_legacy").Error; err != nil {
		t.Fatalf("设置 SAVEPOINT 失败: %v", err)
	}
	legacyErr := tx.Exec("SELECT cardinality(array_agg(DISTINCT unnest(model_list))) FROM (" +
		"SELECT array_agg(DISTINCT model) AS model_list FROM call_logs GROUP BY user_id) sub").Error
	if err := tx.RollbackTo("sp_legacy").Error; err != nil {
		t.Fatalf("回滚到 SAVEPOINT 失败: %v", err)
	}
	if legacyErr == nil || !strings.Contains(legacyErr.Error(), "set-returning function") {
		t.Fatalf("旧写法应报 set-returning function 错误（SQLSTATE 0A000），实际: %v", legacyErr)
	}
	t.Logf("旧写法在 PG 上的报错（复现生产）: %v", legacyErr)

	// 2) 新 SQL 连跑两次
	statDate := time.Date(2026, 6, 28, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for i := 1; i <= 2; i++ {
		if err := tx.Exec(buildUserDailyStatsAggregateSQL(), statDate, statDate, statDate).Error; err != nil {
			t.Fatalf("第 %d 次执行 user_daily_stats 聚合失败: %v", i, err)
		}
	}

	// 3) 校验数值
	type row struct {
		UserID       string `gorm:"column:user_id"`
		StudioID     string `gorm:"column:studio_id"`
		DeptID       string `gorm:"column:dept_id"`
		ReqCount     int    `gorm:"column:req_count"`
		SuccessCount int    `gorm:"column:success_count"`
		ErrorCount   int    `gorm:"column:error_count"`
		ModelCount   int    `gorm:"column:model_count"`
		PromptTokens int64  `gorm:"column:prompt_tokens"`
		EstCostUSD   string `gorm:"column:est_cost_usd"`
		Hourly       string `gorm:"column:hourly"`
	}
	var rows []row
	if err := tx.Raw("SELECT user_id, studio_id, dept_id, req_count, success_count, error_count, " +
		"model_count, prompt_tokens, est_cost_usd::text AS est_cost_usd, " +
		"hourly_distribution::text AS hourly FROM user_daily_stats ORDER BY user_id").Scan(&rows).Error; err != nil {
		t.Fatalf("查询 user_daily_stats 失败: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("结果行数=%d，期望 3（service key 与其他日期必须被排除）: %+v", len(rows), rows)
	}
	got := make(map[string]row, len(rows))
	for _, r := range rows {
		got[r.UserID] = r
	}

	u1, ok := got["u1"]
	if !ok {
		t.Fatalf("缺少 u1 行: %+v", rows)
	}
	if u1.StudioID != "S1" || u1.DeptID != "D1" {
		t.Errorf("u1 studio/dept=%s/%s，期望 S1/D1", u1.StudioID, u1.DeptID)
	}
	if u1.ReqCount != 4 || u1.SuccessCount != 2 || u1.ErrorCount != 2 {
		t.Errorf("u1 req/success/error=%d/%d/%d，期望 4/2/2", u1.ReqCount, u1.SuccessCount, u1.ErrorCount)
	}
	if u1.ModelCount != 3 {
		t.Errorf("u1 model_count=%d，期望 3（hour1 与 hour7 的 gpt-b 只能算一次）", u1.ModelCount)
	}
	if u1.PromptTokens != 1000 {
		t.Errorf("u1 prompt_tokens=%d，期望 1000", u1.PromptTokens)
	}
	if u1.EstCostUSD != "12.000000" {
		t.Errorf("u1 est_cost_usd=%s，期望 12.000000", u1.EstCostUSD)
	}
	var u1Hourly map[string]int
	if err := json.Unmarshal([]byte(u1.Hourly), &u1Hourly); err != nil {
		t.Fatalf("解析 u1 hourly_distribution=%s 失败: %v", u1.Hourly, err)
	}
	if len(u1Hourly) != 2 || u1Hourly["1"] != 2 || u1Hourly["7"] != 2 {
		t.Errorf("u1 hourly_distribution=%v，期望 {1:2, 7:2}", u1Hourly)
	}

	u3, ok := got["u3"]
	if !ok {
		t.Fatalf("缺少 u3 行: %+v", rows)
	}
	if u3.ReqCount != 1 || u3.ModelCount != 1 {
		t.Errorf("u3 req/model=%d/%d，期望 1/1（空模型名按一个模型计入）", u3.ReqCount, u3.ModelCount)
	}

	unknown, ok := got["unknown"]
	if !ok {
		t.Fatalf("缺少 user_id 为 NULL 归并出的 unknown 行: %+v", rows)
	}
	if unknown.StudioID != "S2" || unknown.DeptID != "D2" || unknown.ModelCount != 1 {
		t.Errorf("unknown studio/dept/model=%s/%s/%d，期望 S2/D2/1", unknown.StudioID, unknown.DeptID, unknown.ModelCount)
	}

	if _, ok := got["u2"]; ok {
		t.Errorf("key_type='service' 的 u2 不应出现在结果中")
	}
	if _, ok := got["u4"]; ok {
		t.Errorf("非统计日期的 u4 不应出现在结果中")
	}
}
