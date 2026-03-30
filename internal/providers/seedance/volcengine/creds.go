package volcengine

import (
	"encoding/json"
	"strings"
)

// AuthType 方舟官方渠道鉴权方式。
type AuthType string

const (
	// AuthAPIKey 方舟 API Key（Bearer）。
	AuthAPIKey AuthType = "api_key"
	// AuthAKSK IAM Access Key / Secret Key（HMAC-SHA256）。
	AuthAKSK AuthType = "aksk"
)

// Creds 解析后的火山官方凭证（含显式或推断的 auth_type）。
type Creds struct {
	AuthType  AuthType
	APIKey    string
	AccessKey string
	SecretKey string
	Region    string
}

// ParseCreds 解析渠道凭证。
// 支持完整 JSON（含 auth_type）或 snapshot 抽出的裸 api_key 字符串。
// 无 auth_type 时按字段推断；Normalize 后只保留所选方式对应字段。
func ParseCreds(raw string) Creds {
	c := Creds{Region: "cn-beijing"}
	s := strings.TrimSpace(raw)
	if s == "" {
		return c
	}
	if !strings.HasPrefix(s, "{") {
		c.AuthType = AuthAPIKey
		c.APIKey = s
		return c.Normalized()
	}

	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return c
	}

	c.APIKey = firstNonEmpty(m["api_key"], m["ark_api_key"])
	c.AccessKey = firstNonEmpty(m["access_key"], m["access_key_id"], m["ak"])
	c.SecretKey = firstNonEmpty(m["secret_key"], m["secret_access_key"], m["sk"])
	c.Region = firstNonEmpty(m["region"], "cn-beijing")

	switch AuthType(strings.ToLower(strings.TrimSpace(m["auth_type"]))) {
	case AuthAPIKey:
		c.AuthType = AuthAPIKey
	case AuthAKSK, AuthType("iam"), AuthType("hmac"):
		c.AuthType = AuthAKSK
	default:
		if c.APIKey != "" {
			c.AuthType = AuthAPIKey
		} else if c.AccessKey != "" && c.SecretKey != "" {
			c.AuthType = AuthAKSK
		}
	}
	return c.Normalized()
}

// Normalized 按 AuthType 清空无关字段，避免静默混用。
// 若 AuthType 为空则按字段推断（兼容直接构造 Creds 的调用方）。
func (c Creds) Normalized() Creds {
	out := c
	if out.Region == "" {
		out.Region = "cn-beijing"
	}
	if out.AuthType == "" {
		if out.APIKey != "" {
			out.AuthType = AuthAPIKey
		} else if out.AccessKey != "" && out.SecretKey != "" {
			out.AuthType = AuthAKSK
		}
	}
	switch out.AuthType {
	case AuthAPIKey:
		out.AccessKey = ""
		out.SecretKey = ""
	case AuthAKSK:
		out.APIKey = ""
	}
	return out
}

// HasAuth 所选鉴权方式的必填字段是否齐全。
func (c Creds) HasAuth() bool {
	n := c.Normalized()
	switch n.AuthType {
	case AuthAPIKey:
		return n.APIKey != ""
	case AuthAKSK:
		return n.AccessKey != "" && n.SecretKey != ""
	default:
		return false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
