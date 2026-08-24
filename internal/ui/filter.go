package ui

import (
	"strconv"
	"strings"

	"github.com/miyanaga/my-kube-tui/internal/k8s"
)

func match(s, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(q))
}

func (m *Model) visibleNamespaces() []string {
	q := m.filters[paneNS]
	if q == "" {
		return m.namespaces
	}
	out := make([]string, 0, len(m.namespaces))
	for _, s := range m.namespaces {
		if match(s, q) {
			out = append(out, s)
		}
	}
	return out
}

func (m *Model) visibleWorkloads() []*k8s.Workload {
	q := m.filters[paneWL]
	if q == "" {
		return m.workloads
	}
	out := make([]*k8s.Workload, 0, len(m.workloads))
	for _, w := range m.workloads {
		if match(w.Ref.Name, q) || match(w.Ref.Kind, q) {
			out = append(out, w)
		}
	}
	return out
}

func (m *Model) visibleNodes() []*k8s.Node {
	q := m.filters[paneNode]
	if q == "" {
		return m.nodes
	}
	out := make([]*k8s.Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		if match(n.Name, q) || match(strings.Join(n.Roles, ","), q) {
			out = append(out, n)
		}
	}
	return out
}

// visibleDetailPods は詳細ペインに出す Pod（App なら選択中ワークロード、Infra なら選択中ノード）。
func (m *Model) visibleDetailPods() []*k8s.Pod {
	var src []*k8s.Pod
	var q string
	if m.mode == ModeApp {
		w := m.currentWorkload()
		if w == nil {
			return nil
		}
		src, q = w.Pods, m.filters[m.detailFilterKey()]
	} else {
		n := m.currentNode()
		if n == nil {
			return nil
		}
		src, q = n.Pods, m.filters[m.detailFilterKey()]
	}
	if q == "" {
		return src
	}
	out := make([]*k8s.Pod, 0, len(src))
	for _, p := range src {
		if match(p.Name, q) || match(p.Namespace, q) || match(p.Reason, q) || match(p.Node, q) {
			out = append(out, p)
		}
	}
	return out
}

func (m *Model) visibleEvents() []k8s.Event {
	q := m.filters[m.detailFilterKey()]
	if q == "" {
		return m.events
	}
	out := make([]k8s.Event, 0, len(m.events))
	for _, e := range m.events {
		if match(e.Reason, q) || match(e.Message, q) || match(e.Object, q) {
			out = append(out, e)
		}
	}
	return out
}

func (m *Model) visibleLogLines() []k8s.LogLine {
	q := m.filters[m.detailFilterKey()]
	if q == "" {
		return m.logLines
	}
	out := make([]k8s.LogLine, 0, len(m.logLines))
	for _, l := range m.logLines {
		if match(l.Text, q) || match(l.Pod, q) {
			out = append(out, l)
		}
	}
	return out
}

// detailFilterKey は詳細ペインのフィルタをタブごとに分けるためのキー。
// Pod 一覧の絞り込みがログの絞り込みに漏れないようにする。
func (m *Model) detailFilterKey() paneID {
	if m.mode == ModeApp {
		return paneID("detail" + strconv.Itoa(m.tab))
	}
	return paneID("ndetail" + strconv.Itoa(m.tab))
}
