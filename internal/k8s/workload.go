package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Namespaces は名前空間の一覧。アプリケーション＝名前空間という運用を前提に、
// これが App ビューの最上位になる。
func (c *Client) Namespaces(ctx context.Context) ([]string, error) {
	list, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, ns.Name)
	}
	sort.Strings(out)
	return out, nil
}

// Workloads は 1 名前空間のワークロードと、その配下の Pod を組にして返す。
// Pod の帰属は ownerReferences を辿って決める（ラベルセレクタより正確）。
func (c *Client) Workloads(ctx context.Context, ns string) ([]*Workload, error) {
	pods, err := c.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Pod の取得に失敗: %w", err)
	}
	deploys, err := c.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Deployment の取得に失敗: %w", err)
	}
	daemons, err := c.Clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("DaemonSet の取得に失敗: %w", err)
	}
	statefuls, err := c.Clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("StatefulSet の取得に失敗: %w", err)
	}
	rsets, err := c.Clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("ReplicaSet の取得に失敗: %w", err)
	}
	jobs, err := c.Clientset.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Job の取得に失敗: %w", err)
	}
	crons, err := c.Clientset.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		// CronJob が batch/v1 に無い古いクラスタでも他は見えるようにする。
		crons = &batchv1.CronJobList{}
	}

	byRef := map[WorkloadRef]*Workload{}
	links := map[types.UID]WorkloadRef{} // 中間オブジェクト（ReplicaSet / Job）の UID → 親

	put := func(w *Workload) { byRef[w.Ref] = w }

	for i := range deploys.Items {
		d := &deploys.Items[i]
		w := &Workload{
			Ref:       WorkloadRef{Kind: "Deployment", Namespace: ns, Name: d.Name},
			UID:       d.UID,
			Desired:   ptrInt32(d.Spec.Replicas, 1),
			Ready:     d.Status.ReadyReplicas,
			Updated:   d.Status.UpdatedReplicas,
			Available: d.Status.AvailableReplicas,
			Created:   d.CreationTimestamp.Time,
			Images:    imagesOf(d.Spec.Template.Spec),
		}
		w.RelatedUIDs = []types.UID{d.UID}
		put(w)
	}
	for i := range daemons.Items {
		d := &daemons.Items[i]
		w := &Workload{
			Ref:       WorkloadRef{Kind: "DaemonSet", Namespace: ns, Name: d.Name},
			UID:       d.UID,
			Desired:   d.Status.DesiredNumberScheduled,
			Ready:     d.Status.NumberReady,
			Updated:   d.Status.UpdatedNumberScheduled,
			Available: d.Status.NumberAvailable,
			Created:   d.CreationTimestamp.Time,
			Images:    imagesOf(d.Spec.Template.Spec),
		}
		w.RelatedUIDs = []types.UID{d.UID}
		put(w)
	}
	for i := range statefuls.Items {
		s := &statefuls.Items[i]
		w := &Workload{
			Ref:       WorkloadRef{Kind: "StatefulSet", Namespace: ns, Name: s.Name},
			UID:       s.UID,
			Desired:   ptrInt32(s.Spec.Replicas, 1),
			Ready:     s.Status.ReadyReplicas,
			Updated:   s.Status.UpdatedReplicas,
			Available: s.Status.AvailableReplicas,
			Created:   s.CreationTimestamp.Time,
			Images:    imagesOf(s.Spec.Template.Spec),
		}
		w.RelatedUIDs = []types.UID{s.UID}
		put(w)
	}
	for i := range crons.Items {
		cj := &crons.Items[i]
		w := &Workload{
			Ref:     WorkloadRef{Kind: "CronJob", Namespace: ns, Name: cj.Name},
			UID:     cj.UID,
			Created: cj.CreationTimestamp.Time,
			Images:  imagesOf(cj.Spec.JobTemplate.Spec.Template.Spec),
			Desired: int32(len(cj.Status.Active)),
		}
		note := cj.Spec.Schedule
		if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
			note += "  [suspended]"
		}
		if cj.Status.LastScheduleTime != nil {
			note += "  last=" + ShortAge(cj.Status.LastScheduleTime.Time) + "前"
		}
		w.Note = note
		w.RelatedUIDs = []types.UID{cj.UID}
		put(w)
	}

	// Job：CronJob 配下なら中間オブジェクトとして畳み、単独なら 1 ワークロードとして出す。
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if parent := controllerOf(j.OwnerReferences); parent != nil && parent.Kind == "CronJob" {
			ref := WorkloadRef{Kind: "CronJob", Namespace: ns, Name: parent.Name}
			links[j.UID] = ref
			if w, ok := byRef[ref]; ok {
				w.RelatedUIDs = append(w.RelatedUIDs, j.UID)
			}
			continue
		}
		w := &Workload{
			Ref:     WorkloadRef{Kind: "Job", Namespace: ns, Name: j.Name},
			UID:     j.UID,
			Desired: ptrInt32(j.Spec.Completions, 1),
			Ready:   j.Status.Succeeded,
			Created: j.CreationTimestamp.Time,
			Images:  imagesOf(j.Spec.Template.Spec),
		}
		if j.Status.Failed > 0 {
			w.Note = fmt.Sprintf("failed=%d", j.Status.Failed)
		}
		w.RelatedUIDs = []types.UID{j.UID}
		links[j.UID] = w.Ref
		put(w)
	}

	// ReplicaSet：Deployment 配下なら畳み、単独なら 1 ワークロードとして出す。
	for i := range rsets.Items {
		rs := &rsets.Items[i]
		if parent := controllerOf(rs.OwnerReferences); parent != nil && parent.Kind == "Deployment" {
			ref := WorkloadRef{Kind: "Deployment", Namespace: ns, Name: parent.Name}
			links[rs.UID] = ref
			if w, ok := byRef[ref]; ok {
				w.RelatedUIDs = append(w.RelatedUIDs, rs.UID)
			}
			continue
		}
		if ptrInt32(rs.Spec.Replicas, 1) == 0 && rs.Status.Replicas == 0 {
			continue // 単独 ReplicaSet の残骸は出さない
		}
		w := &Workload{
			Ref:     WorkloadRef{Kind: "ReplicaSet", Namespace: ns, Name: rs.Name},
			UID:     rs.UID,
			Desired: ptrInt32(rs.Spec.Replicas, 1),
			Ready:   rs.Status.ReadyReplicas,
			Created: rs.CreationTimestamp.Time,
			Images:  imagesOf(rs.Spec.Template.Spec),
		}
		w.RelatedUIDs = []types.UID{rs.UID}
		links[rs.UID] = w.Ref
		put(w)
	}

	// Pod を由来へ振り分ける。
	for i := range pods.Items {
		p := convertPod(&pods.Items[i])
		ref := resolveOwner(&pods.Items[i], links)
		if ref.Empty() {
			// 裸の Pod。1 Pod = 1 ワークロードとして扱う。
			ref = WorkloadRef{Kind: "Pod", Namespace: ns, Name: p.Name}
			if _, ok := byRef[ref]; !ok {
				put(&Workload{Ref: ref, UID: p.UID, Desired: 1, Created: p.Created, RelatedUIDs: []types.UID{p.UID}})
			}
		}
		w, ok := byRef[ref]
		if !ok {
			// 親が消えた直後などの取りこぼし。孤児として見せる。
			w = &Workload{Ref: ref, Created: p.Created}
			put(w)
		}
		p.OwnedBy = ref
		w.Pods = append(w.Pods, p)
		w.RelatedUIDs = append(w.RelatedUIDs, p.UID)
	}

	out := make([]*Workload, 0, len(byRef))
	for _, w := range byRef {
		if w.Ref.Kind == "Pod" && len(w.Pods) > 0 {
			w.Ready = int32(boolToInt(w.Pods[0].Health == HealthOK || w.Pods[0].Health == HealthDone))
			if len(w.Images) == 0 {
				w.Images = w.Pods[0].Containers
			}
		}
		sort.Slice(w.Pods, func(i, j int) bool { return w.Pods[i].Name < w.Pods[j].Name })
		w.Health = workloadHealth(w)
		out = append(out, w)
	}
	sortWorkloads(out)
	return out, nil
}

