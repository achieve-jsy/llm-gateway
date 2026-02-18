package authz

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/binding"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
)

// IsServiceKey 判断是否为部门级程序 Service Key。
func IsServiceKey(id *gwmodel.UserIdentity) bool {
	return id != nil && id.KeyType == "service"
}

// ResolveVisibleGroupNames 解析当前身份可见的模型组名列表。
// Service Key 严格限于 verify-key 下发的 usable_groups（部门天花板），绝不回退到全局 ListAllGroups。
func ResolveVisibleGroupNames(ctx context.Context, id *gwmodel.UserIdentity, client *binding.Client) ([]string, error) {
	if id == nil || id.KeyID == "" {
		return nil, nil
	}

	if IsServiceKey(id) {
		if len(id.UsableGroups) == 0 {
			return nil, nil
		}
		if id.AccessAllGroups {
			return cloneStrings(id.UsableGroups), nil
		}
		if client == nil {
			return nil, nil
		}
		groups, err := client.GetKeyGroups(ctx, id.KeyID)
		if err != nil {
			return nil, err
		}
		return intersectStrings(id.UsableGroups, boundGroupNames(groups)), nil
	}

	if len(id.UsableGroups) > 0 {
		if id.AccessAllGroups {
			return cloneStrings(id.UsableGroups), nil
		}
		if client == nil {
			return nil, nil
		}
		groups, err := client.GetKeyGroups(ctx, id.KeyID)
		if err != nil {
			return nil, err
		}
		return intersectStrings(id.UsableGroups, boundGroupNames(groups)), nil
	}

	if id.AccessAllGroups {
		if client == nil {
			return nil, nil
		}
		return client.ListAllGroups(ctx)
	}

	if client == nil {
		return nil, nil
	}
	groups, err := client.GetKeyGroups(ctx, id.KeyID)
	if err != nil {
		return nil, err
	}
	return boundGroupNames(groups), nil
}

// AuthorizeGroupAccess 校验目标模型组是否在当前身份可用范围内。
func AuthorizeGroupAccess(ctx context.Context, id *gwmodel.UserIdentity, client *binding.Client, groupName string) error {
	if id == nil || id.KeyID == "" {
		return nil
	}

	if IsServiceKey(id) {
		if len(id.UsableGroups) == 0 {
			return gwmodel.ErrModelGroupUnavailable
		}
		if !containsString(id.UsableGroups, groupName) {
			return gwmodel.ErrModelGroupUnauthorized
		}
		return nil
	}

	if len(id.UsableGroups) > 0 {
		if !containsString(id.UsableGroups, groupName) {
			return gwmodel.ErrModelGroupUnauthorized
		}
		if !id.AccessAllGroups && client != nil {
			groups, err := client.GetKeyGroups(ctx, id.KeyID)
			if err != nil {
				return gwmodel.ErrModelGroupUnavailable
			}
			if !binding.ContainsGroup(groups, groupName) {
				return gwmodel.ErrModelGroupUnauthorized
			}
		}
		return nil
	}

	if !id.AccessAllGroups && client != nil {
		groups, err := client.GetKeyGroups(ctx, id.KeyID)
		if err != nil {
			return gwmodel.ErrModelGroupUnavailable
		}
		if !binding.ContainsGroup(groups, groupName) {
			return gwmodel.ErrModelGroupUnavailable
		}
	}
	return nil
}

func boundGroupNames(groups []binding.BoundGroup) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Enabled && g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return names
}

func intersectStrings(ceiling, subset []string) []string {
	ceilSet := make(map[string]struct{}, len(ceiling))
	for _, s := range ceiling {
		ceilSet[s] = struct{}{}
	}
	out := make([]string, 0, len(subset))
	for _, s := range subset {
		if _, ok := ceilSet[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
