# Catalog 模块 TDD 测试报告

**生成日期**: 2026-05-23  
**测试框架**: Go testing + testify/assert/require  
**总测试数**: 111  
**通过**: 111  
**失败**: 0  
**通过率**: 100%

---

## 1. 测试范围概览

Catalog 模块横跨三层架构，测试覆盖如下：

| 层级 | 文件 | 测试数 | 测试类型 |
|------|------|--------|----------|
| **Handler** | `catalog_handler_test.go` | 35 | HTTP 接口集成测试（fake service mock） |
| **Service** | `catalog_sync_service_test.go` | 53 | 业务逻辑单元测试（fake repo mock） |
| **Service** | `catalog_parser_test.go` | 22 | JSON 解析纯函数测试 |
| **Service** | `catalog_opts_test.go` | 5 | 配置/环境变量驱动测试 |
| **Service** | `model_catalog_service_test.go` | 18 | Model-Catalog 联动测试 |
| **Repository** | `model_catalog_repo_test.go` | 26 | 数据库 CRUD 测试（SQLite 内存 DB） |

---

## 2. 功能覆盖矩阵

### 2.1 Handler 层 — HTTP API 路径覆盖

| API 路径 | 正向场景 | 异常场景 | 新增测试 |
|----------|---------|---------|---------|
| `GET /search` | QueryParam, FuzzyMatch, TotalIsFullCount, DefaultLimit, SourceParam | ServiceError | SourceParam ✨ |
| `GET /by-source` | OK, DefaultSource, 404 | MissingSourceModelID, ServiceError | - |
| `GET /:id` | OK ✨ | InvalidUUID, NotFound, ServiceError | GetByID_OK ✨ |
| `POST /sync` | AcceptedAndAsync, ReturnsSyncingTrue ✨ | - | ReturnsSyncingTrue ✨ |
| `GET /diff` | ReturnsOverrideFlag, Empty ✨ | ServiceError | Diff_OK_Empty ✨ |
| `POST /apply-diff` | PartialSelection, SingleID ✨ | InvalidIDs, ServiceError, EmptyBody, EmptyModelIDs | OK_SingleID ✨ |
| `POST /auto-bind` | ReturnsResult | ServiceError | - |
| `GET /binding-stats` | OK, Empty ✨ | ServiceError | OK_Empty ✨ |
| `GET /sync-status` | Idle, Running, FinishedWithError | ServiceError | - |

✨ = 本次新增

### 2.2 Service 层 — CatalogSyncService 功能覆盖

| 功能 | 正向场景 | 异常/边界场景 | 新增测试 |
|------|---------|-------------|---------|
| **SyncFromRemote** | HappyPath, RetryOnTransient | FallsBackToBundled, NoBundled, ConcurrentRejected | - |
| **SyncFromBundled** | HappyPath | EmptyBytes, InvalidJSON ✨ | SyncFromBundled_InvalidJSON ✨ |
| **IsSyncing** | ReflectsState ✨ | - | IsSyncing_ReflectsState ✨ |
| **SyncStatus** | AfterRemoteSuccess, AfterFallback, InitiallyEmpty | AfterTotalFailure, ConcurrentRejected, RepoCountError | - |
| **HTTPClient 注入** | CustomClient ✨ | - | HTTPClientInjection ✨ |
| **ApplyAutoUpdates** | SyncsContextLength, SkipsOverridden, NeverTouchesPrice | NoModelRepo, MultipleModels ✨ | MultipleModels ✨ |
| **ComputePriceDiff** | DetectsOverride, FiltersOutEqual, KeepsDeprecated | NoModelRepo | PriceUnsetIsNotOverridden, PriceSetAndDifferent, MultipleModels ✨ |
| **ApplyPriceDiff** | BatchUpdate | InvalidIDs, NoModelRepo, NoCatalogLink, EmptyIDs, CatalogNotFound ✨ | CatalogNotFound ✨ |
| **AutoBind** | OneToOneMatch, TailSegmentSingle, TailSegmentAmbiguous, FallbackToUpstream | NoModelRepo, SetCatalogIDFail, SkipsAlreadyBound | UpstreamModelIDTailSegment ✨, EmptyNameAndUpstream ✨ |
| **BindingStats** | Empty, MixedBoundUnbound | ModelRepoNil | - |
| **parsedToModel** | FieldsCopied, TierPricing | - | - |
| **deltaPct/floatEqual/lastSegment** | FromZero, Normal, FloatEqual | - | - |