// resolveOwner は Pod の由来を 1 段（ReplicaSet → Deployment、Job → CronJob）辿って決める。
// links に中間オブジェクトが無ければ、その中間オブジェクト自身を由来とする。
func resolveOwner(p *corev1.Pod, links map[types.UID]WorkloadRef) WorkloadRef {
	ns := p.Namespace
	or := controllerOf(p.OwnerReferences)
	if or == nil {
		return WorkloadRef{}
	}
	switch or.Kind {
	case "DaemonSet", "StatefulSet", "Deployment":
		return WorkloadRef{Kind: or.Kind, Namespace: ns, Name: or.Name}
	case "ReplicaSet", "Job":
		if ref, ok := links[or.UID]; ok {
			return ref
		}
		return WorkloadRef{Kind: or.Kind, Namespace: ns, Name: or.Name}
	case "Node":
		return WorkloadRef{Kind: "Static", Namespace: ns, Name: p.Name}
	}
	return WorkloadRef{Kind: or.Kind, Namespace: ns, Name: or.Name}
}

// controllerLinksAllNamespaces は全名前空間の ReplicaSet / Job から
// 「中間オブジェクト UID → 親ワークロード」の対応表を作る。
// インフラ視点で Pod の由来を出すために使う。
func (c *Client) controllerLinksAllNamespaces(ctx context.Context) map[types.UID]WorkloadRef {
	links := map[types.UID]WorkloadRef{}
	if rsets, err := c.Clientset.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range rsets.Items {
			rs := &rsets.Items[i]
			ref := WorkloadRef{Kind: "ReplicaSet", Namespace: rs.Namespace, Name: rs.Name}
			if parent := controllerOf(rs.OwnerReferences); parent != nil && parent.Kind == "Deployment" {
				ref = WorkloadRef{Kind: "Deployment", Namespace: rs.Namespace, Name: parent.Name}
			}
			links[rs.UID] = ref
		}
	}
	if jobs, err := c.Clientset.BatchV1().Jobs("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range jobs.Items {
			j := &jobs.Items[i]
			ref := WorkloadRef{Kind: "Job", Namespace: j.Namespace, Name: j.Name}
			if parent := controllerOf(j.OwnerReferences); parent != nil && parent.Kind == "CronJob" {
				ref = WorkloadRef{Kind: "CronJob", Namespace: j.Namespace, Name: parent.Name}
			}
			links[j.UID] = ref
		}
	}
	return links
}

