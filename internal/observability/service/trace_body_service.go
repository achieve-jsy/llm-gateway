package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"gorm.io/gorm"
)

// 本文件是读侧点读服务：给定 request_id，从 call_traces 取回定位键
// (dt, inst, part, row)，经冻结包 internal/tracearchive.Reader 逐字节取回正文行；
// 对「功能关闭（正文在 jsonb 列）」「写入时即失败（body_missing）」「定位不到」
// 「产物损坏」四类状态给出**可判定且互不混淆**的结果（规格 §8.1/§8.2）。
//
// 【存储原语不在本文件】footer/trailer 编解码、按定位键解压定位、spool 兜底、
// 行号越界与残行识别全部由计划 1 冻结的 internal/tracearchive 实现；本文件只接线。
// 因此这里**不得**出现任何解压/编解码逻辑，也**不得**为隔离件新增分支
// （规格 §7.1：隔离件不命中两条精确路径 ⇒ 天然返回 ErrNotArchived）。

// traceBodyRefKeyCount 是定位键的分量数（dt / inst / part / row）。
// 「非 NULL 分量数 == 4」⇔ 四个定位键全非 NULL；1..3 ⇔ 部分 NULL；0 ⇔ 全 NULL。
const traceBodyRefKeyCount = 4

// traceBodyRefDateLayout 是 dt 的**布局格式化**（不是日期换算）。
// 读侧零日期计算（全局约束 + E2）：dt 的唯一事实源是行内已存的 body_ref_dt 列
// （写入侧算好并落库），读侧不做任何日期计算：不取当前时刻、不构造日期、不按时区定位，
// 也不注入任何日期口径。这里只把 date 列值按目录名布局格式化。
const traceBodyRefDateLayout = "2006-01-02"

// tracearchiveReader 是点读原语的【窄接口】——只声明本服务用到的唯一方法。
//
// 目的：B 层可以用替身注入「特定哨兵」，不必真的造出损坏产物；
// *ta.Reader 天然满足它（方法集一致），生产上直接传真实 Reader。
//
// 【接线注意】读侧禁用时**不要**把 `var r *ta.Reader`（nil 指针）装进这个接口字段：
// nil 指针装进接口后 != nil，服务会真的去调它并 panic。禁用时必须让该字段保持
// 「接口值为 nil」，由构造期校验拦下「已启用却没给 Reader」的接线错误。
type tracearchiveReader interface {
	GetBody(ctx context.Context, ref ta.Ref) ([]byte, error)
}

// TraceBodySource 标识正文的实际来源（对外可观测，便于排障与压缩比核算）。
type TraceBodySource string

const (
	SourceArchive TraceBodySource = "archive" // 封块产物
	SourceSpool   TraceBodySource = "spool"   // 未封块的明文 spool
	SourceJSONB   TraceBodySource = "jsonb"   // 功能关闭/构造失败 ⇒ 正文仍在 PG 列
)

// TraceBody 是对外统一的正文形状（三键，与计划 2 的 trace_line.go 同一形状）。
//
// 【omitempty 是形状契约】NULL 列必须**不出现键**，而不是出现 `null` ——
// 否则调用方无法用同一套解析代码处理 jsonb 回退与归档两个来源。
type TraceBody struct {
	RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
	RequestBody    json.RawMessage `json:"request_body,omitempty"`
	ResponseBody   json.RawMessage `json:"response_body,omitempty"`
}

