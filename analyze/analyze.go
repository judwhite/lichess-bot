package analyze

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"time"

	"trollfish-lichess/fen"
	"trollfish-lichess/yamlbook"
)

const useFullResources = true
const logEngineOutput = false

const SyzygyPath = "/home/jud/projects/tablebases/3-4-5:/home/jud/projects/tablebases/wdl6:/home/jud/projects/tablebases/dtz6:/home/jud/projects/tablebases/7" // TODO: get path from config file

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 26
const hashMemory = 40960

// TODO: put in config
const stockfishBinary = "/home/jud/projects/trollfish/stockfish/stockfish"
const stockfishDir = "/home/jud/projects/trollfish/stockfish"

type AnalysisOptions struct {
	MinDepth   int
	MaxDepth   int
	MinTime    time.Duration
	MaxTime    time.Duration
	DepthDelta int
}

// const Engine_Stockfish_15_NN_6e0680e = 1
// id: 1
// sfid = "sf15"
// sfcommit = "6e0680e"
// sfnn = "d0b74ce1e5eb"

func New() *Analyzer {
	return &Analyzer{
		input:           make(chan string, 512),
		output:          make(chan string, 512),
		logEngineOutput: logEngineOutput,
	}
}

type Analyzer struct {
	logEngineMtx     sync.Mutex
	input            chan string
	output           chan string
	stockfishStarted int64
	logEngineOutput  bool
}

func (a *Analyzer) AnalyzePGNFile(ctx context.Context, opts AnalysisOptions, pgnFilename string, book *yamlbook.Book) error {
	db, err := fen.LoadPGNDatabase(pgnFilename)
	if err != nil {
		return err
	}

	for _, game := range db.Games {
		if err := a.AnalyzeGame(ctx, opts, game, book); err != nil {
			return err
		}
	}

	return nil
}

