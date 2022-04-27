package polyglot

import (
	"fmt"
	"trollfish-lichess/fen"
)

var promotionPiece = []string{"", "n", "b", "r", "q"}

func toUCIMove(b *fen.Board, v uint16) string {
	if v == 0 {
		return ""
	}

	toFile := v & 0x07
	toRank := (v >> 3) & 0x07
	fromFile := (v >> 6) & 0x07
	fromRank := (v >> 9) & 0x07
	promote := (v >> 12) & 0x07

	const a = 0
	const e = 4
	const h = 7

	if fromRank == 0 && toRank == 0 && fromFile == e && b.Pos[56+e] == 'K' {
		if toFile == a {
			return "e1c1" // O-O-O
		} else if toFile == h {
			return "e1g1" // O-O
		}
	} else if fromRank == 7 && toRank == 7 && fromFile == e && b.Pos[e] == 'k' {
		if toFile == a {
			return "e8c8" // O-O-O
		} else if toFile == h {
			return "e8g8" // O-O
		}
	}

	uci := fmt.Sprintf("%c%d%c%d%s", 'a'+fromFile, fromRank+1, 'a'+toFile, toRank+1, promotionPiece[promote])
	return uci
}
