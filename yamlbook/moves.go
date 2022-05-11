package yamlbook

type Moves []*Move

func (m Moves) Less(i, j int) bool {
	if m[i].Weight != m[j].Weight {
		return m[i].Weight > m[j].Weight
	}

	if m[i].Mate != 0 && m[j].Mate != 0 {
		return m[i].Mate < m[j].Mate
	}
	if m[i].Mate != 0 || m[j].Mate != 0 {
		return m[i].Mate > m[j].Mate
	}

	return m[i].CP > m[j].CP
}

func (m Moves) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m Moves) Len() int {
	return len(m)
}

func (m Moves) ContainsEvalsFrom(engineID string) bool {
	for _, move := range m {
		if move.Engine != nil && move.Engine.ID == engineID {
			return true
		}
	}
	return false
}

func (m Moves) ContainsSAN(san string) bool {
	for _, move := range m {
		if move.Move == san {
			return true
		}
	}
	return false
}

func (m Moves) HaveDifferentTimestamps() bool {
	if len(m) < 2 {
		return false
	}

	ts := m[0].TS
	for i := 1; i < len(m); i++ {
		if abs(m[i].TS-ts) > 10 {
			return true
		}
	}
	return false
}

func (m Moves) TooOld() bool {
	const minAge = 1651863380 // Friday, May 6, 2022

	for i := 0; i < len(m); i++ {
		if m[i].TS < minAge {
			return true
		}
	}
	return false
}

func (m Moves) GetSAN(san string) *Move {
	for _, move := range m {
		if move.Move == san {
			return move
		}
	}
	return nil
}

func (m Moves) GetBestMoveByEval(preferUCI string) *Move {
	var bestMove *Move
	for _, move := range m {
		if bestMove == nil {
			bestMove = move
			continue
		}

		if move.Mate == bestMove.Mate && move.CP == bestMove.CP && move.UCI() == preferUCI {
			bestMove = move
			continue
		}

		if move.Mate > bestMove.Mate {
			bestMove = move
			continue
		}

		if move.Mate == 0 && bestMove.Mate == 0 && move.CP > bestMove.CP {
			bestMove = move
			continue
		}
	}

	return bestMove
}

func (m Moves) UCIs() []string {
	ucis := make([]string, 0, len(m))
	for _, move := range m {
		ucis = append(ucis, move.UCI())
	}
	return ucis
}

func abs(a int64) int64 {
	if a > 0 {
		return a
	}
	return -a
}
