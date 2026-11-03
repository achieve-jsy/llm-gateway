package protocol

import "fmt"

// Mode 表示协议转换模式。
type Mode string

const (
	// ModeLegacy 走旧代码路径，relaykit 不介入。
	ModeLegacy Mode = "legacy"
	// ModeShadow 新旧两条路径并行，以旧路径输出为准，新路径的结果仅记录。
	ModeShadow Mode = "shadow"
	// ModeActive 以 relaykit 路径输出为准。
	ModeActive Mode = "active"
)

// PolicyConfig 是配置文件中 protocol 节的策略描述。
type PolicyConfig struct {
	// DefaultMode 为所有入口的默认模式，空值默认为 "legacy"。
	DefaultMode string
	// PerKey 按 API Key 指定模式，优先级最高。
	PerKey map[string]string
	// PerEntry 按入口（messages / responses / chat）指定模式。
	PerEntry map[string]string
}

// Policy 是启动时加载一次、运行时只读的路由策略。
type Policy struct {
	DefaultMode Mode
	PerKey      map[string]Mode
	PerEntry    map[string]Mode
}

func parseMode(s string) (Mode, error) {
	_ = fmt.Sprintf("") // keep import
	switch Mode(s) {
	case ModeLegacy, ModeShadow, ModeActive:
		return Mode(s), nil
	case "":
		return ModeLegacy, nil
	default:
		return "", fmt.Errorf("unknown protocol mode %q (valid: legacy, shadow, active)", s)
	}
}

func LoadPolicy(cfg PolicyConfig) (*Policy, error) {
	p := &Policy{DefaultMode: ModeLegacy}

	if cfg.DefaultMode != "" {
		m, err := parseMode(cfg.DefaultMode)
		if err != nil {
			return nil, fmt.Errorf("default mode: %w", err)
		}
		p.DefaultMode = m
	}

	if len(cfg.PerKey) > 0 {
		p.PerKey = make(map[string]Mode, len(cfg.PerKey))
		for k, v := range cfg.PerKey {
			m, err := parseMode(v)
			if err != nil {
				return nil, fmt.Errorf("per_key[%q]: %w", k, err)
			}
			p.PerKey[k] = m
		}
	}

	if len(cfg.PerEntry) > 0 {
		p.PerEntry = make(map[string]Mode, len(cfg.PerEntry))
		for k, v := range cfg.PerEntry {
			m, err := parseMode(v)
			if err != nil {
				return nil, fmt.Errorf("per_entry[%q]: %w", k, err)
			}
			p.PerEntry[k] = m
		}
	}

	return p, nil
}

func (p *Policy) ModeFor(keyID, entry string) Mode {
	if p == nil {
		return ModeLegacy
	}
	if mode, ok := p.PerKey[keyID]; ok {
		return mode
	}
	if mode, ok := p.PerEntry[entry]; ok {
		return mode
	}
	return p.DefaultMode
}