// TraceBodyResult 是 GetBody 的成功结果。
//
// 【逐字节保证的适用边界（登记 D57）】归档路径的 Body 是写入时那一行的**原字节**；
// jsonb 回退路径是**重新序列化**（键序与空白不保证与请求当时相同）——这是 jsonb
// 存储本身的代价，不得声称 jsonb 路径逐字节。
type TraceBodyResult struct {
	Body   []byte
	Source TraceBodySource
	// Ref 是本次取回**实际使用**的定位键（四个分量的唯一导出途径）。
	//
	// 【为什么必须有这个导出（登记 E14）】任务 7 的 CLI `--ref` 要输出 dt/inst/part/row
	// 四分量且**必须复用本服务**；若没有这个字段，CLI 只能自己查 `call_traces` 的四列
	// 再拼定位键 —— 那是「什么算合法定位键」的第二份实现，正是 D59 警告的形态。
	//
	// 【语义】只有「经 Reader.GetBody 命中归档/未封块 spool」的成功路径才有值，
	// 且与请求行的四列一一对应（DT 是行内 body_ref_dt 的布局格式化，不是日期换算）。
	// jsonb 回退路径（四个定位键全为 NULL）**没有定位键可报 ⇒ 零值**（不是拼半个路径）。
	// 其余失败路径（记录缺失 / ErrBodyMissing / ErrTraceBodyInconsistent /
	// ErrTraceBodyDisabled / ErrNotArchived / Err*Corrupt）一律**不返回结果** ⇒ 本字段无值可读。
	Ref ta.Ref
}

// TraceBodyServiceOptions 是点读服务的装配选项。
//
// 【没有日期换算字段，也不得有】dt 一律取自行内 body_ref_dt 列，读侧零日期计算
// （全局约束 + E2）。
type TraceBodyServiceOptions struct {
	Config cpconfig.TraceBodyReadConfig
	// Reader 为 nil ⇒ 归档路径不可用。读侧启用（root 非空）时**必须**提供；
	// 读侧禁用时保持 nil 即可（带定位键的行会在触碰任何存储之前返回 ErrTraceBodyDisabled）。
	Reader tracearchiveReader
}

// TraceBodyService 把 PG 行与冻结包 Reader 接起来：查库 → 优先级判定 → 归档取回 /
// jsonb 回退 → 错误分类。它**不碰协议层**（没有 web 框架上下文、没有状态码）。
type TraceBodyService struct {
	db     *gorm.DB
	cfg    cpconfig.TraceBodyReadConfig
	reader tracearchiveReader
	paths  ta.Paths
}

// NewTraceBodyService 构造点读服务。
//
// 两道构造期否决（都必须在启动时响亮失败，而不是等到第一次点读）：
//   - db 为 nil：点读必须能查 call_traces；
//   - 读侧已启用却没给 Reader：归档路径会在运行时对 nil 接口调用并 panic。
//
// 读侧禁用（Root 空）时 Reader 可以为 nil —— 这是任务 5 的接线形态。
func NewTraceBodyService(db *gorm.DB, opts TraceBodyServiceOptions) (*TraceBodyService, error) {
	if db == nil {
		return nil, errors.New("tracebody: db 不能为空（点读必须能查 call_traces）")
	}
	if !opts.Config.Disabled() && opts.Reader == nil {
		return nil, errors.New("tracebody: 读侧已启用（trace_body.root 非空）但未提供 Reader")
	}
	return &TraceBodyService{
		db:     db,
		cfg:    opts.Config,
		reader: opts.Reader,
		paths:  ta.NewPaths(opts.Config.Root),
	}, nil
}

