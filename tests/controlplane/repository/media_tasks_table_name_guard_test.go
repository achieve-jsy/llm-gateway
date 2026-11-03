package repository_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// 本文件是「控制面不得再出现旧表名 `video_tasks` / `video_task_events`」的**静态守卫**。
//
// 为什么需要它（E 队 Batch 3 · 任务 1）：
// C1（决策 D）把 `video_tasks` 改名为 `media_tasks`、`video_task_events` 改名为
// `media_task_events`。GORM 的 `Model(&model.VideoTask{})` 走 `TableName()`，自动正确；
// 但**手写 SQL / Raw() / Table("...") / 字符串常量**不会跟着改 —— 漏掉一处就会在生产上
// 直接表现为 `relation "video_tasks" does not exist`（看板/导出/告警整块 500）。
//
// 判定规则（逐行，够严也不误报）：
//   - 出现 `video_tasks` / `video_task_events` 的行，先剔掉「允许的具名对象」：
//     GORM tag 里的二级索引名 `idx_video_tasks_*` / `idx_video_task_events_*`
//     （**刻意**保留历史名，见 model/video_task.go 的具名对象注释）；
//   - 剩下的行若仍是纯注释（`//`、`/*`、`*` 开头）→ 允许（历史说明文字）；
//   - 其余任何出现（尤其带 SELECT/FROM/JOIN/WHERE/UPDATE/DELETE 的字符串）→ **失败**。
//
// 该守卫覆盖整个 `internal/controlplane`，新增裸 SQL 时无需改本测试（它会自己抓到）。

// allowedNamedObjectRe 匹配「允许保留历史名」的具名对象标识符。
//
// 只放行 GORM tag 里的索引/唯一约束名，不放行任何 SQL 片段：
// 索引名不是表名，改名后二级索引**刻意**保留 `idx_video_tasks_*`（Atlas 按「名 + 定义」
// 比对索引，改名的成本与风险都不划算）。
var allowedNamedObjectRe = regexp.MustCompile(`(?:idx|uniq|fk)_video_task(?:s|_events)_[A-Za-z0-9_]*`)

// legacyTableTokens 是必须绝迹的旧表名字面量。
var legacyTableTokens = []string{"video_tasks", "video_task_events"}

func TestControlPlaneHasNoRawLegacyVideoTaskTableName(t *testing.T) {
	root := repoRoot(t)
	scanRoot := filepath.Join(root, "internal", "controlplane")
	if _, err := os.Stat(scanRoot); err != nil {
		t.Fatalf("扫描根目录不存在（工作区结构变了？）: %v", err)
	}

	var violations []string
	scanned := 0

	err := filepath.WalkDir(scanRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(raw), "\n") {
			candidate := line
			for _, token := range legacyTableTokens {
				if !strings.Contains(candidate, token) {
					continue
				}
				// 剔掉允许的具名对象（索引/约束名）后再判定。
				stripped := allowedNamedObjectRe.ReplaceAllString(candidate, "")
				if !containsAnyToken(stripped) {
					continue
				}
				if isCommentLine(stripped) {
					continue
				}
				violations = append(violations, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", scanRoot, err)
	}
	if scanned == 0 {
		t.Fatalf("未扫描到任何 .go 文件（路径推断错了？root=%s）", root)
	}

	if len(violations) > 0 {
		t.Fatalf("控制面仍有 %d 处引用旧表名（video_tasks/video_task_events）—— "+
			"改名后必须写成 media_tasks/media_task_events，否则生产上会 "+
			"`relation \"video_tasks\" does not exist`:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// TestControlPlaneTableNameGuardHasTeeth 是守卫自身的**反向验证**：
// 证明上面的判定规则真的能抓到裸 SQL，而不是因为过滤规则太宽而永远通过
// （否则这个守卫就是装饰品）。
func TestControlPlaneTableNameGuardHasTeeth(t *testing.T) {
	mustFail := []string{
		`db.Raw("SELECT COUNT(*) FROM video_tasks")`,
		`db.Table("video_tasks").Where("status = ?", "failed")`,
		"sql := `DELETE FROM video_task_events WHERE task_id = ?`",
		`query := "SELECT * FROM " + "video_tasks"`,
		`db.Exec("UPDATE video_tasks SET status = 'expired'")`,
		`const legacyTable = "video_tasks"`,
	}
	for _, line := range mustFail {
		if !wouldBeViolation(line) {
			t.Errorf("守卫漏判（放过了真实违规）: %s", line)
		}
	}

	mustPass := []string{
		`// 表名已由 video_tasks 改名为 media_tasks（历史说明，允许）`,
		"// 具名对象保留历史名 idx_video_tasks_status / idx_video_task_events_task_id",
		"Status string `gorm:\"type:varchar(32);not null;index:idx_video_tasks_status\" json:\"status\"`",
		"CreatedAt time.Time `gorm:\"not null;default:now();index:idx_video_task_events_created_at\" json:\"created_at\"`",
		`query := r.db.Model(&model.VideoTask{}).Where("media_tasks.user_id = ?", scope.UserID)`,
		`func (VideoTask) TableName() string { return "media_tasks" }`,
	}
	for _, line := range mustPass {
		if wouldBeViolation(line) {
			t.Errorf("守卫误判（把合法行当成违规）: %s", line)
		}
	}
}

// wouldBeViolation 复刻守卫的逐行判定（本测试与守卫共用同一规则，避免两处漂移）。
func wouldBeViolation(line string) bool {
	for _, token := range legacyTableTokens {
		if !strings.Contains(line, token) {
			continue
		}
		stripped := allowedNamedObjectRe.ReplaceAllString(line, "")
		if !containsAnyToken(stripped) {
			continue
		}
		if isCommentLine(stripped) {
			continue
		}
		return true
	}
	return false
}

func containsAnyToken(s string) bool {
	for _, token := range legacyTableTokens {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "//") ||
		strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// repoRoot 从本测试文件位置向上找到含 go.mod 的目录（tests/ 与 internal/ 分离，不能写死层级）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败，无法定位仓库根目录")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("从 %s 向上未找到 go.mod", file)
	return ""
}
