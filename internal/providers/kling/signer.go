package kling

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// Kling 的 JWT 参数（逐字对齐上游与 New API 的 Go 实现
// `relay/channel/task/kling/adaptor.go:294-320`）：
// iss=accessKey、exp=now+1800、nbf=now-5、签名算法 HS256（密钥=secretKey）、header.typ=JWT。
const (
	// jwtTTLSeconds 是签发 token 的有效期（秒）。取 30 分钟：远大于一次提交/查询的耗时，
	// 又足够短以限制 token 泄露窗口。
	jwtTTLSeconds = 1800
	// jwtNotBeforeSkewSeconds 是 nbf 的回拨量（秒），用于容忍上下游之间的时钟偏差。
	jwtNotBeforeSkewSeconds = 5
)

// credentials 是 Kling 渠道的凭据 schema。
//
// ⚠️ 我们控制面的 `credential_schema` 是 **两列** `access_key` / `secret_key`
// （`docs/media-channels-integration-design.md` §536），直接各自取值即可；
// **不要**照抄 New API 插件的单字段 `accessKey|secretKey` 拼接约定
// （那是 New API 把两段塞进一个 api_key 字段的历史包袱）。
type credentials struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// parseCredentials 解析 providers.MediaAdapterParams.Credentials 这段原始 JSON。
//
// 契约（见 providers.MediaAdapterBuilder 注释）：凭证缺失/非法时返回 error 而不是占位适配器，
// 这样该渠道不会出现在 MediaAdapterRegistry 里，请求得到明确的 501，而不会拿半个凭证
// 打真实上游产生生成费用。
//
// 只认 `access_key` / `secret_key` 两个键；其余键（如 region）原样忽略——凭据 schema
// 由控制面定义，适配器不做超出自己需要的校验。
func parseCredentials(raw string) (credentials, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return credentials{}, errors.New(`kling: 渠道凭据为空，需要 {"access_key":"...","secret_key":"..."}`)
	}

	var creds credentials
	if err := json.Unmarshal([]byte(trimmed), &creds); err != nil {
		return credentials{}, fmt.Errorf("kling: 渠道凭据不是合法 JSON（需要 access_key/secret_key 两列）: %w", err)
	}
	creds.AccessKey = strings.TrimSpace(creds.AccessKey)
	creds.SecretKey = strings.TrimSpace(creds.SecretKey)
	if creds.AccessKey == "" {
		return credentials{}, errors.New("kling: 渠道凭据缺少 access_key")
	}
	if creds.SecretKey == "" {
		return credentials{}, errors.New("kling: 渠道凭据缺少 secret_key")
	}
	return creds, nil
}

// signJWT 用 accessKey/secretKey 签发 Kling 鉴权用的 HS256 JWT。
//
// 用 github.com/golang-jwt/jwt/v5（仓库已 vendored，见 vendor/modules.txt:301）
// 而不是手写 base64url 拼接：签名/编码的边界情况（padding、URL 安全字符集、
// header 字段顺序）由库保证，测试里也能用同一个库独立验签。
//
// now 由调用方注入（adapter.now 字段），便于将来做确定性测试与时钟漂移诊断。
func signJWT(accessKey, secretKey string, now time.Time) (string, error) {
	nowUnix := now.Unix()
	claims := jwt.MapClaims{
		"iss": accessKey,
		"exp": nowUnix + jwtTTLSeconds,
		"nbf": nowUnix - jwtNotBeforeSkewSeconds,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Kling 要求 header 里带 typ=JWT（jwt/v5 默认写 typ=JWT，这里显式设置以固定契约）。
	token.Header["typ"] = "JWT"

	signed, err := token.SignedString([]byte(secretKey))
	if err != nil {
		return "", fmt.Errorf("kling: 签发鉴权 JWT 失败: %w", err)
	}
	return signed, nil
}
