package service

import "encoding/json"

func extractAPIKey(credentialJSON string) string {
	var m map[string]string
	if json.Unmarshal([]byte(credentialJSON), &m) == nil {
		if key, ok := m["api_key"]; ok && key != "" {
			return key
		}
	}
	return credentialJSON
}
