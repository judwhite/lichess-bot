package analyze

type Moves []Move

func (moves Moves) Less(i, j int) bool {
	return moves[i].Ply < moves[j].Ply
}

func (moves Moves) Swap(i, j int) {
	moves[i], moves[j] = moves[j], moves[i]
}

func (moves Moves) Len() int {
	return len(moves)
}

type Move struct {
	Ply      int    `json:"ply"`
	UCI      string `json:"uci"`
	SAN      string `json:"san"`
	Eval     Eval   `json:"eval"`
	BestMove Eval   `json:"best_move"`
	IsMate   bool   `json:"mate,omitempty"`
	PV       string `json:"pv,omitempty"`
}
