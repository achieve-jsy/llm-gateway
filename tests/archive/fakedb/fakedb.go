// Package fakedb 提供「可注入的假 DB」：一个不依赖真实 PostgreSQL 的 *gorm.DB。
//
// 背景：归档写/清理路径（internal/observability/service）直接持有 *gorm.DB，且下发的是
// PostgreSQL 专有 SQL（`AT TIME ZONE`、`COLLATE "C"`、`encode(sha256(...))`、`?::date`），
// 因此无法用 sqlite 顶替。本包实现一个最小 database/sql 驱动，把每条语句交给测试提供的
// Handler 脚本作答，从而在纯单测里驱动真实业务代码（契约 §8：测试必须用可注入的假 DB）。
//
// 用法：
//
//	db := fakedb.New(func(sql string, args []any) (fakedb.Result, error) {
//	    switch {
//	    case strings.Contains(sql, "count(*)"):
//	        return fakedb.Rows([]string{"count"}, []any{int64(3)}), nil
//	    default:
//	        return fakedb.Result{}, fmt.Errorf("fakedb: 未脚本化的语句: %s", sql)
//	    }
//	})
//	gormDB, err := db.GormDB()
//	...
//	db.Statements()        // 全部执行过的语句（含失败的）
//	db.CountContains("archive_ref = ")  // 断言「指针未被写入」
package fakedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Stmt 记录一条被执行的语句（SQL 原文 + 参数）。
type Stmt struct {
	SQL  string
	Args []any
}

// Result 是一次语句的脚本化结果。
//
//	Columns/Rows：查询结果集（列名 + 行值，值类型为 driver.Value）
//	RowsAffected：Exec 的影响行数；未显式设置时默认 1（模拟语句命中 1 行）
//	Err：返回给业务代码的错误（用于注入落库失败）；与 Handler 的 error 返回值等效
type Result struct {
	Columns      []string
	Rows         [][]driver.Value
	RowsAffected int64
	Err          error
}

// Handler 按 SQL 文本与参数返回结果。
// SQL 保持 GORM 生成的原文（PostgreSQL 风格 $1/$2 占位符，参数单独传入）。
type Handler func(sql string, args []any) (Result, error)

// Rows 构造一个「单行」结果集的便捷函数（列名与行值一一对应）。
func Rows(columns []string, values ...any) Result {
	row := make([]driver.Value, 0, len(values))
	for _, v := range values {
		row = append(row, v)
	}
	return Result{Columns: columns, Rows: [][]driver.Value{row}, RowsAffected: 1}
}

// MultiRows 构造「多行」结果集。
func MultiRows(columns []string, rows ...[]any) Result {
	out := make([][]driver.Value, 0, len(rows))
	for _, r := range rows {
		row := make([]driver.Value, 0, len(r))
		for _, v := range r {
			row = append(row, v)
		}
		out = append(out, row)
	}
	return Result{Columns: columns, Rows: out, RowsAffected: int64(len(out))}
}

// Empty 构造空结果集（0 行；GORM First 会因此返回 ErrRecordNotFound）。
func Empty(columns ...string) Result {
	return Result{Columns: columns}
}

// Err 构造一条失败语句的脚本结果。
func Err(err error) Result { return Result{Err: err} }

// DB 是假库实例（并发安全）。
type DB struct {
	mu    sync.Mutex
	stmts []Stmt
	h     Handler
}

// New 创建假库；h 为 nil 时任何语句都报错（暴露未脚本化的路径）。
func New(h Handler) *DB {
	if h == nil {
		h = func(sql string, _ []any) (Result, error) {
			return Result{}, fmt.Errorf("fakedb: no handler for statement: %s", compact(sql))
		}
	}
	return &DB{h: h}
}

// GormDB 打开指向本假库的 *gorm.DB（postgres dialector，注入自带 ConnPool，不连网络）。
func (d *DB) GormDB() (*gorm.DB, error) {
	sqlDB := sql.OpenDB(&connector{d: d})
	return gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Discard,
	})
}

// Statements 返回按执行顺序记录的全部语句（副本，含执行失败的语句）。
func (d *DB) Statements() []Stmt {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Stmt, len(d.stmts))
	copy(out, d.stmts)
	return out
}

// CountContains 返回 SQL（大小写不敏感）中包含 substr 的语句数。
func (d *DB) CountContains(substr string) int {
	return len(d.FindContains(substr))
}

// FindContains 返回 SQL 中包含 substr 的语句。
func (d *DB) FindContains(substr string) []Stmt {
	needle := strings.ToLower(substr)
	var out []Stmt
	for _, s := range d.Statements() {
		if strings.Contains(strings.ToLower(s.SQL), needle) {
			out = append(out, s)
		}
	}
	return out
}

// Reset 清空已记录语句（便于在同一假库上跑多段场景）。
func (d *DB) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stmts = nil
}

func (d *DB) record(sql string, args []any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]any, len(args))
	copy(cp, args)
	d.stmts = append(d.stmts, Stmt{SQL: sql, Args: cp})
}

func (d *DB) handle(sql string, args []any) (Result, error) {
	d.record(sql, args)
	return d.h(sql, args)
}

func compact(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// ─── database/sql 驱动实现（最小集）──────────────────────────────────────────

type connector struct{ d *DB }

func (c *connector) Connect(context.Context) (driver.Conn, error) { return &conn{d: c.d}, nil }
func (c *connector) Driver() driver.Driver                        { return driverImpl{} }

type driverImpl struct{}

func (driverImpl) Open(string) (driver.Conn, error) {
	return nil, fmt.Errorf("fakedb: 不支持 DSN，请用 DB.GormDB()")
}

type conn struct{ d *DB }

func (c *conn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("fakedb: Prepare 未实现（GORM 默认走 QueryerContext/ExecerContext）")
}
func (c *conn) Close() error              { return nil }
func (c *conn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("fakedb: 不支持事务") }

func (c *conn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	res, err := c.d.handle(query, namedValues(args))
	if err != nil {
		return nil, err
	}
	if res.Err != nil {
		return nil, res.Err
	}
	return &rows{cols: res.Columns, rows: res.Rows}, nil
}

func (c *conn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	res, err := c.d.handle(query, namedValues(args))
	if err != nil {
		return nil, err
	}
	if res.Err != nil {
		return nil, res.Err
	}
	n := res.RowsAffected
	if n == 0 && len(res.Rows) == 0 {
		// 默认「命中 1 行」：业务代码普遍用 RowsAffected != 0 判定落库成功。
		n = 1
	}
	return driver.RowsAffected(n), nil
}

func namedValues(args []driver.NamedValue) []any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		out = append(out, a.Value)
	}
	return out
}

type rows struct {
	cols []string
	rows [][]driver.Value
	i    int
}

func (r *rows) Columns() []string { return r.cols }
func (r *rows) Close() error      { return nil }

func (r *rows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}
