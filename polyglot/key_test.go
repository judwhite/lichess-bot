package polyglot

import (
	"testing"

	"trollfish-lichess/fen"
)

func TestKey(t *testing.T) {
	cases := []struct {
		fen  string
		want uint64
	}{
		{fen: "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", want: 0x463b96181691fc9c},
		{fen: "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1", want: 0x823c9b50fd114196},
		{fen: "rnbqkbnr/ppp1pppp/8/3p4/4P3/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 2", want: 0x0756b94461c50fb0},
		{fen: "rnbqkbnr/ppp1pppp/8/3pP3/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 2", want: 0x662fafb965db29d4},
		{fen: "rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3", want: 0x22a48b5a8e47ff78},
		{fen: "rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPPKPPP/RNBQ1BNR b kq - 0 3", want: 0x652a607ca3f242c1},
		{fen: "rnbq1bnr/ppp1pkpp/8/3pPp2/8/8/PPPPKPPP/RNBQ1BNR w - - 0 4", want: 0x00fdd303c946bdd9},
		{fen: "rnbqkbnr/p1pppppp/8/8/PpP4P/8/1P1PPPP1/RNBQKBNR b KQkq c3 0 3", want: 0x3c8123ea7b067637},
		{fen: "rnbqkbnr/p1pppppp/8/8/P6P/R1p5/1P1PPPP1/1NBQKBNR b Kkq - 0 4", want: 0x5c3f9b829b279560},
	}

	for _, c := range cases {
		t.Run(c.fen, func(t *testing.T) {
			// arrange
			board := fen.FENtoBoard(c.fen)

			// act
			got := Key(board)

			// assert
			if c.want != got {
				t.Errorf("want: %x got: %x", c.want, got)
			}
		})
	}
}
