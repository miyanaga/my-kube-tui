package ui

// レイアウトはヘッダ 1 行・フッタ 1 行を固定で取り、残りを本文に割る。
// 各ペインの「何行入るか」は Update 側のクランプでも使うので、ここに集約する。

func (m *Model) bodyH() int { return maxInt(3, m.h-2) }

func (m *Model) leftW() int {
	w := m.w / 5
	if w < 20 {
		w = 20
	}
	if w > 32 {
		w = 32
	}
	if w > m.w-30 {
		w = maxInt(12, m.w-30)
	}
	return w
}

func (m *Model) rightW() int { return maxInt(20, m.w-m.leftW()) }

// App モード：右側を上下に割る。
func (m *Model) wlH() int {
	h := m.bodyH() * 2 / 5
	if h < 6 {
		h = 6
	}
	if h > m.bodyH()-6 {
		h = maxInt(4, m.bodyH()-6)
	}
	return h
}

func (m *Model) appDetH() int { return maxInt(4, m.bodyH()-m.wlH()) }

// Infra モード：上にノード一覧、下に詳細。
func (m *Model) nodeH() int {
	h := m.bodyH() * 9 / 20
	if h < 7 {
		h = 7
	}
	if h > m.bodyH()-7 {
		h = maxInt(4, m.bodyH()-7)
	}
	return h
}

func (m *Model) infraDetH() int { return maxInt(4, m.bodyH()-m.nodeH()) }

// 各ペインに実際に並ぶ行数（枠線・列見出し・タブ行を差し引いたもの）。
func (m *Model) nsRows() int { return maxInt(1, m.bodyH()-2) }

func (m *Model) wlRows() int { return maxInt(1, m.wlH()-3) }

func (m *Model) nodeRows() int { return maxInt(1, m.nodeH()-3) }

func (m *Model) detRows() int {
	h := m.appDetH()
	if m.mode == ModeInfra {
		h = m.infraDetH()
	}
	// 枠 2 行 ＋ タブ 1 行。
	rows := h - 3
	switch m.tab {
	case 0:
		rows -= 2 // 列見出し ＋ 選択中 Pod の理由を出す 1 行（常に確保する）
	case 2:
		rows-- // 列見出し
	}
	return maxInt(1, rows)
}
