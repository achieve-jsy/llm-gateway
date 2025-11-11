package scheduler

import "sync"

// RouteTable holds the in-memory group routing configuration with thread-safe access.
type RouteTable struct {
	mu   sync.RWMutex
	data map[string]*GroupRoutingConfig
}

// NewRouteTable creates an empty RouteTable.
func NewRouteTable() *RouteTable {
	return &RouteTable{
		data: make(map[string]*GroupRoutingConfig),
	}
}

// Get returns the GroupRoutingConfig for the given group name.
// Returns nil if the group is not found.
func (rt *RouteTable) Get(groupName string) *GroupRoutingConfig {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.data[groupName]
}

// GetAllModels collects and returns all ModelRoutingEntry across all groups.
func (rt *RouteTable) GetAllModels() []*ModelRoutingEntry {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var allModels []*ModelRoutingEntry
	for _, group := range rt.data {
		allModels = append(allModels, group.Models...)
	}
	return allModels
}

// ReplaceAll replaces the entire route table with data from a ControlPlaneConfig.
// Groups with Enabled=false are filtered out. Models are matched to groups by GroupID.
func (rt *RouteTable) ReplaceAll(config *ControlPlaneConfig) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	newData := make(map[string]*GroupRoutingConfig)

	if config == nil {
		rt.data = newData
		return
	}

	// Build model lookup by group ID
	groupModels := make(map[uint][]*ModelRoutingEntry)
	for _, m := range config.Models {
		groupModels[m.GroupID] = append(groupModels[m.GroupID], &ModelRoutingEntry{
			ModelID:     m.ModelID,
			ModelName:   m.ModelName,
			ChannelType: m.ChannelType,
			Endpoint:    m.Endpoint,
			Priority:    derefPriority(m.Priority),
			Weight:      derefWeight(m.Weight),
		})
	}

	for _, g := range config.Groups {
		if !g.Enabled {
			continue
		}
		models := groupModels[g.ID]
		if models == nil {
			models = []*ModelRoutingEntry{}
		}
		newData[g.Name] = &GroupRoutingConfig{
			GroupID:   g.ID,
			GroupName: g.Name,
			Strategy:  g.Strategy,
			Enabled:   g.Enabled,
			Models:    models,
		}
	}

	rt.data = newData
}

func derefPriority(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefWeight(w *int) int {
	if w == nil {
		return 0
	}
	return *w
}
