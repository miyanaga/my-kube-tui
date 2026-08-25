# CLAUDE.md

## このリポジトリ

`my-kube-tui` は Kubernetes を**アプリ視点**と**インフラ視点**の 2 つだけに絞って眺める
**参照専用**の TUI（Go + Bubble Tea + client-go）。設計の意図と操作は `README.md` が正。

## 触るときの前提

- **書き込み系の API を足さない。** 参照専用であることがこの道具の価値。
  scale / delete / edit / exec / port-forward などを追加しない
- **ストレージ（PV / PVC / StorageClass）と Service / Ingress / ConfigMap は対象外。**
  「見えると便利そう」で足すと、目的が絞られている利点が消える
- **視点は 2 つ、タブは 3 つに固定。** 増やす前に、既存のどれかを置き換えられないか考える

## 踏みやすい罠

- **`Update` で `return m, m.someMutatingMethod()` と書かない。**
  返り値の評価順が仕様上あいまいで、ポインタレシーバの変更が `m` のコピーに
  反映されないことがある。`cmd := m.x(); return m, cmd` と 2 行に分ける
- **ペインの行数は `internal/ui/layout.go` に集約してある。**
  描画側だけ行を足すと `Update` のクランプとずれて、選択が画面外に出る。
  Pod タブは「列見出し 1 行＋理由 1 行」を常に確保している
- **表の桁揃えに `pad()` を使えるのは装飾前の文字列だけ。**
  色をつけた後は `padStyled()`（ANSI を無視して幅を測る）を使う
- **Pod 名は右から切らない。** 末尾だけが違うので識別子が消える。
  表では `padName()`（中央省略）、ログでは共通接頭辞を落として後半だけ出す
- **ログは Pod ごとに独立したストリーム。** 到着順に積むと Pod ごとの塊になるので
  `appendLogLines` が時刻順に差し込んでいる。ここを素朴な append に戻さない
- **`drain` の残数は DaemonSet と static Pod を除いて数える。**
  除かないと「いつまでも空にならない」表示になる
- **マウスの当たり判定は描画時の座標を控えずに計算で出す**（`internal/ui/mouse.go`）。
  `layout.go` の値だけから逆算しているので、行を足し引きしたら両方を直す
- **ログのさかのぼりは `tailLines` を増やして引き直すしかない。**
  Kubernetes のログ API に「途中から」は無い。必ず既存の行と重なるので
  `LogLine.Key()` で重複を捨てている。表示位置は末尾からのオフセットで
  持っているため、前に足しても見ている場所は動かない

## 機能を変えたら

`README.md`（キー一覧・設計方針）と `?` のヘルプ（`internal/ui/view.go` の `helpBody`）、
`--help`（`cmd/my-kube-tui/main.go`）の 3 点を揃えて更新する。

## 動作確認

`kubectl config get-contexts` に出るクラスタが要る。TUI なので目視確認が要るが、
pty 上で動かして画面をテキスト化すれば自動でも確認できる
（`pyte` で端末をエミュレートし、`ESC]11;?` と `ESC[6n` の問い合わせに
返事をしないと termenv が起動時に固まる点に注意）。
