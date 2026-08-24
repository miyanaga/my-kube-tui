package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Nodes はインフラ視点の一覧。全名前空間の Pod を 1 回引いてノードへ割り付ける。
// 「このノードは本当に空になったか」「詰め込めているか」を出すのが目的。
func (c *Client) Nodes(ctx context.Context) ([]*Node, error) {
	nodeList, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Node の取得に失敗: %w", err)
	}
	podList, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Pod の取得に失敗: %w", err)
	}

	links := c.controllerLinksAllNamespaces(ctx)

	byNode := map[string][]*Pod{}
	for i := range podList.Items {
		p := convertPod(&podList.Items[i])
		p.OwnedBy = resolveOwner(&podList.Items[i], links)
		if p.Node == "" {
			byNode[""] = append(byNode[""], p) // 未スケジュール
			continue
		}
		byNode[p.Node] = append(byNode[p.Node], p)
	}

	usage := c.nodeUsage(ctx)

	out := make([]*Node, 0, len(nodeList.Items)+1)
	for i := range nodeList.Items {
		n := &nodeList.Items[i]
		node := &Node{
			Name:          n.Name,
			Unschedulable: n.Spec.Unschedulable,
			Version:       n.Status.NodeInfo.KubeletVersion,
			Created:       n.CreationTimestamp.Time,
			AllocCPU:      n.Status.Allocatable[corev1.ResourceCPU],
			AllocMem:      n.Status.Allocatable[corev1.ResourceMemory],
		}
		if q, ok := n.Status.Allocatable[corev1.ResourcePods]; ok {
			node.AllocPod = q.Value()
		}
		for _, a := range n.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				node.InternalIP = a.Address
			}
		}
		for k := range n.Labels {
			if r := strings.TrimPrefix(k, "node-role.kubernetes.io/"); r != k && r != "" {
				node.Roles = append(node.Roles, r)
			}
		}
		sort.Strings(node.Roles)

		for _, cond := range n.Status.Conditions {
			if cond.Type == corev1.NodeReady {
				node.Ready = cond.Status == corev1.ConditionTrue
				if !node.Ready {
					node.ReadyReason = cond.Reason
				}
				continue
			}
			// Ready 以外は「True なら異常」という条件が並ぶ。
			if cond.Status == corev1.ConditionTrue {
				node.Problems = append(node.Problems, string(cond.Type))
			}
		}

		node.Pods = byNode[n.Name]
		sort.Slice(node.Pods, func(i, j int) bool {
			if node.Pods[i].Namespace != node.Pods[j].Namespace {
				return node.Pods[i].Namespace < node.Pods[j].Namespace
			}
			return node.Pods[i].Name < node.Pods[j].Name
		})

		req := corev1.ResourceList{}
		lim := corev1.ResourceList{}
		for _, p := range node.Pods {
			if p.Terminal() {
				continue
			}
			node.ActivePods++
			addQ(req, corev1.ResourceCPU, p.CPURequest)
			addQ(req, corev1.ResourceMemory, p.MemRequest)
			addQ(lim, corev1.ResourceCPU, p.CPULimit)
			addQ(lim, corev1.ResourceMemory, p.MemLimit)
			switch {
			case p.IsStatic:
				node.StaticPods++
			case p.IsDaemonSet:
				node.DaemonSetPods++
			default:
				node.EvictablePods++
			}
			if p.Health == HealthBad || p.Health == HealthWarn {
				node.ProblemPods++
			}
		}
		node.ReqCPU, node.ReqMem = req[corev1.ResourceCPU], req[corev1.ResourceMemory]
		node.LimCPU, node.LimMem = lim[corev1.ResourceCPU], lim[corev1.ResourceMemory]

		if u, ok := usage[n.Name]; ok {
			cpu, mem := u.cpu, u.mem
			node.UsedCPU, node.UsedMem = &cpu, &mem
		}

		switch {
		case !node.Ready:
			node.Health = HealthBad
		case node.Unschedulable:
			node.Health = HealthWarn
		case len(node.Problems) > 0:
			node.Health = HealthWarn
		default:
			node.Health = HealthOK
		}
		out = append(out, node)
	}

	// どのノードにも乗れていない Pod は「置き場が無い」という別種の問題なので、
	// 疑似ノードとして必ず見えるようにする。
	if pending := byNode[""]; len(pending) > 0 {
		n := &Node{Name: "(未スケジュール)", Ready: false, Health: HealthBad, Pods: pending}
		for _, p := range pending {
			if !p.Terminal() {
				n.ActivePods++
				n.EvictablePods++
				n.ProblemPods++
			}
		}
		out = append(out, n)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type usageEntry struct{ cpu, mem resource.Quantity }

func (c *Client) nodeUsage(ctx context.Context) map[string]usageEntry {
	out := map[string]usageEntry{}
	if c.Metrics == nil {
		return out
	}
	list, err := c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return out // metrics-server 未導入。requests ベースの表示だけで進む。
	}
	for _, m := range list.Items {
		out[m.Name] = usageEntry{cpu: m.Usage[corev1.ResourceCPU], mem: m.Usage[corev1.ResourceMemory]}
	}
	return out
}

func addQ(l corev1.ResourceList, name corev1.ResourceName, q resource.Quantity) {
	cur := l[name]
	cur.Add(q)
	l[name] = cur
}

// DrainStatus は cordon 済みノードが本当に空になったかを 1 行で返す。
// drain したつもりで残っている Pod を見落とさないための検算。
func (n *Node) DrainStatus() (done bool, text string) {
	if !n.Unschedulable {
		return false, ""
	}
	if n.EvictablePods == 0 {
		return true, fmt.Sprintf("drain 完了：退避対象は残っていません（DaemonSet %d / static %d は残って正常）", n.DaemonSetPods, n.StaticPods)
	}
	return false, fmt.Sprintf("drain 未完了：退避対象の Pod が %d 個残っています（DaemonSet %d / static %d は対象外）", n.EvictablePods, n.DaemonSetPods, n.StaticPods)
}

// Pct は割合（0-100）。分母が 0 なら -1。
func Pct(used, total resource.Quantity) float64 {
	t := total.AsApproximateFloat64()
	if t <= 0 {
		return -1
	}
	return used.AsApproximateFloat64() / t * 100
}
