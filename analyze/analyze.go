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

const SyzygyPath = "/home/jud/projects/tablebases/3-4-5:/home/jud/projects/tablebases/wdl6:/home/jud/projects/tablebases/dtz6:/home/jud/projects/tablebases/7:/home/jud/projects/tablebases/dtz7" // TODO: get path from config file

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 28
const hashMemory = 90112        // aim for 70% hashfull
const maxNodes = 20_852_058_695 // should be about 100% hashfull
//const maxNodes = 25_156_594_000 // arbitrarily large value (nps * 1000)

// TODO: put in config
const stockfishBinary = "/home/jud/projects/trollfish/stockfish/stockfish"
const stockfishDir = "/home/jud/projects/trollfish/stockfish"

type AnalysisOptions struct {
	MinDepth   int
	MaxDepth   int
	MinTime    time.Duration
	MaxTime    time.Duration
	DepthDelta int
	MultiPV    int
	MinNodes   int
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
		legalMoveCount := len(board.AllLegalMoves())

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
			pgn := evalToPGN(pgn, movesEval)
			logMultiline(pgn)
			//if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
			//	return err
			//}

			tbl := debugEvalTable(startPosFEN, movesEval)
			logMultiline(tbl)
		}

		bookMoves, _ := book.Get(boardFEN)

		diffTS := bookMoves.HaveDifferentTimestamps()
		tooFewMoves := len(bookMoves) < 3 && len(bookMoves) != legalMoveCount
		tooOld := bookMoves.TooOld()

		needsUpdate := diffTS || tooFewMoves || tooOld
		updateBookMoves := board.FullMove != 1 && needsUpdate

		if updateBookMoves {
			ucis := bookMoves.UCIs()
			fmt.Printf("UCIs: %v\n", ucis)

			var evals []Eval
			if len(ucis) < opts.MultiPV && len(ucis) != legalMoveCount {
				// TODO: if UCIs don't show up re-run analysis
				evals, err = a.AnalyzePosition(ctx, opts, boardFEN)
			} else {
				evals, err = a.AnalyzePosition(ctx, opts, boardFEN, ucis...)
			}

			if err != nil {
				return err
			}

			fmt.Printf("UCIs: %v\n", ucis)
			fmt.Printf("len(bokMoves): %d\n", len(bookMoves))
			fmt.Printf("len(evals): %d\n", len(evals))

			if err := a.SaveEvalsToBook(book, boardFEN, evals); err != nil {
				return err
			}

			bookMoves, _ = book.Get(boardFEN)
			fmt.Printf("super sketchy code done, go check it out\n")
			for _, bookMove := range bookMoves {
				fmt.Printf("book move: %s cp: %3d fen: %-72s\n", bookMove.Move, bookMove.CP, bookMove.FEN())
			}
		}

		bestMove := bookMoves.GetBestMoveByEval(playerMoveUCI)

		if bestMove == nil {
			logInfo("running engine to find best move...")
			evals, err := a.AnalyzePosition(ctx, opts, boardFEN)
			if err != nil {
				return err
			}

			for _, eval := range evals {
				logInfo(fmt.Sprintf("depth: %d move: %s global_cp: %4d global_mate: %4d", eval.Depth, eval.UCIMove, eval.GlobalCP(player), eval.GlobalMate(player)))
			}

			if err := a.SaveEvalsToBook(book, boardFEN, evals); err != nil {
				return err
			}

			bookMoves, _ = book.Get(boardFEN)
			for _, bookMove := range bookMoves {
				fmt.Printf("new: %s %v\n", bookMove.Move, *bookMove)
			}
		}

		bestMove = bookMoves.GetBestMoveByEval(playerMoveUCI)

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

			bookMovesPlusPlayerMoves := []string{playerMoveUCI}
			bookMovesPlusPlayerMoves = append(bookMovesPlusPlayerMoves, bookMoves.UCIs()...)

			evals, err := a.AnalyzePosition(ctx, opts, boardFEN, bookMovesPlusPlayerMoves...)
			if err != nil {
				return err
			}

			for _, eval := range evals {
				logInfo(fmt.Sprintf("depth: %d move: %s global_cp: %4d global_mate: %4d", eval.Depth, eval.UCIMove, eval.GlobalCP(player), eval.GlobalMate(player)))
			}

			if err := a.SaveEvalsToBook(book, boardFEN, evals); err != nil {
				return err
			}

			bookMoves, _ = book.Get(boardFEN)
			for _, bookMove := range bookMoves {
				fmt.Printf("new: %s %v\n", bookMove.Move, *bookMove)
			}

			bestMove = bookMoves.GetBestMoveByEval(playerMoveUCI)
			if bestMove.Move == playerMoveSAN {
				playerMove = bestMove
			} else {
				playerMove = bookMoves.GetSAN(playerMoveSAN)
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

	evalPGN := evalToPGN(pgn, movesEval)
	logMultiline(evalPGN)

	tbl := debugEvalTable(startPosFEN, movesEval)
	logMultiline(tbl)

	if err := ioutil.WriteFile(fmt.Sprintf("eval%d.pgn", time.Now().Unix()), []byte(evalPGN), 0644); err != nil {
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

	var moveCount int
	if len(moves) != 0 {
		if len(moves) == 1 {
			panic(fmt.Errorf("len(moves) = %d; most likely not intended. moves: %v", len(moves), moves))
		}
		moveCount = len(moves)
		a.input <- fmt.Sprintf("setoption name MultiPV value %d", len(moves))
		a.input <- fmt.Sprintf("go depth %d nodes %d movetime %d searchmoves %s", opts.MaxDepth, maxNodes, opts.MaxTime.Milliseconds(), strings.Join(moves, " "))
	} else {
		moveCount = opts.MultiPV
		a.input <- fmt.Sprintf("setoption name MultiPV value %d", opts.MultiPV)
		a.input <- fmt.Sprintf("go depth %d nodes %d movetime %d", opts.MaxDepth, maxNodes, opts.MaxTime.Milliseconds())
	}

	evals := a.engineEvals(ctx, opts, fenPos, moveCount)
	if len(evals) == 0 {
		return nil, fmt.Errorf("no evaluations returned for fen '%s'", fenPos)
	}

	logInfo("")
	var best Eval
	for _, eval := range evals {
		if eval.Depth > best.Depth {
			best = eval
		} else if eval.Depth == best.Depth && eval.Score() > best.Score() {
			best = eval
		}

		san := board.UCItoSAN(eval.UCIMove)

		logInfo(fmt.Sprintf("    depth: %2d move: %5s %-7s cp: %6d mate: %3d", eval.Depth, eval.UCIMove, san, eval.CP, eval.Mate))
	}
	logInfo("")

	for _, move := range moves {
		var count int

		maxDepth := 0
		for i := 0; i < len(evals); i++ {
			eval := evals[i]

			if eval.UCIMove != move {
				continue
			}

			if eval.Depth > maxDepth {
				maxDepth = eval.Depth
			}

			// keep only the last 5 depths
			if count >= 5 {
				evals = append(evals[:i], evals[i+1:]...)
				i--
				continue
			}

			count++
		}
	}

	logInfo("")
	logInfo(fmt.Sprintf("%3d/%3d %3d. top_move: %-7s top_cp: %6d top_mate: %3d",
		1, 1, 1, board.UCItoSAN(best.UCIMove), best.CP, best.Mate))

	return evals, nil
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

func (a *Analyzer) SaveEvalsToBook(book *yamlbook.Book, boardFEN string, evals []Eval) error {
	if len(evals) == 0 {
		return nil
	}

	depth := evals[0].Depth
	count := 0
	for i := 0; i < len(evals); i++ {
		if evals[i].Depth < depth {
			break
		}
		count++
	}

	for i := 0; i < count; i++ {
		bookMove := evalsToBookMove(boardFEN, "sf15", evals[i], evals)
		book.Add(boardFEN, bookMove)
	}

	if err := book.Save(); err != nil {
		return err
	}

	return nil
}

func evalsToBookMove(boardFEN string, engineID string, moveEval Eval, evals []Eval) *yamlbook.Move {
	board := fen.FENtoBoard(boardFEN)

	move := yamlbook.NewMove(boardFEN, yamlbook.Move{
		Move:   board.UCItoSAN(moveEval.UCIMove),
		CP:     moveEval.CP,
		Mate:   moveEval.Mate,
		TS:     time.Now().Unix(),
		Engine: &yamlbook.Engine{ID: engineID},
	})

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
