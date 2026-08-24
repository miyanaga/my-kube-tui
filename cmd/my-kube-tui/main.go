// my-kube-tui は Kubernetes を「アプリ視点」と「インフラ視点」の 2 つだけに絞って眺める
// 参照専用の TUI。汎用の kubectl 代替ではなく、次の 2 つの問いに答えることだけを目的にしている。
//
//	アプリ視点  : この名前空間で何由来のどの Pod が動いていて、何を書き出しているか
//	インフラ視点: どのノードが健全で、drain したつもりの台は本当に空になったか
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/miyanaga/my-kube-tui/internal/ui"
)

// GoReleaser がタグから注入する。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		ctxName     string
		showVersion bool
	)
	flag.StringVar(&ctxName, "context", "", "接続する kubeconfig のコンテキスト名（既定は current-context）")
	flag.BoolVar(&showVersion, "version", false, "バージョンを表示して終了する")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `my-kube-tui - Kubernetes をアプリ視点／インフラ視点で眺める参照専用 TUI

使い方:
  my-kube-tui [--context <名前>]

キー操作は起動後に ? で表示します。クラスタを変更する操作は持ちません。

オプション:
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		fmt.Printf("my-kube-tui %s (%s, %s)\n", version, commit, date)
		return
	}

	p := tea.NewProgram(ui.New(ctxName), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}