func (a *Analyzer) AnalyzeGame(ctx context.Context, opts AnalysisOptions, pgn *fen.PGNGame, book *yamlbook.Book) error {
	logInfo(fmt.Sprintf("start game analysis, %d moves (%d plies)", (len(pgn.Moves)+1)/2, len(pgn.Moves)))

	// lowercase all moves
	// TODO: might be important to do in the PGN file itself
	/*for i := 0; i < len(moves); i++ {
		moves[i] = strings.ToLower(moves[i])
	}*/

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return err
	}

	var movesEval Moves

	board := fen.FENtoBoard(pgn.SetupFEN)
	for i := 0; i < len(pgn.Moves); i++ {
		boardFEN := board.FEN()
		logInfo(fmt.Sprintf("FEN: %s", boardFEN))

		playerMoveUCI := pgn.Moves[i].UCI
		playerMoveSAN := board.UCItoSAN(playerMoveUCI)

		player := board.ActiveColor

		nextBoard := fen.FENtoBoard(boardFEN)
		nextBoard.Moves(playerMoveUCI)
		if nextBoard.IsMate() {
			// TODO: stalemate
			movesEval = append(movesEval, Move{
				Ply:      i,
				UCI:      playerMoveUCI,
				SAN:      playerMoveSAN,
				IsMate:   true,
				Eval:     Eval{UCIMove: playerMoveUCI, Mated: true},
				BestMove: Eval{UCIMove: playerMoveUCI, Mated: true},
			})
			continue
		}

		// per-ply debug output
		if len(movesEval) > 0 {
			//pgn := evalToPGN(startPosFEN, 0, movesEval, false)
			//logMultiline(pgn)
			//if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
			//	return err
			//}

			tbl := debugEvalTable(startPosFEN, movesEval)
			logMultiline(tbl)
		}

		// TODO: lookup positions on Lichess only if there are N pieces left on the board (30,31,32, for example)

		// 1. Check the book to see if we have stored lichess evals
		// 2. If not, check Lichess for position evals

		bookMoves, _ := book.Get(boardFEN)
		if !bookMoves.ContainsEvalsFrom("lichess") && board.FullMove <= 10 {
			if err := book.CheckOnlineDatabase(ctx, boardFEN); err != nil {
				return err
			}
		}

		bestMove := bookMoves.GetBestMoveByEval()

		if bestMove == nil {
			logInfo("running engine to find best move...")
			evals, err := a.AnalyzePosition(ctx, opts, boardFEN)
			if err != nil {
				return err
			}

			for _, eval := range evals {
				logInfo(fmt.Sprintf("depth: %d move: %s global_cp: %4d global_mate: %4d", eval.Depth, eval.UCIMove, eval.GlobalCP(player), eval.GlobalMate(player)))
			}

			bestMove = evalsToBookMove(boardFEN, "sf15", evals[0], evals)

			book.Add(boardFEN, bestMove)
			if err := book.Save(); err != nil {
				return err
			}
		}

		// TODO: keep for a book-only analysis?
		//if len(evals) == 0 {
		//	evals = append(evals, Eval{UCIMove: playerMoveUCI, CP: 55555, Mate: 0})
		//}

		// TODO: put back for book-only analysis?
		// playerMove not in book
		/*if playerMove.UCIMove == "" {
			playerMove = Eval{UCIMove: playerMoveUCI, CP: 99999, Mate: 0}
			evals = append(evals, playerMove)
		}*/

		var playerMove *yamlbook.Move
		if bestMove.Move == playerMoveSAN {
			playerMove = bestMove
		} else {
			playerMove = bookMoves.GetSAN(playerMoveSAN)
		}

		if playerMove == nil {
			logInfo(fmt.Sprintf("playerMoveSAN: '%s' bestMove.Move: '%s'", playerMoveSAN, bestMove.Move))

			// TODO: extract playerMove, bestMove from evals. may have gotten bestMove from analysis or book. handle both.
			// TODO: make this work:
			// evals, err = a.AnalyzePosition(ctx, opts, boardFEN, bestMove.UCIMove, playerMoveUCI)
			logInfo(fmt.Sprintf("played move %s wasn't the best (best was %s) and eval not found in book. running engine to find player's move...", playerMoveSAN, bestMove.Move))
			evals, err := a.AnalyzePosition(ctx, opts, boardFEN, playerMoveUCI)
			if err != nil {
				return err
			}

			// TODO: verify move SAN matches
			if evals[0].UCIMove != playerMoveUCI {
				panic(fmt.Errorf("evals[0].UCIMove '%s' != playerMoveUCI '%s'", evals[0].UCIMove, playerMoveUCI))
			}
			playerMove = evalsToBookMove(boardFEN, "sf15", evals[0], evals)

			book.Add(boardFEN, playerMove)
			if err := book.Save(); err != nil {
				return err
			}
		}

		fmt.Printf("best move:   %-7s cp: %d mate: %d\n", bestMove.Move, bestMove.CP, bestMove.Mate)
		fmt.Printf("player move: %-7s cp: %d mate: %d\n", playerMove.Move, playerMove.CP, playerMove.Mate)

		// set played move + best move eval

		newMove := Move{
			Ply:      i,
			UCI:      playerMoveUCI,
			SAN:      playerMoveSAN,
			Eval:     bookMoveToEval(playerMove),
			BestMove: bookMoveToEval(bestMove),
		}

		movesEval = append(movesEval, newMove)

		// show output

		logInfo(fmt.Sprintf("%3d/%3d %3d. %-7s played_cp: %6d played_mate: %2d top_move: %-7s top_cp: %6d top_mate: %2d",
			i+1, len(pgn.Moves), (i+2)/2,
			playerMove.Move, playerMove.CP, playerMove.Mate,
			bestMove.Move, bestMove.CP, bestMove.Mate,
		))

		board.Moves(playerMoveUCI)
	}

	evalPGN := evalToPGN(startPosFEN, 0, movesEval, true)
	logMultiline(evalPGN)

	tbl := debugEvalTable(startPosFEN, movesEval)
	logMultiline(tbl)

	if err := ioutil.WriteFile("eval.pgn", []byte(evalPGN), 0644); err != nil {
		logMultiline(evalPGN)
		log.Fatal(err)
	}

	if wg != nil {
		a.input <- "quit"

		cancel()
		wg.Wait()
	}

	return nil
}

