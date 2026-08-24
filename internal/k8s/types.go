package k8s

import (
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

// Health はワークロード／Pod／ノードの状態を 4 段階に丸めたもの。
// 目視で「動いている／怪しい／壊れている」を一目で分けるためだけに使う。
type Health int

const (
	HealthOK Health = iota
	HealthProgressing
	HealthWarn
	HealthBad
	HealthDone // Completed（Job など、終了が正常）
)

func (h Health) Symbol() string {
	switch h {
	case HealthOK:
		return "●"
	case HealthProgressing:
		return "◐"
	case HealthWarn:
		return "▲"
	case HealthBad:
		return "✖"
	case HealthDone:
		return "✓"
	}
	return "?"
}

// Pod は一覧表示に必要な範囲だけを抜き出した Pod。
type Pod struct {
	Namespace   string
	Name        string
	UID         types.UID
	Node        string
	Phase       string // Running / Pending / Succeeded / Failed
	Reason      string // ImagePullBackOff など、止まっている理由の要約
	Message     string
	ReadyCount  int
	TotalCount  int
	Restarts    int32
	Created     time.Time
	Health      Health
	Containers  []string
	OwnedBy     WorkloadRef // 由来（空なら裸の Pod）
	IsDaemonSet bool
	IsStatic    bool // static / mirror pod（drain しても残る）
	CPURequest  resource.Quantity
	MemRequest  resource.Quantity
	CPULimit    resource.Quantity
	MemLimit    resource.Quantity
}

// WorkloadRef はワークロードの同定キー。
type WorkloadRef struct {
	Kind      string // Deployment / DaemonSet / StatefulSet / CronJob / Job / Pod
	Namespace string
	Name      string
}

func (r WorkloadRef) Empty() bool { return r.Kind == "" && r.Name == "" }

// Workload は「Pod を束ねている何か」。ストレージや Service は扱わない。
type Workload struct {
	Ref       WorkloadRef
	UID       types.UID
	Desired   int32
	Ready     int32
	Updated   int32
	Available int32
	Created   time.Time
	Images    []string
	Health    Health
	Note      string // Suspended、スケジュール、最終実行など補足
	Pods      []*Pod
	// イベント引き当て用。ReplicaSet / Job など中間オブジェクトの UID も含む。
	RelatedUIDs []types.UID
}

// Node はインフラ視点の 1 台。
type Node struct {
	Name          string
	Ready         bool
	ReadyReason   string
	Unschedulable bool // cordon 済み
	Roles         []string
	Version       string
	Created       time.Time
	InternalIP    string
	Health        Health
	Problems      []string // MemoryPressure など、Ready 以外で立っている条件

	AllocCPU resource.Quantity
	AllocMem resource.Quantity
	AllocPod int64

	ReqCPU resource.Quantity
	ReqMem resource.Quantity
	LimCPU resource.Quantity
	LimMem resource.Quantity

	UsedCPU *resource.Quantity // metrics-server がある場合のみ
	UsedMem *resource.Quantity

	Pods []*Pod

	// drain の検算用
	ActivePods    int // Succeeded/Failed を除く
	DaemonSetPods int
	StaticPods    int
	EvictablePods int // drain でまだ抜けていない Pod の数
	ProblemPods   int
}

// Event は表示に必要な範囲の Event。
type Event struct {
	Time    time.Time
	Type    string // Normal / Warning
	Reason  string
	Object  string // Kind/Name
	Message string
	Count   int32
}

// Snapshot は 1 回のポーリングで取得した内容。
type Snapshot struct {
	Namespaces []string
	Workloads  []*Workload
	Nodes      []*Node
	Err        error
	FetchedAt  time.Time
}