func workloadHealth(w *Workload) Health {
	worst := HealthOK
	running := 0
	for _, p := range w.Pods {
		if p.Health > worst && p.Health != HealthDone {
			worst = p.Health
		}
		if p.Health == HealthOK {
			running++
		}
	}
	switch w.Ref.Kind {
	case "Job":
		if w.Desired > 0 && w.Ready >= w.Desired {
			return HealthDone
		}
	case "CronJob":
		if len(w.Pods) == 0 {
			return HealthOK // 実行待ち
		}
	}
	if w.Desired == 0 && len(w.Pods) == 0 {
		return HealthOK // 意図的に 0 に絞っている
	}
	if worst == HealthBad {
		return HealthBad
	}
	if int32(running) < w.Desired {
		if worst == HealthOK {
			return HealthProgressing
		}
		return worst
	}
	return worst
}

func sortWorkloads(ws []*Workload) {
	order := map[string]int{"Deployment": 0, "StatefulSet": 1, "DaemonSet": 2, "CronJob": 3, "Job": 4, "ReplicaSet": 5, "Static": 6, "Pod": 7}
	sort.Slice(ws, func(i, j int) bool {
		oi, oj := order[ws[i].Ref.Kind], order[ws[j].Ref.Kind]
		if oi != oj {
			return oi < oj
		}
		return ws[i].Ref.Name < ws[j].Ref.Name
	})
}

func controllerOf(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	if len(refs) > 0 {
		return &refs[0]
	}
	return nil
}

func imagesOf(spec corev1.PodSpec) []string {
	var out []string
	for _, c := range spec.Containers {
		out = append(out, c.Image)
	}
	return out
}

func ptrInt32(p *int32, def int32) int32 {
	if p == nil {
		return def
	}
	return *p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ShortImage は表示用にレジストリとダイジェストを削った短いイメージ名。
func ShortImage(img string) string {
	if i := strings.Index(img, "@"); i > 0 {
		img = img[:i]
	}
	parts := strings.Split(img, "/")
	return parts[len(parts)-1]
}