func (a *Analyzer) AnalyzePosition(ctx context.Context, opts AnalysisOptions, fenPos string, moves ...string) ([]Eval, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return nil, err
	}

	a.waitReady()
	a.input <- fmt.Sprintf("position fen %s", fenPos)

	var searchMoves []string
	var evals Evals

	logInfo(fmt.Sprintf("start fen '%s' min_depth=%d", fenPos, opts.MinDepth))

	evals, err = a.analyzePosition(ctx, opts, fenPos, moves)
	if err != nil {
		return nil, fmt.Errorf("searchmoves '%v': %v", searchMoves, err)
	}

	if wg != nil {
		logInfo("sending quit")
		a.input <- "quit"

		cancel()
		wg.Wait()
	}

	return evals, nil
}

func (a *Analyzer) analyzePosition(ctx context.Context, opts AnalysisOptions, fenPos string, moves []string) ([]Eval, error) {
	board := fen.FENtoBoard(fenPos)

	if board.IsMate() {
		return nil, fmt.Errorf("TODO: position '%s' is already game over", fenPos)
	}

	if len(moves) != 0 {
		a.input <- fmt.Sprintf("setoption name MultiPV value %d", len(moves))
		a.input <- fmt.Sprintf("go depth %d movetime %d searchmoves %s", opts.MaxDepth, opts.MaxTime.Milliseconds(), strings.Join(moves, " "))
	} else {
		a.input <- fmt.Sprintf("setoption name MultiPV value 1")
		a.input <- fmt.Sprintf("go depth %d movetime %d", opts.MaxDepth, opts.MaxTime.Milliseconds())
	}

	evals := a.engineEvals(ctx, opts, fenPos)
	if len(evals) == 0 {
		return nil, fmt.Errorf("no evaluations returned for fen '%s'", fenPos)
	}

	logInfo("")
	//newestEvals := maxDepthEvals(evals)
	var best Eval
	for _, eval := range evals {
		if eval.Depth > best.Depth {
			best = eval
		} else if eval.Depth == best.Depth && eval.Score() > best.Score() {
			best = eval
		}
		//wc := evalWinningChances(eval)
		//diff := diffWC(eval, bestMoveAtDepth)

		san := board.UCItoSAN(eval.UCIMove)
		//newestEvals = append(newestEvals, eval.Clone())

		logInfo(fmt.Sprintf("    depth: %2d depth_delta: %2d move: %5s %-7s cp: %6d mate: %3d", eval.Depth, eval.DepthDelta, eval.UCIMove, san, eval.CP, eval.Mate))
		// wc: %6.2f wc_diff: %6.2f" , wc, diff)
	}
	logInfo("")

	cpSum := 0
	cpCount := 0
	for _, eval := range evals {
		if eval.UCIMove != best.UCIMove {
			break
		}
		cpSum += eval.CP
		cpCount++
		if cpCount == 5 {
			break
		}
	}
	best.CP = cpSum / cpCount

	//bestMove := bestEval(newestEvals).Clone()

	/*for _, eval := range newestEvals {
		san := board.UCItoSAN(eval.UCIMove)
		diff := diffWC(eval, bestMove)
		logInfo(fmt.Sprintf("*** depth: %d depth_delta: %d move: %-7s %s cp: %6d mate: %3d wc-diff: %0.2f", eval.Depth, eval.DepthDelta, san, eval.UCIMove, eval.POVCP(player), eval.POVMate(player), diff))
	}*/

	logInfo("")
	logInfo(fmt.Sprintf("%3d/%3d %3d. top_move: %-7s top_cp: %6d top_mate: %3d",
		1, 1, 1, board.UCItoSAN(best.UCIMove), best.CP, best.Mate))

	return []Eval{best}, nil
}

