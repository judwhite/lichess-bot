package yamlbook

import (
	"sort"
	"testing"
)

func TestMoves_GetBestMoveByEval(t *testing.T) {
	// arrange
	cases := []struct {
		moves Moves
		want  string
	}{
		{
			moves: Moves{
				&Move{Move: "Nxe4", CP: -2063},
				&Move{Move: "Qd3", CP: -2204},
				&Move{Move: "Rc1", Mate: -24},
				&Move{Move: "a3", Mate: -26},
				&Move{Move: "Qc1", Mate: -36},
				&Move{Move: "Qe1", Mate: -43},
			},
			want: "Nxe4",
		},
	}

	for _, c := range cases {
		// act
		got := c.moves.GetBestMoveByEval("")

		// assert
		if c.want != got.Move {
			t.Errorf("want: %v got: %v cp: %d mate: %d", c.want, got.Move, got.CP, got.Mate)
		}
	}
}

func TestMoves_Sort(t *testing.T) {
	// arrange
	cases := []struct {
		moves Moves
		want  string
	}{
		{
			moves: Moves{
				&Move{Move: "Nxe4", CP: -2063},
				&Move{Move: "Qd3", CP: -2204},
				&Move{Move: "Rc1", Mate: -24},
				&Move{Move: "a3", Mate: -26},
				&Move{Move: "Qc1", Mate: -36},
				&Move{Move: "Qe1", Mate: -43},
			},
			want: "Nxe4,Qd3,Qe1,Qc1,a3,Rc1",
		},
	}

	for _, c := range cases {
		// act
		sort.Sort(c.moves)

		// assert
		var got string
		for _, move := range c.moves {
			if got != "" {
				got += ","
			}
			got += move.Move
		}

		if c.want != got {
			t.Errorf("want: %v got: %v", c.want, got)
		}
	}
}
