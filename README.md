# my-kube-tui

[![release](https://img.shields.io/github/v/release/miyanaga/my-kube-tui?sort=semver)](https://github.com/miyanaga/my-kube-tui/releases)
[![test](https://github.com/miyanaga/my-kube-tui/actions/workflows/test.yml/badge.svg)](https://github.com/miyanaga/my-kube-tui/actions/workflows/test.yml)
[![license](https://img.shields.io/github/license/miyanaga/my-kube-tui)](./LICENSE)

Kubernetes を **2 つの視点だけ**に絞って眺める、参照専用のターミナル UI。

k9s や lazykube のような汎用ビューアーではありません。答えたい問いを 2 つに固定し、
それ以外（編集・削除・スケール・exec・ストレージ・Service・ConfigMap …）は
**意図的に持っていません**。

| 視点 | 答えたい問い |
|---|---|
| **アプリ** | この名前空間で、何由来のどの Pod が動いていて、何を書き出しているか |
| **インフラ** | どのノードが健全で、drain したつもりの台は本当に空になったか |

最上位に接続先（kubeconfig のコンテキスト）があり、それを切り替えながら
この 2 つの視点を行き来します。

```
接続先（kubeconfig のコンテキスト）      ← c で切り替え
├── 1 アプリ視点    名前空間 → ワークロード → [ Pod | ログ | イベント ]
└── 2 インフラ視点  ノード              → [ Pod | 概要  | イベント ]
```

## 画面

**アプリ視点。** レプリカ数は揃っているのに ▲ が付く（停止しないままの Pod が残っている）
——そういう「数字だけでは分からない状態」が一覧の時点で見えます。

```
 ctx  prod  (c)  1 アプリ   2 インフラ                        ns: shop   1s前に更新
╭─ 名前空間 ─────────╮╭─ ワークロード  3 件（異常 1 / 要注意 1） ─────────────────╮
│  default           ││  種別         名前            Ready  経過 イメージ / 補足 │
│▸ shop              ││● Deployment   web              3/3   12d web:1.8.2        │
│  kube-system       ││✖ Deployment   payment          0/2    4m payment:1.9.0    │
│  monitoring        ││▲ DaemonSet    log-shipper     11/12   90d fluent-bit:3.1   │
│                    │╰──────────────────────────────────────────────────────────╯
│                    │╭─ Deployment / payment ───────────────────────────────────╮
│                    ││ Pod   ログ   イベント                                    │
│                    ││  Pod              状態              Ready 再起動 経過    │
│                    ││✖ payment-7d…-4kx2 ImagePullBackOff    0/1      0    4m   │
│                    ││✖ payment-7d…-q8vn ImagePullBackOff    0/1      0    4m   │
│                    ││↳ Back-off pulling image "payment:1.9.0"                  │
╰────────────────────╯╰──────────────────────────────────────────────────────────╯
```

**ログ**はワークロード配下の全 Pod を 1 本に集約します（Pod 名は共通部分を落として色分け）。

```
│ Pod   ログ   イベント
│18:59:21 4kx2 ▏2026-08-24 18:59:21.608 INFO  listening on :8080
│18:59:22 q8vn ▏2026-08-24 18:59:22.104 WARN  retrying upstream (1/5)
│18:59:24 q8vn ▏[payment-7d9f-q8vn] ログを取得できません: dial tcp 10.0.0.9:10250: no route
```

**インフラ視点。** cordon したノードが本当に空になったかを検算して 1 行で出します。

```
╭─ ノード  5 台（Ready 4 / NotReady 1 / cordon 1） ────────────────────────────╮
│  ノード       状態                    Pod  CPU 要求      メモリ要求          │
│● node-01      Ready                 8/110  █░░░░░░░ 15%  ░░░░░░░░  4%        │
│▲ node-02      Ready cordon(残3)     6/110  ██░░░░░░ 28%  █░░░░░░░  7%        │
│✖ node-03      NotReady:NodeStatus…  6/110 !5  █░░░░░░░ 15%  ░░░░░░░░  4%     │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ ノード / node-02 ───────────────────────────────────────────────────────────╮
│ Pod   概要   イベント                                                        │
│状態         Ready cordon(残3)                                                │
│drain        drain 未完了：退避対象の Pod が 3 個残っています                 │
│             （DaemonSet 2 / static 0 は対象外）                              │
│Pod          6 / 110   内訳: DaemonSet 2 / static 0 / 退避対象 3   異常 0      │
│CPU 要求     ████░░░░░░░░░░░░  28%   1100m / 4   上限合計 2                    │
│名前空間別   kube-system:2  monitoring:1  shop:3                              │
╰──────────────────────────────────────────────────────────────────────────────╯
```

## インストール

### リリースアーカイブ

[Releases](https://github.com/miyanaga/my-kube-tui/releases) から
OS / アーキテクチャに合うものを取って、PATH の通った場所に置きます。

```bash
# macOS (Apple Silicon) の例
VER=0.2.0
curl -sL https://github.com/miyanaga/my-kube-tui/releases/download/v${VER}/my-kube-tui_${VER}_darwin_arm64.tar.gz \
  | tar xz -C /tmp my-kube-tui
install -m 755 /tmp/my-kube-tui /usr/local/bin/my-kube-tui
```

### go install

```bash
go install github.com/miyanaga/my-kube-tui/cmd/my-kube-tui@latest
```

### ソースから

```bash
git clone https://github.com/miyanaga/my-kube-tui.git
cd my-kube-tui
make build      # ./bin/my-kube-tui
```

## 使い方

```bash
my-kube-tui                    # current-context に接続
my-kube-tui --context prod     # コンテキストを指定して起動
```

キー操作は起動後に `?` で表示されます。

| キー | 動作 |
|---|---|
| `1` / `2` | アプリ視点 / インフラ視点 |
| `c` | 接続先（コンテキスト）を切り替える |
| `tab` / `S-tab` | ペインの移動 |
| `↑↓` `jk` `g` `G` `C-u` `C-d` | 選択の移動 |
| `enter` | 決定（名前空間 → ワークロード → 詳細） |
| `[` `]` `←` `→` | 詳細タブの切り替え |
| `/` | 絞り込み（`Esc` で解除） |
| `r` | 今すぐ更新（既定で 3 秒ごとに自動更新） |
| `M` | マウスの on/off |
| `?` / `q` | ヘルプ / 終了 |

マウスも使えます。

| 操作 | 動作 |
|---|---|
| クリック | 見出しのタブ・接続先・一覧の行・詳細のタブを直接選ぶ |
| ホイール | カーソルの下のペインをスクロール（選択は動かさない） |

マウスを掴んでいるあいだは端末側の範囲選択ができなくなります。ログをコピーしたいときは
`M` で手放してください（`M` でまた戻せます）。

ログのタブでのみ効くキー:

| キー | 動作 |
|---|---|
| `f` | 追従の on/off（`↑` でスクロールすると自動的に off） |
| `p` | 直前に落ちたコンテナのログ（`CrashLoopBackOff` の原因を見る） |
| `a` | ワークロード全体 ⇔ 選択した 1 Pod |

タブ名に付く `(…)` は今の状態です。`1Pod` = 1 つの Pod に絞り込み中、
`追従OFF` = 新しい行を自動で追いかけていない、`直前` = 直前に落ちたコンテナのログ。

## アプリ視点

名前空間＝アプリケーションという運用を前提にしています。

- **Pod の帰属はラベルセレクタではなく `ownerReferences` で決める。**
  Pod → ReplicaSet → Deployment、Pod → Job → CronJob と 1 段辿るので、
  「何由来の Pod か」がセレクタの重複に左右されません。中間の ReplicaSet と Job は
  畳んで表示しません（Deployment 1 行の下に Pod が並ぶ）
- **ログはワークロード単位で 1 本に集約する。** Pod ごとに独立したストリームなので
  到着順に積むと Pod ごとの塊になってしまうため、**時刻順にマージ**しています。
  Pod 名は共通する接頭辞を落として、見分けのつく後半だけを色分けして出します
- **ログは最初に直近 100 行だけ読む。** 上端までスクロールすると自動でさかのぼって
  読み足します（Pod あたり 100 → 300 → 900 …）。Kubernetes のログ API に
  「途中から」は無く `tailLines` を増やして引き直すしかないので、
  重複する行はタイムスタンプで突き合わせて捨てています
- **名前空間やワークロードを選び直すと、詳細は Pod 一覧に戻ります。**
  別の対象のログを開いたまま見出しだけ入れ替わると、何を見ているのか分からなくなるため
- **「起動する前に落ちている」理由はログではなくイベントに出る。**
  だからイベントをログと同格のタブに置いています。イメージの取得失敗、
  スケジュールできない、設定が読めない、といったものはこちら
- Pod 一覧で選んだ行の理由は表の直下に 1 行（`↳`）で出るので、
  多くの場合はイベントタブを開かずに済みます

## インフラ視点

ノードはフラット運用（役割で差をつけない）を前提にしています。

- **cordon したノードは「本当に空になったか」を検算して 1 行で出す。**
  DaemonSet と static Pod は drain しても残るのが正常なので、それらを除いた
  **退避対象の残数**を数えます。抜けたつもりの取りこぼしを見落としません
- **使用率は requests / allocatable。** metrics-server があれば実測も併記しますが、
  無くても「詰め込めているか」は要求量で判断できます
- **どのノードにも乗れていない Pod は `(未スケジュール)` という疑似ノードに集める。**
  「置き場が無い」はノードの異常とは別種の問題なので、必ず目に入る場所に出します
- Pod タブは名前空間をまたいだ全 Pod を、由来つきで並べます

## 設計方針

- **参照専用。** 変更操作は一切実装しません。誤爆の余地が無いことが、
  この道具を安心して常用できる条件だと考えています
- **ストレージは扱いません。** PV / PVC / StorageClass は表示しません
- **汎用性より目的。** 見たいものが最初から画面に出ていることを優先し、
  任意リソースのブラウズや YAML 編集は持ちません
- クラスタへの問い合わせは 3 秒ごとのポーリング（informer は使いません）。
  対象は今表示している視点に必要なものだけです

## 動作要件

- `~/.kube/config`（または `KUBECONFIG`）に読める接続先があること
- 必要な権限は参照だけ: Namespace / Pod / Node / Event の `get,list`、
  Deployment / DaemonSet / StatefulSet / ReplicaSet / Job / CronJob の `get,list`、
  Pod のログ（`pods/log` の `get`）
- metrics-server は任意（無ければ要求量ベースの表示になります）

## 開発

```bash
make build   # ビルド
make run     # 実行
make check   # go vet + go build
```

```
cmd/my-kube-tui/    エントリポイント
internal/k8s/       参照専用の Kubernetes クライアント（client-go）
  client.go           コンテキストの列挙と接続
  workload.go         ワークロードの列挙と Pod の帰属解決
  pod.go              Pod の状態を「動いていない理由」1 行に要約する
  node.go             ノードの集計と drain の検算
  event.go            イベントの取得
  log.go              複数 Pod のログを 1 本に集約するストリーム
internal/ui/        Bubble Tea の TUI
  model.go            状態とメッセージ、取得コマンド
  update.go           キー操作とログストリームの生死管理
  view*.go            描画（アプリ視点 / インフラ視点）
  layout.go           ペインの高さと行数の一元管理
```

リリースはタグの push で GitHub Actions（GoReleaser）が作ります。

```bash
git tag v0.1.0 && git push origin v0.1.0
```

## ライセンス

MIT