### 2.3 Service 层 — ParseLiteLLMJSON 功能覆盖

| 功能 | 测试场景 | 新增 |
|------|---------|------|
| **sample_spec 跳过** | SkipsSampleSpec | - |
| **基础解析** | BasicEntry（provider/mode/pricing/capabilities/deprecation） | - |
| **上下文长度** | max_input_tokens 优先, max_tokens 回退 | MaxOutputFallsBackToMaxTokens ✨ |
| **单价换算** | per-token × 1e6 → per-million | - |
| **缓存定价** | cache_read + cache_write | CachePrices, CacheWritePrice ✨ |
| **阶梯定价** | 单阈值 TieredPricing | 多阈值取最小 ✨ |
| **能力位** | 存在=true, 缺失=false | - |
| **DeprecationDate** | YYYY-MM-DD, RFC3339, Slash, Invalid, Whitespace | - |
| **source_model_id 含 '/'** | KeepsSlashedID | - |
| **空输入** | EmptyInput ✨ | ✨ |
| **DisplayName** | DisplayNameIsSourceID ✨ | ✨ |
| **无定价字段** | NoPricingFields ✨ | ✨ |
| **无效 JSON entry** | SkipsInvalidJSONEntry ✨ | ✨ |
| **全量端到端** | FullBundledFile（>2000 条目, 80% 有效率） | - |

### 2.4 Service 层 — CatalogOpts 配置覆盖

| 配置项 | 测试场景 |
|--------|---------|
| CATALOG_REMOTE_URL | EnvURL_OverridesDefault, ExplicitURL_TakesPrecedenceOverEnv |
| CATALOG_HTTP_TIMEOUT | EnvTimeout_OverridesDefault, ShortEnoughCausesTimeout |
| 默认回退 | NoEnv_FallsBackToDefaultURL |

### 2.5 Model-Catalog 联动测试覆盖

| 功能 | 测试场景 |
|------|---------|
| **CreateModelFromCatalog** | CopiesSnapshot, CatalogNotFound |
| **UpdateModel override 标记** | AutoMarksCtxOverridden, AutoMarksMaxOutputOverridden, NoCatalog_DoesNotMark |
| **ResetOverride** | ReturnsToCatalogValue, InvalidField, ModelNotFound |
| **BindCatalog** | NoReset, WithReset, AlreadySameRejects, AlreadySameAllows, ModelNotFound, CatalogNotFound |
| **UnbindCatalog** | ClearsLink, NotBound, ModelNotFound |
| **BatchBindCatalog** | PartialFailure, Empty |

### 2.6 Repository 层 — ModelCatalogRepo 功能覆盖

| 功能 | 测试场景 | 新增 |
|------|---------|------|
| **Upsert** | InsertNew, UpdateExisting | AutoGenerateID ✨, DefaultSource ✨, DefaultSyncedAt ✨ |
| **GetByID** | RoundTrip, NotFound | - |
| **GetBySourceModelID** | NotFound | - |
| **Search** | ByQuery, ByProvider, ByMode, BySource, CaseInsensitive, CombinedFilters, EmptyQuery, DisplayNameFuzzy, TotalIsFullCount | LimitBounds ✨, OrderByCreatedAtDesc ✨ |
| **List** | Pagination | DefaultLimit ✨ |
| **BatchUpsert** | BatchUpsert, Empty | - |
| **Count** | 空库 / 有数据 | - |
| **DeleteStale** | 删除旧条目 | - |
| **ListAll** | 返回全量 | - |

---

## 3. 新增测试明细（本次 TDD 补充）

