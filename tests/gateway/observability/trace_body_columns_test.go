package observability_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCallTraceBodyRefColumnsRoundTrip 固定规格 §6 的 6 个新列：
// 四个定位键必须能存 NULL 与 0（不能把「没有定位键」和「part 0」混为一谈），
// body_missing 必须 NOT NULL DEFAULT false，body_bytes 可空。
func TestCallTraceBodyRefColumnsRoundTrip(t *testing.T) {
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))

	part, row, bytes := 0, 0, 4096
	inst := "blue"
	// date 列必须是 *time.Time（不是 *string）：见本任务字段说明。
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	in := obsmodel.CallTrace{
		RequestID:   "req-1",
		TS:          time.Now(),
		BodyRefDT:   &day,
		BodyRefInst: &inst,
		BodyRefPart: &part,
		BodyRefRow:  &row,
		BodyMissing: false,
		BodyBytes:   &bytes,
	}
	require.NoError(t, db.Create(&in).Error)

	var out obsmodel.CallTrace
	require.NoError(t, db.First(&out, "request_id = ?", "req-1").Error)
	require.NotNil(t, out.BodyRefPart)
	require.Equal(t, 0, *out.BodyRefPart, "part 0 必须读出 0，而不是 NULL")
	require.NotNil(t, out.BodyRefDT)
	require.Equal(t, "2026-09-24", out.BodyRefDT.Format("2006-01-02"),
		"date 列必须按日期口径读回；若用 *string 会读成 2026-09-24T00:00:00Z")

	// 没有定位键的行：四个指针全 nil。
	// 注意：下面的 BodyMissing 断言【不构成守卫】——Go 的 bool 零值恒为 false，
	// 该列即使可空、库里即使是 NULL 也照样为绿；`body_missing` 的约束由
	// TestCallTraceBodyMissingIsNotNullWithFalseDefault 在 schema 层钉住。
	require.NoError(t, db.Create(&obsmodel.CallTrace{RequestID: "req-2", TS: time.Now()}).Error)
	var none obsmodel.CallTrace
	require.NoError(t, db.First(&none, "request_id = ?", "req-2").Error)
	require.Nil(t, none.BodyRefDT)
	require.Nil(t, none.BodyRefPart)
	require.False(t, none.BodyMissing)
	require.Nil(t, none.BodyBytes)
}

// TestCallTraceBodyMissingIsNotNullWithFalseDefault 在 **schema 层**钉住模型 tag 的
// `not null;default:false`。上面往返测试里的 `require.False(none.BodyMissing)` 证明不了它：
// Go 的 bool 零值恒为 false，列即使可空、库里即使是 NULL，那一行也照样是绿的。
// 因此这里用两条**行为断言**，约束一旦缺失就真的会红：
//
//	① 显式写 NULL 必须被数据库拒绝（NOT NULL 约束）；
//	② 省略该列时读回的必须是「非 NULL 的 false」（DEFAULT false，而不是落成 NULL）。
func TestCallTraceBodyMissingIsNotNullWithFalseDefault(t *testing.T) {
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))

	// ① NOT NULL：显式写 NULL 必须报错
	err = db.Exec("INSERT INTO call_traces (request_id, ts, body_missing) VALUES (?, ?, NULL)",
		"req-null", time.Now()).Error
	require.Error(t, err, "body_missing 必须 NOT NULL：显式写 NULL 必须被数据库拒绝")

	// ② DEFAULT false：省略该列 ⇒ 落库必须是非 NULL 的 false
	require.NoError(t, db.Exec("INSERT INTO call_traces (request_id, ts) VALUES (?, ?)",
		"req-default", time.Now()).Error)
	var stored sql.NullBool
	require.NoError(t, db.Raw("SELECT body_missing FROM call_traces WHERE request_id = ?", "req-default").
		Scan(&stored).Error)
	require.True(t, stored.Valid, "省略该列时不得落成 NULL（NOT NULL / DEFAULT false 必须生效）")
	require.False(t, stored.Bool, "省略该列时默认值必须是 false")
}

// TestAddTraceBodyRefMigrationContent 固定迁移文件的**内容**：
// 只含作用在 call_traces 上的 6 个 ADD COLUMN，且不含任何破坏性语句；
// 并且该文件必须已登记进 atlas.sum。
//
// 【形态说明】atlas migrate diff 会把 6 个列合并进**一条** schema 限定的
// `ALTER TABLE "public"."call_traces"`（不是 6 条独立语句），所以这里断言的是
// 「1 条 ALTER TABLE + 恰好 6 个 ADD COLUMN」，而不是 6 条 ALTER TABLE。
//
// 【为什么必须挡住 DROP/ALTER COLUMN/CREATE TABLE】本仓库的迁移历史与 GORM model
// 已存在漂移（计划 D52）：完整 diff 产物含 15 处 DROP INDEX、81 处 ALTER COLUMN、
// 6 处 CREATE TABLE。本任务的迁移是从该产物里**只筛出 call_traces 的语句**，
// 一旦筛选不干净就会混入破坏性语句 ⇒ 用断言把它钉死。规格 §6：保留既有三个 jsonb 列不删。
func TestAddTraceBodyRefMigrationContent(t *testing.T) {
	matches, err := filepath.Glob("../../../atlas-migrations/*_add_trace_body_ref.sql")
	require.NoError(t, err)
	require.Len(t, matches, 1, "必须恰好有一个 add_trace_body_ref 迁移文件")

	raw, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	sql := string(raw)

	want := []string{
		`ADD COLUMN "body_ref_dt" date`,
		`ADD COLUMN "body_ref_inst" text`,
		`ADD COLUMN "body_ref_part" integer`,
		`ADD COLUMN "body_ref_row" integer`,
		`ADD COLUMN "body_missing" boolean NOT NULL DEFAULT false`,
		`ADD COLUMN "body_bytes" integer`,
	}
	for _, w := range want {
		require.Contains(t, sql, w)
	}
	require.Equal(t, 6, strings.Count(sql, "ADD COLUMN"), "恰好 6 个 ADD COLUMN")
	require.Equal(t, 1, strings.Count(sql, "ALTER TABLE"),
		"工具把 6 个列合并进一条 ALTER TABLE")
	require.NotContains(t, sql, "DROP", "迁移里不得出现任何 DROP（规格 §6：保留既有列）")
	require.NotContains(t, sql, "ALTER COLUMN", "本任务只加列，不得改动既有列定义")
	require.NotContains(t, sql, "CREATE TABLE", "本任务不建表：漂移语句不得混入")

	// 该迁移必须已登记进 atlas.sum：否则「文件在、条目被删」这种破坏会让本测试保持绿色，
	// 而部署时 Atlas 会因缺条目 / checksum mismatch 直接失败。
	sumPath := "../../../atlas-migrations/atlas.sum"
	sum, err := os.ReadFile(sumPath)
	require.NoError(t, err)
	require.Contains(t, string(sum), filepath.Base(matches[0]),
		"atlas.sum 必须包含该迁移文件名（缺失 ⇒ 部署时 checksum mismatch）")
}
