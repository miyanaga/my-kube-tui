package k8s

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// 起動そのものに失敗している状態。イメージや設定の間違いはここに出る。
var fatalWaitReasons = map[string]bool{
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"InvalidImageName":           true,
	"CrashLoopBackOff":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"RunContainerError":          true,
	"ErrImageNeverPull":          true,
	"CriticalError":              true,
}

// convertPod は corev1.Pod を表示用の Pod に落とす。
// 「なぜ動いていないか」を Reason 1 行に凝縮するのがここの仕事。
func convertPod(p *corev1.Pod) *Pod {
	out := &Pod{
		Namespace:  p.Namespace,
		Name:       p.Name,
		UID:        p.UID,
		Node:       p.Spec.NodeName,
		Phase:      string(p.Status.Phase),
		Created:    p.CreationTimestamp.Time,
		TotalCount: len(p.Spec.Containers),
	}
	for _, c := range p.Spec.Containers {
		out.Containers = append(out.Containers, c.Name)
	}

	reason := string(p.Status.Phase)
	if p.Status.Reason != "" {
		reason = p.Status.Reason // Evicted / NodeAffinity など
	}
	fatal := false
	waiting := false

	for _, cs := range p.Status.InitContainerStatuses {
		switch {
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0:
			continue
		case cs.State.Terminated != nil:
			r := cs.State.Terminated.Reason
			if r == "" {
				r = "InitError"
			}
			reason, fatal = "Init:"+r, true
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + cs.State.Waiting.Reason
			waiting = true
			fatal = fatal || fatalWaitReasons[cs.State.Waiting.Reason]
		}
		out.Restarts += cs.RestartCount
	}

	for _, cs := range p.Status.ContainerStatuses {
		out.Restarts += cs.RestartCount
		if cs.Ready {
			out.ReadyCount++
		}
		if fatal {
			continue
		}
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			reason = cs.State.Waiting.Reason
			waiting = true
			fatal = fatalWaitReasons[cs.State.Waiting.Reason]
		case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
			if p.Status.Phase != corev1.PodSucceeded {
				reason = cs.State.Terminated.Reason
				fatal = cs.State.Terminated.ExitCode != 0
			}
		}
	}

	if p.DeletionTimestamp != nil {
		reason = "Terminating"
	}
	out.Reason = reason
	out.Message = firstMessage(p)

	switch {
	case p.DeletionTimestamp != nil:
		out.Health = HealthWarn
	case p.Status.Phase == corev1.PodSucceeded:
		out.Health = HealthDone
	case p.Status.Phase == corev1.PodFailed || fatal:
		out.Health = HealthBad
	case p.Status.Phase == corev1.PodRunning && out.ReadyCount == out.TotalCount && out.TotalCount > 0:
		out.Health = HealthOK
	case waiting || p.Status.Phase == corev1.PodPending:
		out.Health = HealthProgressing
	default:
		out.Health = HealthWarn
	}
	// Running なのに Ready でないコンテナがある＝ヘルスチェックを通っていない。
	if out.Health == HealthProgressing && p.Status.Phase == corev1.PodRunning {
		out.Health = HealthWarn
	}

	for _, or := range p.OwnerReferences {
		if or.Kind == "DaemonSet" {
			out.IsDaemonSet = true
		}
		if or.Kind == "Node" {
			out.IsStatic = true
		}
	}
	if _, ok := p.Annotations[corev1.MirrorPodAnnotationKey]; ok {
		out.IsStatic = true
	}

	req, lim := podResources(p)
	out.CPURequest, out.MemRequest = req[corev1.ResourceCPU], req[corev1.ResourceMemory]
	out.CPULimit, out.MemLimit = lim[corev1.ResourceCPU], lim[corev1.ResourceMemory]
	return out
}

func firstMessage(p *corev1.Pod) string {
	if p.Status.Message != "" {
		return p.Status.Message
	}
	for _, cs := range append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		if cs.State.Waiting != nil && cs.State.Waiting.Message != "" {
			return strings.TrimSpace(cs.State.Waiting.Message)
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			return strings.TrimSpace(cs.State.Terminated.Message)
		}
	}
	for _, c := range p.Status.Conditions {
		if c.Status == corev1.ConditionFalse && c.Message != "" {
			return strings.TrimSpace(c.Message)
		}
	}
	return ""
}

// podResources は Pod の実効 requests / limits を求める。
// 通常コンテナの合計と init コンテナの最大値の大きい方、というスケジューラの定義に合わせる。
func podResources(p *corev1.Pod) (req, lim corev1.ResourceList) {
	req = corev1.ResourceList{}
	lim = corev1.ResourceList{}
	add := func(dst corev1.ResourceList, src corev1.ResourceList) {
		for name, q := range src {
			if cur, ok := dst[name]; ok {
				cur.Add(q)
				dst[name] = cur
			} else {
				dst[name] = q.DeepCopy()
			}
		}
	}
	maxInto := func(dst corev1.ResourceList, src corev1.ResourceList) {
		for name, q := range src {
			if cur, ok := dst[name]; !ok || q.Cmp(cur) > 0 {
				dst[name] = q.DeepCopy()
			}
		}
	}
	for _, c := range p.Spec.Containers {
		add(req, c.Resources.Requests)
		add(lim, c.Resources.Limits)
	}
	initReq, initLim := corev1.ResourceList{}, corev1.ResourceList{}
	for _, c := range p.Spec.InitContainers {
		maxInto(initReq, c.Resources.Requests)
		maxInto(initLim, c.Resources.Limits)
	}
	maxInto(req, initReq)
	maxInto(lim, initLim)
	if p.Spec.Overhead != nil {
		add(req, p.Spec.Overhead)
		add(lim, p.Spec.Overhead)
	}
	ensure := func(l corev1.ResourceList) {
		for _, n := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if _, ok := l[n]; !ok {
				l[n] = *resource.NewQuantity(0, resource.DecimalSI)
			}
		}
	}
	ensure(req)
	ensure(lim)
	return req, lim
}

// Terminal は Pod が終了済み（drain の残存数から除いてよい）かどうか。
func (p *Pod) Terminal() bool {
	return p.Phase == string(corev1.PodSucceeded) || p.Phase == string(corev1.PodFailed)
}
