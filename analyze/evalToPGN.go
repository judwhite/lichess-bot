package analyze

import (
	"fmt"
	"strings"

	"trollfish-lichess/fen"
)

func evalToPGN(pgn *fen.PGNGame, movesEval Moves) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("[Event \"%s\"]\n", pgn.Tags["Event"]))
	sb.WriteString(fmt.Sprintf("[Site \"%s\"]\n", pgn.Tags["Site"]))
	sb.WriteString(fmt.Sprintf("[Date \"%s\"]\n", pgn.Tags["Date"]))
	sb.WriteString(fmt.Sprintf("[Round \"%s\"]\n", pgn.Tags["Round"]))
	sb.WriteString(fmt.Sprintf("[White \"%s\"]\n", pgn.White))
	sb.WriteString(fmt.Sprintf("[Black \"%s\"]\n", pgn.Black))
	sb.WriteString(fmt.Sprintf("[WhiteElo \"%d\"]\n", pgn.WhiteElo))
	sb.WriteString(fmt.Sprintf("[BlackElo \"%d\"]\n", pgn.BlackElo))
	sb.WriteString(fmt.Sprintf("[Result \"%s\"]\n", pgn.Result))

	if pgn.SetupFEN != "" && pgn.SetupFEN != startPosFEN {
		sb.WriteString(fmt.Sprintf("[FEN \"%s\"]\n", pgn.SetupFEN))
		sb.WriteString("[Setup \"1\"]\n")
	}

	sb.WriteString(fmt.Sprintf("[Annotator \"Stockfish 15\"]\n"))
	sb.WriteString("\n")

	board := fen.FENtoBoard(pgn.SetupFEN)
	prevEval := "0.24"
	for _, move := range movesEval {
		moveNumber := board.FullMove
		color := board.ActiveColor

		var englishColor string
		if color == fen.WhitePieces {
			sb.WriteString(fmt.Sprintf("%d. ", moveNumber))
			englishColor = "White"
		} else {
			sb.WriteString(fmt.Sprintf("%d. ... ", moveNumber))
			englishColor = "Black"
		}

		bestMove := move.BestMove
		playedMove := move.Eval

		// $1 = !  (good move)
		// $2 = ?  (poor move, mistake)
		// $4 = ?? (very poor move or blunder)
		// $6 = ?! (questionable or dubious move, inaccuracy)
		var annotation, annotationWord string
		var showVariations bool
		if !move.IsMate && bestMove.UCIMove != "" {
			diff := diffWC(playedMove, bestMove)
			if diff <= -0.3 {
				annotation = "??" // $4
				annotationWord = "Blunder"
				if bestMove.Mate > 0 && playedMove.Mate <= 0 {
					annotationWord = "Lost forced checkmate sequence"
				} else if bestMove.Mate == 0 && playedMove.Mate < 0 {
					annotationWord = "Checkmate is now unavoidable"
				}
			} else if diff <= -0.2 {
				annotation = "?" // $2
				annotationWord = "Mistake"
			} else if diff <= -0.1 {
				annotation = "?!" // $6
				annotationWord = "Inaccuracy"
			}

			showVariations = diff <= -0.02
		}

		sb.WriteString(move.SAN + annotation + "\n")
		if annotation != "" {
			bestMoveSAN := board.UCItoSAN(move.BestMove.UCIMove)

			if strings.HasPrefix(prevEval, "#") {
				mate := strings.TrimLeft(prevEval, "#-")
				prevEval = "Mate in " + mate
			}

			curEval := move.Eval.String(color)
			if strings.HasPrefix(curEval, "#") {
				mate := strings.TrimLeft(curEval, "#-")
				curEval = "Mate in " + mate
			}

			sb.WriteString(fmt.Sprintf("    { (%s → %s) %s. %s was best. }\n", prevEval, curEval, annotationWord, bestMoveSAN))
		}

		if move.Eval.Mated {
			sb.WriteString(fmt.Sprintf("    { Checkmate. %s is victorious. }\n", englishColor))
		} else {
			sb.WriteString(fmt.Sprintf("    { [%%eval %s] }\n", move.Eval.String(color)))
		}

		if showVariations {
			writeVariation(&sb, board, bestMove, "")
			//writeVariation(&sb, board, playedMove, annotation)
		}
		board.Moves(move.UCI)

		prevEval = move.Eval.String(color)
	}
	sb.WriteString(fmt.Sprintf("%s\n", pgn.Result))

	return sb.String()
}

func writeVariation(sb *strings.Builder, board fen.Board, eval Eval, annotation string) {
	sb.WriteString("    ( ")

	used := 6

	basePly := (board.FullMove - 1) * 2
	if board.ActiveColor == fen.BlackPieces {
		basePly++
	}

	sans := board.UCItoSANs(eval.PV...)
	for j := 0; j < len(sans); j++ {
		san := sans[j]

		ply := basePly + j
		moveNumber := (ply + 2) / 2

		color := plyToColor(ply)

		if j == 0 {
			sb.WriteString(fmt.Sprintf("%d. ", moveNumber))
			used += 5
			if color == fen.BlackPieces {
				sb.WriteString("... ")
				used += 4
			}
		} else if color == fen.WhitePieces {
			sb.WriteString(fmt.Sprintf("%d. ", moveNumber))
			used += 5
		}

		if j == 0 {
			sb.WriteString(fmt.Sprintf("%s%s ", san, annotation))
			used += len(san) + len(annotation) + 1
		} else {
			sb.WriteString(fmt.Sprintf("%s ", san))
			used += len(san) + 1
		}

		if j == 0 {
			variationEval := fmt.Sprintf("{ [%%eval %s] } ", eval.String(color))
			sb.WriteString(variationEval)
			used += len(variationEval)
		}

		if used > 72 && j != len(eval.PV)-1 {
			sb.WriteString("\n    ")
			used = 4
		}
	}
	sb.WriteString(")\n")
}
