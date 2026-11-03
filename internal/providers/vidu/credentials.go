package vidu

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// credentials 是 Vidu 渠道的凭据。
//
// Vidu 只需要一个 API Key（鉴权头 `Authorization: Token <apiKey>`），因此凭据 schema
// 是**单列**。这与 Kling 的两列（access_key/secret_key）不同，见 parseCredentials 的说明。
type credentials struct {
	APIKey string `json:"api_key"`
}

// jsonCredentialKeys 是 JSON 形态凭据里被接受的键（只认 api_key，与仓库其它单 Key 渠道一致：
// internal/controlplane/service/credential.go:16 也从凭据 JSON 里取 `api_key`）。
const jsonCredentialKeys = "api_key"

// parseCredentials 解析 providers.MediaAdapterParams.Credentials。
//
// 契约（providers.MediaAdapterBuilder 注释）：凭证缺失/非法时返回 error，**不返回占位适配器**，
// 这样该渠道不会出现在 MediaAdapterRegistry 里，请求得到明确的 501，而不会拿半个凭证
// 打真实上游产生生成费用。
//
// ⚠️ 这里必须同时接受**两种形态**，原因是本仓库控制面会把单 Key 凭据拆出来再下发：
//
//  1. **裸 API Key**（`vda_xxx` / `sk-xxx`）：这是**生产实际形态**。控制面
//     `provider_snapshot_service` → `internal/controlplane/service/internal_service.go:55`
//     对非 aksk 渠道调用 `extractAPIKey(credentials)`，它会把 `{"api_key":"..."}` 抽成**纯字符串**
//     （`internal/controlplane/service/credential.go:8-21`），gateway 的
//     `provider_snapshot`/`config_loader` 原样塞进 MediaAdapterParams.Credentials。
//     只认 JSON 会导致真机上「凭据解析失败 → 渠道静默消失 → 全部 501」。
//  2. **JSON 对象** `{"api_key":"..."}`：控制面 credential_schema 的原始形态，
//     也用于测试/直连构造（以及控制面将来若变成保留下发的兜底）。
//
// 其余键（region 等）原样忽略——凭据 schema 由控制面定义，适配器不做超出自己需要的校验。
func parseCredentials(raw string) (credentials, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return credentials{}, fmt.Errorf(
			`vidu: 渠道凭据为空，需要 API Key 明文或 {"%s":"..."}`, jsonCredentialKeys)
	}

	// 形态 1：JSON 对象。只有以 '{' 开头才按 JSON 解析——裸 Key 里出现 '{' 不合法，
	// 若真出现，报「不是合法 JSON」比把垃圾当 Key 发给上游更可诊断。
	if strings.HasPrefix(trimmed, "{") {
		var creds credentials
		if err := json.Unmarshal([]byte(trimmed), &creds); err != nil {
			return credentials{}, fmt.Errorf(
				"vidu: 渠道凭据不是合法 JSON（需要 {%q:\"...\"} 或 API Key 明文）: %w",
				jsonCredentialKeys, err)
		}
		key := strings.TrimSpace(creds.APIKey)
		if key == "" {
			return credentials{}, fmt.Errorf("vidu: 渠道凭据缺少 %s", jsonCredentialKeys)
		}
		return credentials{APIKey: key}, nil
	}

	// 形态 2：裸 API Key。
	if err := validateBareAPIKey(trimmed); err != nil {
		return credentials{}, err
	}
	return credentials{APIKey: trimmed}, nil
}

// validateBareAPIKey 校验裸 API Key 的形态。
//
// 只做一件事：**拒绝内部含空白/控制字符的取值**。原因有二：
//   - 合法的 Vidu Key 不含空白，含空白几乎一定是「运维把 JSON 或 YAML 片段贴进了 api_key 字段」
//     （例如 `{"api_key": "..."}` 漏了引号、或粘贴时带了换行）；
//   - 这种值一旦发给上游，上游只会回一个语义模糊的 401，排障成本远高于在构造期直接报错。
//
// 刻意**不**校验长度/前缀：Key 的格式由生数科技掌握，写死前缀会在对方换发 Key 规则时误杀。
func validateBareAPIKey(key string) error {
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New(
				"vidu: 渠道凭据含空白字符（疑似把 JSON/YAML 片段整体贴进了 API Key 字段）")
		}
	}
	return nil
}
