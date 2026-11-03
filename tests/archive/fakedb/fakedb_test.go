// fakedb_test.go —— 自检：假 DB 必须能真实驱动 GORM 的 Raw/Scan/Exec、ErrRecordNotFound、
// sql.NullString 与 []string 扫描，以及错误注入；否则上层负向测试的结论不可信。
package fakedb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type manifestRow struct {
	DT     time.Time `gorm:"column:dt"`
	State  string    `gorm:"column:state"`
	Verify string    `gorm:"column:verify_state"`
}

func TestFakeDB_DrivesGormRawScanAndExec(t *testing.T) {
	boom := errors.New("injected failure")
	day := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	db := New(func(q string, args []any) (Result, error) {
		switch {
		// 注意顺序：基准 2 的 SQL 同时含 count(*) 与 string_agg，必须先匹配更具体的模式。
		case strings.Contains(q, "string_agg"):
			return Rows([]string{"cnt", "sha"}, int64(3), "abc123"), nil
		case strings.Contains(q, "count(*)") && strings.Contains(q, "call_traces"):
			return Rows([]string{"count"}, int64(7)), nil
		case strings.Contains(q, "FROM trace_archive_manifests"):
			return MultiRows([]string{"dt", "state", "verify_state"},
				[]any{day, "verified", "ok"}), nil
		case strings.Contains(q, "LIMIT") && strings.Contains(q, "FROM \"manifest_rows\""):
			// GORM First 生成的单行查询：返回 0 行 → ErrRecordNotFound
			return Empty("dt", "state"), nil
		case strings.Contains(q, "UPDATE call_traces") && strings.Contains(q, "archive_ref"):
			return Result{RowsAffected: 4}, nil
		case strings.Contains(q, "INSERT INTO trace_archive_manifests"):
			return Err(boom), nil
		default:
			return Result{}, fmt.Errorf("unscripted: %s", q)
		}
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}

	// ① Raw().Scan(&int64)
	var n int64
	if err := gdb.Raw(`SELECT count(*) FROM call_traces WHERE ts >= $1 AND ts < $2`, day, day).Scan(&n).Error; err != nil {
		t.Fatalf("scan count: %v", err)
	}
	if n != 7 {
		t.Fatalf("count = %d want 7", n)
	}

	// ② 多列 + sql.NullString（冻结基准 2 的形状）
	var row struct {
		Cnt int64          `gorm:"column:cnt"`
		SHA sql.NullString `gorm:"column:sha"`
	}
	if err := gdb.Raw(`SELECT count(*) AS cnt, encode(sha256(string_agg(request_id)), 'hex') AS sha FROM call_traces`).Scan(&row).Error; err != nil {
		t.Fatalf("scan freeze: %v", err)
	}
	if row.Cnt != 3 || !row.SHA.Valid || row.SHA.String != "abc123" {
		t.Fatalf("freeze row = %+v", row)
	}

	// ③ Model 扫描（SELECT * 形状）与 time.Time 字段
	var ms []manifestRow
	if err := gdb.Raw(`SELECT dt, state, verify_state FROM trace_archive_manifests WHERE state = $1`, "verified").Scan(&ms).Error; err != nil {
		t.Fatalf("scan manifests: %v", err)
	}
	if len(ms) != 1 || ms[0].State != "verified" || !ms[0].DT.Equal(day) {
		t.Fatalf("manifests = %+v", ms)
	}

	// ④ Exec 的 RowsAffected
	res := gdb.Exec(`UPDATE call_traces SET archive_ref = $1, archive_status = 1 WHERE ts >= $2`, "dt=2026-09-19/", day)
	if res.Error != nil {
		t.Fatalf("exec pointer: %v", res.Error)
	}
	if res.RowsAffected != 4 {
		t.Fatalf("rows affected = %d want 4", res.RowsAffected)
	}

	// ⑤ 错误注入
	if err := gdb.Exec(`INSERT INTO trace_archive_manifests (dt) VALUES ($1)`, day).Error; !errors.Is(err, boom) {
		t.Fatalf("injected error = %v want %v", err, boom)
	}

	// ⑥ First 无行 → ErrRecordNotFound
	if err := gdb.Where("dt = ?", "2026-01-01").First(&manifestRow{}).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("First empty err = %v want ErrRecordNotFound", err)
	}

	// ⑦ 语句审计：能按子串统计（供「指针未被写入」类断言使用）
	if got := db.CountContains("archive_ref"); got != 1 {
		t.Fatalf("archive_ref exec count = %d want 1", got)
	}
	if got := len(db.Statements()); got < 6 {
		t.Fatalf("statements recorded = %d want >= 6", got)
	}
	if got := db.CountContains("INSERT INTO trace_archive_manifests"); got != 1 {
		t.Fatalf("insert count = %d want 1（失败的语句也要被记录）", got)
	}
}

func TestFakeDB_UnscriptedStatementFailsLoudly(t *testing.T) {
	db := New(nil)
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	var n int64
	err = gdb.Raw(`SELECT 1`).Scan(&n).Error
	if err == nil || !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("未脚本化语句必须显式失败，got %v", err)
	}
}

func TestFakeDB_ScanStringSlice(t *testing.T) {
	db := New(func(q string, _ []any) (Result, error) {
		return MultiRows([]string{"request_id"}, []any{"req_a"}, []any{"req_b"}), nil
	})
	gdb, err := db.GormDB()
	if err != nil {
		t.Fatalf("GormDB: %v", err)
	}
	var ids []string
	if err := gdb.Raw(`SELECT request_id FROM call_traces`).Scan(&ids).Error; err != nil {
		t.Fatalf("scan string slice: %v", err)
	}
	if len(ids) != 2 || ids[0] != "req_a" || ids[1] != "req_b" {
		t.Fatalf("ids = %v", ids)
	}
}
