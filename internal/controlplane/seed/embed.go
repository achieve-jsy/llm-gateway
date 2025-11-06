// Package seed 内嵌种子数据。
//
// litellm_catalog.json 是来自 https://github.com/BerriAI/litellm 的 model_prices_and_context_window.json
// 离线快照（约 1.45MB / 2720 条），用于：
//   - SyncFromBundled：开箱即用、首次启动可立刻使用；
//   - SyncFromRemote 失败时的回退（D1 + D3）。
//
// 想要刷新这份数据，重新下载远程 JSON 覆盖即可，无需改代码。
package seed

import _ "embed"

//go:embed litellm_catalog.json
var LiteLLMCatalogJSON []byte