| # | 测试名 | 层级 | 验证目标 |
|---|--------|------|---------|
| 1 | `TestCatalogHandler_GetByID_OK` | Handler | UUID 主键成功查询返回 200 + data |
| 2 | `TestCatalogHandler_Search_SourceParam` | Handler | Search 接口 source 参数透传 |
| 3 | `TestCatalogHandler_Diff_OK_Empty` | Handler | diff 为空时返回空数组 + total=0 |
| 4 | `TestCatalogHandler_ApplyDiff_OK_SingleID` | Handler | 单个 model_id apply 返回 updated=1 |
| 5 | `TestCatalogHandler_Sync_ReturnsSyncingTrue` | Handler | sync 返回 202 + syncing=true |
| 6 | `TestCatalogHandler_BindingStats_OK_Empty` | Handler | 全空 binding-stats 透传 |
| 7 | `TestParseLiteLLMJSON_EmptyInput` | Parser | 空 map 返回空列表 |
| 8 | `TestParseLiteLLMJSON_DisplayNameIsSourceID` | Parser | display_name 默认值=source_model_id |
| 9 | `TestParseLiteLLMJSON_NoPricingFields` | Parser | 无价格字段时默认 0 |
| 10 | `TestParseLiteLLMJSON_MultipleTierThresholds` | Parser | 多阈值取最小 + 最后处理的价格 |
| 11 | `TestParseLiteLLMJSON_SkipsInvalidJSONEntry` | Parser | 非 JSON object entry 被跳过 |
| 12 | `TestParseLiteLLMJSON_MaxOutputFallsBackToMaxTokens` | Parser | max_output_tokens 回退到 max_tokens |
| 13 | `TestParseLiteLLMJSON_CacheWritePrice` | Parser | 仅 cache_write 有值时 cache_read 为 nil |
| 14 | `TestCatalogSync_IsSyncing_ReflectsState` | Sync | IsSyncing() 在同步期间/之后状态正确 |
| 15 | `TestCatalogSync_HTTPClientInjection` | Sync | 自定义 HTTPClient 可注入 |
| 16 | `TestAutoBind_UpstreamModelIDTailSegment` | Sync | upstream_model_id 去前缀匹配 |
| 17 | `TestAutoBind_EmptyNameAndUpstream` | Sync | 空 name+upstream → unmatched |
| 18 | `TestApplyAutoUpdates_MultipleModels` | Sync | 多 model 同时自动同步 |
| 19 | `TestComputePriceDiff_MultipleModels` | Sync | 多 model 仅不一致的出现在 diff |
| 20 | `TestApplyPriceDiff_CatalogNotFound` | Sync | catalog 不存在时报错 |
| 21 | `TestCatalogSync_SyncFromBundled_InvalidJSON` | Sync | bundled 数据非法 JSON 报错 |
| 22 | `TestCatalogRepo_Upsert_AutoGenerateID` | Repo | uuid.Nil 时自动生成 ID |
| 23 | `TestCatalogRepo_Upsert_DefaultSource` | Repo | 空 source 默认 "litellm" |
| 24 | `TestCatalogRepo_Upsert_DefaultSyncedAt` | Repo | 空 synced_at 自动设为当前时间 |
| 25 | `TestCatalogRepo_Search_LimitBounds` | Repo | limit=0 默认 20, limit>200 默认 20 |
| 26 | `TestCatalogRepo_Search_OrderByCreatedAtDesc` | Repo | 最新条目排在前面 |
| 27 | `TestCatalogRepo_List_DefaultLimit` | Repo | List limit=0 默认 50 |

---

## 4. 测试策略总结

| 策略 | 说明 |
|------|------|
| **接口隔离** | Handler 通过 `CatalogServiceInterface` mock；SyncService 通过 `CatalogRepoInterface` + `CatalogSyncModelRepo` mock，不依赖真实 DB |
| **纯函数测试** | `ParseLiteLLMJSON` / `parsedToModel` / `lastSegment` / `deltaPct` / `floatEqual` 无任何副作用，输入 → 输出直接断言 |
| **SQLite 内存 DB** | Repo 层用 `newTestDB(t)` 创建 SQLite 内存实例，测试结束自动清理 |
| **并发安全** | `TestSyncStatus_ConcurrentTriggerIsRejected` 验证互斥锁拒绝并发同步 |
| **容错/韧性** | Parser 跳过 sample_spec 和无效 entry；Sync 有 bundled 回退和 retry；AutoBind SetCatalogID 失败降级为 unmatched |
| **业务回归** | TotalIsFullCountNotPage 防止 limit=1 时 total=1 的 UI 误报；PriceUnsetIsNotOverridden 防止 $0 误标为"已脱离目录" |

---

## 5. 结论

Catalog 模块 9 个 API 路径、6 个核心业务功能、全部数据访问方法均已通过 TDD 测试覆盖。111 项测试 100% 通过，新增 27 项测试填补了原有覆盖在以下方面的空白：

- **Handler 正向场景补全**：GetByID 成功、Diff 空、ApplyDiff 单项、Sync 返回值
- **Parser 边界条件**：空输入、无定价、多阈值、无效 entry、max_output 回退
- **Sync 状态与配置**：IsSyncing 反映状态、HTTPClient 注入、bundled 无效 JSON
- **AutoBind 边界**：upstream_model_id 去前缀匹配、空字段 unmatched
- **Repo 默认值**：ID 自动生成、source/synced_at 默认、limit 边界、排序