func debugEvalTable(startFEN string, movesEval Moves) string {
	var sb strings.Builder
	dbgBoard := fen.FENtoBoard(startFEN)

	firstMove := movesEval[0]
	firstMoveNumber := (firstMove.Ply / 2) + 1
	sb.WriteString(fmt.Sprintf("%3d. ", firstMoveNumber))
	firstPlayer := firstMove.Ply % 2
	if firstPlayer == 1 {
		sb.WriteString(fmt.Sprintf("%-7s%-2s %7s", "", "", ""))
		sb.WriteString(fmt.Sprintf("        %-7s %7s", "", ""))
	}

	for _, move := range movesEval {
		color := plyToColor(move.Ply)

		moveNumber := (move.Ply / 2) + 1
		if color == fen.WhitePieces {
			if moveNumber != firstMoveNumber {
				sb.WriteString(fmt.Sprintf("%3d. ", moveNumber))
			}
		} else {
			sb.WriteString("  |  ")
		}

		e1 := move.BestMove
		e2 := move.Eval

		var annotation string
		if !move.IsMate {
			diff := diffWC(e2, e1)
			if diff <= -0.3 {
				annotation = "??" // $4
			} else if diff <= -0.2 {
				annotation = "?" // $2
			} else if diff <= -0.1 {
				annotation = "?!" // $6
			}
		}

		sb.WriteString(fmt.Sprintf("%-7s%-2s %7s", move.SAN, annotation, move.Eval.String(color)))

		if move.UCI != move.BestMove.UCIMove {
			bestMoveSAN := dbgBoard.UCItoSAN(move.BestMove.UCIMove)
			sb.WriteString(fmt.Sprintf(" / top: %-7s %7s", bestMoveSAN, move.BestMove.String(color)))
		} else {
			sb.WriteString(fmt.Sprintf("        %-7s %7s", "", ""))
		}

		dbgBoard.Moves(move.UCI)

		if color == fen.BlackPieces {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func evalsToBookMove(boardFEN string, engineID string, moveEval Eval, evals []Eval) *yamlbook.Move {
	board := fen.FENtoBoard(boardFEN)

	move := &yamlbook.Move{
		Move:   board.UCItoSAN(moveEval.UCIMove),
		CP:     moveEval.CP,
		Mate:   moveEval.Mate,
		TS:     time.Now().Unix(),
		Engine: &yamlbook.Engine{ID: engineID},
	}

	for _, eval := range evals {
		move.Engine.Log(yamlbook.LogLine{
			Depth:    eval.Depth,
			SelDepth: eval.SelDepth,
			MultiPV:  eval.MultiPV,
			CP:       eval.CP,
			Mate:     eval.Mate,
			Nodes:    eval.Nodes,
			TBHits:   eval.TBHits,
			Time:     eval.Time,
			PV:       strings.Join(board.UCItoSANs(eval.PV...), " "),
		})
	}

	return move
}

func bookMoveToEval(bookMove *yamlbook.Move) Eval {
	logLine := bookMove.GetLastLogLineFor(bookMove.Move)
	boardFen := bookMove.FEN()
	board := fen.FENtoBoard(boardFen)
	pv, err := board.SANtoUCIs(strings.Split(logLine.PV, " ")...)
	if err != nil {
		panic(err)
	}

	return Eval{
		UCIMove:  bookMove.UCI(),
		Depth:    logLine.Depth,
		SelDepth: logLine.SelDepth,
		MultiPV:  logLine.MultiPV,
		CP:       logLine.CP,
		Mate:     logLine.Mate,
		Nodes:    logLine.Nodes,
		TBHits:   logLine.TBHits,
		Time:     logLine.Time,
		PV:       pv,
	}
}
