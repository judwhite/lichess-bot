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

		piece := san[0]
		if piece >= 'a' && piece <= 'h' {
			piece = 'P'
		} else if piece == 'O' {
			piece = 'K'
		}
		if b.ActiveColor == BlackPieces {
			piece = lower(piece)
		}

		legalMoves := b.PieceLegalMoves(piece)
		var move LegalMove
		var uci string
		var lastUCItoSAN string
		for _, legalMove := range legalMoves {
			if legalMove.Piece == piece {
				lastUCItoSAN = b.UCItoSAN(legalMove.UCI)
				if lastUCItoSAN == san {
					uci = legalMove.UCI
					move = legalMove
					break
				}
			}
		}
		if san == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: <empty>", b.FEN(), fullMove, b.ActiveColor, part)
		}
		if san != part {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: '%s'", b.FEN(), fullMove, b.ActiveColor, part, san)
		}
		if uci == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' piece: '%c' san: '%s' uci: <empty> move: %v legalMoves: %v last_san_check: %s", b.FEN(), fullMove, b.ActiveColor, piece, part, move, legalMoves, lastUCItoSAN)
		}
		moves = append(moves, move)
		b.Moves(uci)
	}
	return moves, nil
}
