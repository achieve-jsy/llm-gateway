package report

import (
	"math"
	"sort"
)

// 本文件实现 docs §9.5 的分群（§7）。
// 纯函数：对成员的五种情况归因占比向量做聚类，产出 MemberCluster[]。
//   - 人少（< MinForKMeans）：不上算法，按 dominant_issue 直接分色。
//   - 人多（>= MinForKMeans）：对 [五种情况 attributed 占比] 向量做 KMeans（k≈情况数）。

// clusterMember 是分群所需的单个成员数据（不可变输入）。
type clusterMember struct {
	id  string
	dom string
	vec []float64
}

// ClusterParams 分群配置。
type ClusterParams struct {
	// MinForKMeans 人数超过此值才用 KMeans；否则按 dominant_issue 分色。默认 30。
	MinForKMeans int
	// K KMeans 聚类数（≈情况数）。默认 5。
	K int
	// MaxIter KMeans 最大迭代。默认 100。
	MaxIter int
}

// DefaultClusterParams 返回默认分群配置（min30 / k5 / iter100）。
func DefaultClusterParams() ClusterParams {
	return ClusterParams{MinForKMeans: 30, K: 5, MaxIter: 100}
}

// ClusterMembers 计算成员分群。
//   - 人少（len(inputs) < MinForKMeans）：按每个成员的 dominant_issue 分组，label=dominant_issue（含 "normal"）。
//   - 人多：KMeans 聚类，label=该簇质心五种情况中占比最大者（全零 → "normal"）。
func ClusterMembers(inputs []MemberQuadrantInput, params ClusterParams) []MemberCluster {
	members := make([]clusterMember, 0, len(inputs))
	for _, in := range inputs {
		dom, _ := DominantIssue(in.IssueAttributed, 0)
		members = append(members, clusterMember{id: in.UserID, dom: dom, vec: issueRatioVector(in.IssueAttributed)})
	}
	if len(members) == 0 {
		return nil
	}
	if len(members) < params.MinForKMeans {
		return clusterByDominant(members)
	}
	return kmeans(members, params)
}

// issueRatioVector 构造 [五种情况 attributed 占比] 向量（维度按 IssueTypeOrder）。
// 占比 = 各情况归因 / 该成员五种归因合计；无任何归因 → 全零向量（normal 簇）。
func issueRatioVector(attributed map[string]float64) []float64 {
	total := issueTotal(attributed)
	v := make([]float64, len(IssueTypeOrder))
	if total <= 0 {
		return v
	}
	for i, it := range IssueTypeOrder {
		v[i] = attributed[it] / total
	}
	return v
}

// clusterByDominant 按 dominant_issue 分色分群（人少场景，不上算法）。输出 label 按字典序保证确定性。
func clusterByDominant(members []clusterMember) []MemberCluster {
	groups := map[string][]string{}
	labels := []string{}
	for _, m := range members {
		if _, ok := groups[m.dom]; !ok {
			labels = append(labels, m.dom)
		}
		groups[m.dom] = append(groups[m.dom], m.id)
	}
	sort.Strings(labels)
	clusters := make([]MemberCluster, 0, len(labels))
	for _, lab := range labels {
		ids := groups[lab]
		clusters = append(clusters, MemberCluster{Label: lab, MemberIDs: ids})
	}
	return clusters
}

// kmeans 对成员的五种归因占比向量做 KMeans（Lloyd 算法）。
// 种子用「最远点优先」确定性初始化；label 取簇质心占比最大者（全零 → normal）。
func kmeans(members []clusterMember, params ClusterParams) []MemberCluster {
	n := len(members)
	k := params.K
	if k < 1 {
		k = 1
	}
	if k > n {
		k = n
	}
	dims := len(members[0].vec)

	// 确定性最远点优先初始化 centroids。
	centroids := make([][]float64, 0, k)
	centroids = append(centroids, append([]float64(nil), members[0].vec...))
	for len(centroids) < k {
		farthest := -1
		farDist := -1.0
		for i, m := range members {
			nd := nearestCentroidDist(m.vec, centroids)
			if nd > farDist {
				farDist = nd
				farthest = i
			}
		}
		centroids = append(centroids, append([]float64(nil), members[farthest].vec...))
	}

	assign := make([]int, n)
	for iter := 0; iter < params.MaxIter; iter++ {
		changed := false
		for i, m := range members {
			best := 0
			bestD := dist(m.vec, centroids[0])
			for c := 1; c < len(centroids); c++ {
				d := dist(m.vec, centroids[c])
				if d < bestD {
					bestD = d
					best = c
				}
			}
			if assign[i] != best {
				assign[i] = best
				changed = true
			}
		}
		centroids = recomputeCentroids(members, assign, len(centroids), dims)
		if !changed {
			break
		}
	}

	byCentroid := map[int][]string{}
	labels := make([]int, 0)
	for i, m := range members {
		c := assign[i]
		if _, ok := byCentroid[c]; !ok {
			labels = append(labels, c)
		}
		byCentroid[c] = append(byCentroid[c], m.id)
	}
	sort.Ints(labels)
	clusters := make([]MemberCluster, 0, len(labels))
	for _, c := range labels {
		ids := byCentroid[c]
		clusters = append(clusters, MemberCluster{
			Label:     centroidLabel(centroids[c]),
			MemberIDs: ids,
		})
	}
	return clusters
}

// nearestCentroidDist 返回点到最近质心的距离平方（用于最远点初始化）。
func nearestCentroidDist(v []float64, centroids [][]float64) float64 {
	best := math.Inf(1)
	for _, c := range centroids {
		if d := dist(v, c); d < best {
			best = d
		}
	}
	return best
}

// recomputeCentroids 重新计算各簇质心（均值）；簇空则保留原质心。
func recomputeCentroids(members []clusterMember, assign []int, k, dims int) [][]float64 {
	sums := make([][]float64, k)
	counts := make([]int, k)
	for i := 0; i < k; i++ {
		sums[i] = make([]float64, dims)
	}
	for i, m := range members {
		c := assign[i]
		counts[c]++
		for d := 0; d < dims; d++ {
			sums[c][d] += m.vec[d]
		}
	}
	out := make([][]float64, k)
	for c := 0; c < k; c++ {
		if counts[c] == 0 {
			out[c] = make([]float64, dims)
			continue
		}
		centroid := make([]float64, dims)
		for d := 0; d < dims; d++ {
			centroid[d] = sums[c][d] / float64(counts[c])
		}
		out[c] = centroid
	}
	return out
}

// centroidLabel 取簇质心五种情况占比最大者的 issue type；质心全零 → normal。
func centroidLabel(centroid []float64) string {
	best := IssueTypeNormal
	maxV := 0.0
	for i, it := range IssueTypeOrder {
		if centroid[i] > maxV {
			maxV = centroid[i]
			best = it
		}
	}
	return best
}

// dist 欧氏距离。
func dist(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		d := a[i] - b[i]
		s += d * d
	}
	return math.Sqrt(s)
}
