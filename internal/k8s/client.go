// Package k8s は TUI が必要とする範囲に絞った Kubernetes 参照クライアント。
// 書き込み系の操作は一切持たない（ビューアーに徹する）。
package k8s

import (
	"fmt"
	"sort"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Contexts は ~/.kube/config（KUBECONFIG 環境変数も尊重）のコンテキスト一覧と
// 現在のコンテキスト名を返す。
func Contexts() (names []string, current string, err error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := rules.Load()
	if err != nil {
		return nil, "", fmt.Errorf("kubeconfig の読み込みに失敗: %w", err)
	}
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cfg.CurrentContext, nil
}

// Client は 1 つのコンテキストに紐づく接続。
type Client struct {
	ContextName string
	ClusterName string
	Clientset   kubernetes.Interface
	Metrics     metricsclient.Interface // metrics-server が無いクラスタでは nil のまま使う
}

// NewClient は指定コンテキストへの接続を作る。contextName が空なら current-context。
func NewClient(contextName string) (*Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("接続情報の構築に失敗: %w", err)
	}
	// TUI は小さなリストを頻繁に引くので、既定のレート制限を少し緩める。
	restCfg.QPS = 50
	restCfg.Burst = 100

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("クライアントの生成に失敗: %w", err)
	}

	c := &Client{ContextName: contextName, Clientset: cs}
	if raw, err := cc.RawConfig(); err == nil {
		name := contextName
		if name == "" {
			name = raw.CurrentContext
			c.ContextName = name
		}
		if ctx, ok := raw.Contexts[name]; ok {
			c.ClusterName = ctx.Cluster
		}
	}
	// metrics-server は「あれば使う」。無くても致命的ではない。
	if mc, err := metricsclient.NewForConfig(restCfg); err == nil {
		c.Metrics = mc
	}
	return c, nil
}