// GetBody 是唯一的对外取正文入口。
//
// 【判定顺序（逐条承重，任何一步被摘掉或换序都必须有变异红）】
//
//  0. 记录不存在（gorm.ErrRecordNotFound）        ⇒ 透传（handler 映射 404）
//  1. 定位键/body_missing 的**形态判定**（纯内存，先于任何存储访问）
//     · 定位键部分 NULL（1..3 个非 NULL）
//     · body_missing=true 却带定位键（数据写坏）
//     · 列值非法（cpconfig.ValidateRef 失败；理论上不该发生）
//     ⇒ 一律 ErrTraceBodyInconsistent（服务层 500）
//  2. body_missing == true                        ⇒ ErrBodyMissing（从未落盘，404 类）
//  3. 四个定位键全为 NULL                          ⇒ 200 + SourceJSONB（正文在 jsonb 列）
//  4. 四个定位键全非 NULL                          ⇒ 禁用 ⇒ ErrTraceBodyDisabled（503）；
//     否则 Reader.GetBody + 哨兵透传
//
// 【错误身份】直接透传冻结包的哨兵（用 %w / 原样返回），调用方用 errors.Is 分类；
// 本服务不新增存储层哨兵。ErrTraceBodyInconsistent / ErrTraceBodyDisabled 是服务层哨兵。
//
// 【为什么「body_missing 且带定位键」归 1 而不是 2】全局约束把「四者之外的形态」定为
// **500 + 明确诊断**，并明文禁止报 404：报 404 会把「存储不一致」伪装成业务事实
// （规格 §8.1 反复强调要消灭的一类错误）。步骤 1 是纯内存判定，因此步骤 2 仍然是
// 「先于任何存储访问」的判决。
func (s *TraceBodyService) GetBody(ctx context.Context, requestID string) (*TraceBodyResult, error) {
	var trace obsmodel.CallTrace
	if err := s.db.WithContext(ctx).First(&trace, "request_id = ?", requestID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 记录不存在：透传 gorm 哨兵（%w 包装后 errors.Is 仍成立，handler 用它映射 404）。
			return nil, fmt.Errorf("tracebody: %s 无对应 call_traces 记录: %w", requestID, gorm.ErrRecordNotFound)
		}
		return nil, fmt.Errorf("tracebody: 查询 call_traces 失败: %w", err)
	}

	refs := refsOf(&trace)
	allRefs := len(refs) == traceBodyRefKeyCount

	// 只有四个键全非 NULL 才构造定位键；其它形态下 ref 保持零值且不参与任何判定
	// （因此不会拼出半个路径）。
	var ref ta.Ref
	if allRefs {
		ref = ta.Ref{
			DT:   trace.BodyRefDT.Format("2006-01-02"), // 只做布局格式化；不是日期换算
			Inst: *trace.BodyRefInst,
			Part: *trace.BodyRefPart,
			Row:  *trace.BodyRefRow,
		}
	}

	// 【不一致的唯一出口】以下三种形态都是「PG 行在本服务语义下不可解释」，
	// 一律 500，既不静默回退 jsonb，也不报 404：
	//   ① 定位键部分 NULL；
	//   ② body_missing=true 却带定位键；
	//   ③ 列值非法（ValidateRef 失败：dt 来自 date 列、inst 来自白名单 ⇒ 理论上不该发生）。
	// 判据全是纯内存的：证据是替身 Reader 的调用次数必须为 0。
	badRefs := len(refs) > 0 && (!allRefs || trace.BodyMissing)
	if allRefs {
		if err := cpconfig.ValidateRef(ref); err != nil {
			badRefs = true
		}
	}
	if badRefs {
		return nil, fmt.Errorf("%w: %s", ErrTraceBodyInconsistent, requestID)
	}

	if trace.BodyMissing {
		// 正文在写入时即失败，从未落盘（规格 §9.1）：这是业务事实，不是存储故障。
		return nil, ta.ErrBodyMissing
	}
	if len(refs) == 0 {
		// 四个定位键全为 NULL：正文在 jsonb 列（功能关闭 / sink 构造失败）。
		// 判定顺序的 2a（读侧禁用）与 2b（读侧启用下的历史行）是同一结果 ⇒ 合并成这一处。
		return s.fromJSONB(trace)
	}

	// 到这里：四个定位键全非 NULL，且未标 body_missing ⇒ 正文在归档。
	// 先判禁用：元数据指向归档但读侧未配置（TRACE_BODY_ROOT 为空）属**部署错误**，
	// 既不是 404（正文不存在），也不是 jsonb 回退（把部署错误伪装成「正文在 jsonb 列」）。
	if s.cfg.Disabled() {
		return nil, fmt.Errorf("%w: %s", ErrTraceBodyDisabled, requestID)
	}
	body, err := s.reader.GetBody(ctx, ref)
	if err != nil {
		// 原样透传冻结包哨兵（ErrBodyMissing / ErrNotArchived / ErrFooterCorrupt /
		// ErrTrailerCorrupt）：调用方用 errors.Is 分类，底层诊断文本必须保留
		// （ErrNotArchived 覆盖三种可分辨的成因，换成固定文案会让运维无从下手）。
		return nil, err
	}
	src := sourceOf(s.paths, ref)
	// Ref: ref —— 把本次实际使用的定位键随结果导出（任务 7 的 CLI --ref 复用本服务，
	// 不得自己查 call_traces 四列；见 TraceBodyResult.Ref 的语义注释）。
	return &TraceBodyResult{Body: body, Source: src, Ref: ref}, nil
}

