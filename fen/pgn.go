package fen

import (
	"fmt"
	"strconv"
	"strings"
)

func PGNtoMoves(pgn string) ([]LegalMove, error) {
	pgn = strings.TrimSpace(pgn)
	if len(pgn) == 0 {
		return nil, nil
	}

	var moves []LegalMove

	pgn = strings.TrimSpace(strings.Join(strings.Split(pgn, "\n"), " "))
	parts := strings.Split(pgn, " ")
	b := FENtoBoard("")
	var fullMove int
	for i, part := range parts {
		if part == "1-0" || part == "0-1" || part == "1/2-1/2" || part == "*" || part == "" {
			continue
		}

		if strings.HasSuffix(part, ".") {
			fullMove++
			moveNum := strings.TrimSuffix(part, ".")
			n, err := strconv.Atoi(moveNum)
			if err != nil {
				return nil, fmt.Errorf("%v: '%s'", err, moveNum)
			}
			if n != fullMove {
				return nil, fmt.Errorf("move number '%s' (%d) doesn't match fullMove: %d i: %d", part, n, fullMove, i)
			}
			continue
		}

		san := part
		legalMoves := b.LegalMoves()
		var move LegalMove
		var uci string
		for _, legalMove := range legalMoves {
			if legalMove.SAN == san {
				uci = legalMove.UCI
				move = legalMove
				break
			}
		}
		if san == "" {
			return nil, fmt.Errorf("FEN: %s move: %d color: %s want: %s got: empty string", b.FEN(), fullMove, b.ActiveColor, part)
		}
		if san != part {
			return nil, fmt.Errorf("FEN: %s move: %d color: %s want: %s got: %s", b.FEN(), fullMove, b.ActiveColor, part, san)
		}
		moves = append(moves, move)
		b.Moves(uci)
	}
	return moves, nil
}
