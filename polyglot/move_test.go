package polyglot

import (
	"fmt"
	"testing"

	"trollfish-lichess/fen"
)

const castlingFEN = "r3k2r/pppppppp/8/8/8/8/PPPPPPPP/R3K2R w KQkq - 0 1"
const noCastlingFEN = "4r3/pppppppk/8/8/8/8/PPPPPPPK/4R3 w KQkq - 0 1"
const promoteFEN = "r3k2r/pPpppppp/8/8/8/8/PpPPPPPP/R3K2R w KQkq - 0 1"

func TestToUCIMove(t *testing.T) {
	cases := []struct {
		move uint16
		fen  string
		want string
	}{
		//  1101 0010 0011  ->
		// 110 100 100 011  ->
		//   6   4   4   3  ->
		//  +1  +1  +1  +1  ->
		//   7   5   5   4  ->
		//   7   e   5   d  ->  e7d5
		// the 6 bits can be converted directly 0-63, but we need rank/file for castling translation and uci
		{move: 0x0d23, want: "e7d5"},
		{move: 0b100000111, fen: castlingFEN, want: "e1g1"},    // translated from e1h1
		{move: 0b100000000, fen: castlingFEN, want: "e1c1"},    // translated from e1a1
		{move: 0b111100111111, fen: castlingFEN, want: "e8g8"}, // translated from e8h8
		{move: 0b111100111000, fen: castlingFEN, want: "e8c8"}, // translated from e8a8
		{move: 0b100000111, fen: noCastlingFEN, want: "e1h1"},
		{move: 0b100000000, fen: noCastlingFEN, want: "e1a1"},
		{move: 0b111100111111, fen: noCastlingFEN, want: "e8h8"},
		{move: 0b111100111000, fen: noCastlingFEN, want: "e8a8"},
		{move: 0b001110001111001, fen: promoteFEN, want: "b7b8n"},
		{move: 0b010110001111001, fen: promoteFEN, want: "b7b8b"},
		{move: 0b011110001111001, fen: promoteFEN, want: "b7b8r"},
		{move: 0b100110001111001, fen: promoteFEN, want: "b7b8q"},
		{move: 0b001001110000110, fen: promoteFEN, want: "g2g1n"},
		{move: 0b010001110000110, fen: promoteFEN, want: "g2g1b"},
		{move: 0b011001110000110, fen: promoteFEN, want: "g2g1r"},
		{move: 0b100001110000110, fen: promoteFEN, want: "g2g1q"},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_%04x_%s", c.fen, c.move, c.want), func(t *testing.T) {
			// arrange
			board := fen.FENtoBoard(c.fen)

			// act
			uciMove := toUCIMove(board, c.move)

			// assert
			if c.want != uciMove {
				t.Errorf("want: %s got: %s", c.want, uciMove)
			}
		})
	}
}