// ErrTraceBodyInconsistent 是【服务层】哨兵：PG 行的定位键/body_missing 组合在
// 读侧语义下不可解释（4 个键部分非 NULL；或 body_missing=true 却带定位键；或列值非法）。
//
// 【为什么必须与 ErrNotArchived 分开】前者是「元数据自身矛盾」，后者是
// 「元数据与存储不一致」——运维处置不同（一个是数据写坏，一个是文件不在/越界）。
var ErrTraceBodyInconsistent = errors.New("tracebody: 定位键与 body_missing 组合不一致")

// ErrTraceBodyDisabled 读侧禁用（Root 空）但行【带定位键】⇒ 部署错误（503）。
var ErrTraceBodyDisabled = errors.New("tracebody: 读侧未配置（TRACE_BODY_ROOT 为空）")

// fromJSONB 把「正文在 jsonb 列」的行重建成本计划的统一三键形状。
//
// 【为什么必须重建】归档路径返回的是一个三键 JSON 对象（计划 2 的 trace_line.go，
// 键序 request_headers / request_body / response_body，omitempty）；jsonb 回退必须
// 给出同一形状，调用方才能用同一套解析代码处理两个来源。
// 【它不是逐字节的（登记 D57）】这里是一次重新序列化：键序与空白由 json.Marshal
// 决定，不保证等于请求当时的字节。
func (s *TraceBodyService) fromJSONB(trace obsmodel.CallTrace) (*TraceBodyResult, error) {
	shape := TraceBody{
		RequestHeaders: json.RawMessage(trace.RequestHeaders),
		RequestBody:    json.RawMessage(trace.RequestBody),
		ResponseBody:   json.RawMessage(trace.ResponseBody),
	}
	b, err := json.Marshal(shape)
	if err != nil {
		return nil, fmt.Errorf("tracebody: 重新序列化 jsonb 正文失败: %w", err)
	}
	return &TraceBodyResult{Body: b, Source: SourceJSONB}, nil
}

// refsOf 返回四个定位键中**非 NULL** 分量的文本形态（按 dt / inst / part / row 顺序）。
//
// 【返回长度的语义载体】NULL 处不产生条目 ⇒ len(refs) 就是「非 NULL 分量数」，
// 「全 NULL / 部分 NULL / 全非 NULL」三种形态共用一次判定（不得拆成三段 if）。
// dt 只做布局格式化，读侧零日期计算（E2）。
func refsOf(trace *obsmodel.CallTrace) []string {
	refs := make([]string, 0, traceBodyRefKeyCount)
	if trace.BodyRefDT != nil {
		refs = append(refs, trace.BodyRefDT.Format(traceBodyRefDateLayout))
	}
	if trace.BodyRefInst != nil {
		refs = append(refs, *trace.BodyRefInst)
	}
	if trace.BodyRefPart != nil {
		refs = append(refs, strconv.Itoa(*trace.BodyRefPart))
	}
	if trace.BodyRefRow != nil {
		refs = append(refs, strconv.Itoa(*trace.BodyRefRow))
	}
	return refs
}

// sourceOf 只用于诊断字段 Source；判定顺序与 Reader.GetBody 一致（产物 → spool）。
//
// 【为什么不用 GetBody 的返回值判断来源】它不回报来源。因此这里的判定与 GetBody
// 内部是两次 stat，理论上存在「两次之间 spool 被 sealer 删除」的窗口 —— 这**不是**
// 竞态缺陷：GetBody 自身在同一次调用内按「产物 → spool」判定，两次 stat 只影响
// Source 这个**诊断字段**；最坏情况是 Source 标成 spool 而实际读到产物（或反之），
// **正文内容仍然正确**。Source 不是原子的，不得如此声称。
func sourceOf(paths ta.Paths, ref ta.Ref) TraceBodySource {
	if _, err := os.Stat(paths.PartFile(ref)); err == nil {
		return SourceArchive
	}
	if _, err := os.Stat(paths.SpoolFile(ref)); err == nil {
		return SourceSpool
	}
	return SourceArchive // 都不存在：GetBody 必返回 ErrNotArchived，Source 无语义
}
