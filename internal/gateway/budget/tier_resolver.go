package budget

import "github.com/google/uuid"

type TierResolver interface {
	Resolve(groupName string) (TierContext, bool)
}

type SnapshotTierResolver struct {
	holder *SnapshotHolder
}

func NewTierResolver(holder *SnapshotHolder) *SnapshotTierResolver {
	return &SnapshotTierResolver{holder: holder}
}

func (r *SnapshotTierResolver) Resolve(groupName string) (TierContext, bool) {
	if r == nil || r.holder == nil {
		return TierContext{}, false
	}
	snapshot := r.holder.Get()
	if snapshot == nil {
		return TierContext{}, false
	}
	entry, ok := snapshot.GroupTiers[groupName]
	if !ok || entry.TierID == uuid.Nil {
		return TierContext{}, false
	}
	return TierContext{
		TierID:              entry.TierID.String(),
		TierName:            entry.TierName,
		GroupName:           groupName,
		SnapshotRevision:    snapshot.Revision,
		AccountingEligible:  entry.Enabled,
		EnforcementEligible: entry.Enabled,
	}, true
}
