package analyze

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trollfish-lichess/fen"
)

const SyzygyPath = "/home/jud/projects/tablebases/3-4-5:/home/jud/projects/tablebases/wdl6:/home/jud/projects/tablebases/dtz6" // TODO: get path from config file

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 24
const threadsHashMultiplier = 2048 // 24*2048 = 49152
const hashMemory = threads * threadsHashMultiplier

const multiPV = 8

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

type MovesReverse []Move

func (moves MovesReverse) Less(i, j int) bool {
	return moves[i].Ply > moves[j].Ply
}

func (moves MovesReverse) Swap(i, j int) {
	moves[i], moves[j] = moves[j], moves[i]
}

func (moves MovesReverse) Len() int {
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

type Evals []Eval

func (e Evals) Find(uciMove string) (Eval, bool) {
	evals := maxDepthEvals(e)
	for _, eval := range evals {
		if eval.UCIMove == uciMove {
			return eval.Clone(), true
		}
	}
	return Eval{}, false
}

func (e Evals) Contains(uciMove string) bool {
	for _, eval := range e {
		if eval.UCIMove == uciMove {
			return true
		}
	}
	return false
}

type Eval struct {
	UCIMove    string   `json:"uci"`
	Depth      int      `json:"depth"`
	SelDepth   int      `json:"seldepth"`
	MultiPV    int      `json:"multipv"`
	CP         int      `json:"cp"`
	Mate       int      `json:"mate"`
	Nodes      int      `json:"nodes"`
	NPS        int      `json:"nps"`
	TBHits     int      `json:"tbhits"`
	Time       int      `json:"time"`
	UpperBound bool     `json:"ub,omitempty"`
	LowerBound bool     `json:"lb,omitempty"`
	PV         []string `json:"pv"`
	Mated      bool     `json:"mated,omitempty"`
	Raw        string   `json:"-"`
	DepthDelta int      `json:"delta"`
}

func (e Eval) Score() int {
	if e.Mate > 0 {
		return 400_00 - e.Mate*100 // closer mates equal higher numbers
	} else if e.Mate < 0 {
		return -300_00 + e.Mate*100 // mates further away equal more negative numbers
	}

	return e.CP
}

func (e Eval) Empty() bool {
	return e.UCIMove == ""
}

func (e Eval) Clone() Eval {
	clone := Eval{
		UCIMove:    e.UCIMove,
		Depth:      e.Depth,
		SelDepth:   e.SelDepth,
		MultiPV:    e.MultiPV,
		CP:         e.CP,
		Mate:       e.Mate,
		Nodes:      e.Nodes,
		NPS:        e.NPS,
		TBHits:     e.TBHits,
		Time:       e.Time,
		UpperBound: e.UpperBound,
		LowerBound: e.LowerBound,
		PV:         e.PV,
		Mated:      e.Mated,
		Raw:        e.Raw,
		DepthDelta: e.DepthDelta,
	}
	clone.PV = make([]string, len(e.PV))
	copy(clone.PV, e.PV)
	return clone
}

func (e Eval) POVCP(color fen.Color) int {
	return e.CP * int(color)
}

func (e Eval) POVMate(color fen.Color) int {
	return e.Mate * int(color)
}

func (e Eval) String(color fen.Color) string {
	if e.Mated {
		return ""
	}

	if e.Mate != 0 {
		return fmt.Sprintf("#%d", e.POVMate(color))
	}

	s := fmt.Sprintf("%.2f", float64(e.POVCP(color)/100))

	if s == "+0.00" || s == "-0.00" {
		return "0.00"
	}

	return s
}

func parseEval(line string) Eval {
	parts := strings.Split(line, " ")
	var eval Eval

scoreLoop:
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		inc := 1
		switch p {
		case "depth":
			eval.Depth = atoi(parts[i+1])
		case "seldepth":
			eval.SelDepth = atoi(parts[i+1])
		case "multipv":
			eval.MultiPV = atoi(parts[i+1])
		case "score":
			p2 := parts[i+1]
			switch p2 {
			case "cp":
				eval.CP = atoi(parts[i+2])
				inc++
			case "mate":
				eval.Mate = atoi(parts[i+2])
				inc++
			default:
				log.Fatalf("unhandled: 'info ... score %s'", p2)
			}
		case "upperbound":
			eval.UpperBound = true
			inc = 0
		case "lowerbound":
			eval.LowerBound = true
			inc = 0
		case "nodes":
			eval.Nodes = atoi(parts[i+1])
		case "nps":
			eval.NPS = atoi(parts[i+1])
		case "tbhits":
			eval.TBHits = atoi(parts[i+1])
		case "time":
			eval.Time = atoi(parts[i+1])
		case "hashfull":
			// ignore
		case "pv":
			pvMoves := parts[i+1:]
			eval.PV = pvMoves
			eval.UCIMove = pvMoves[0]
			break scoreLoop
		default:
			log.Fatalf("unhandled: 'info ... %s'", p)
		}

		i += inc
	}

	return eval
}

func bestEval(evals []Eval) Eval {
	if len(evals) == 0 {
		log.Fatalf("len(evals) = 0")
	}

	evals = maxDepthEvals(evals)
	return evals[0].Clone()
}

func maxDepthEvals(evals []Eval) []Eval {
	var maxDepth int
	maxDepthEvals := make([]Eval, 0, 8)

	// find maxDepth
	for _, eval := range evals {
		if eval.Depth > maxDepth {
			maxDepth = eval.Depth
			maxDepthEvals = maxDepthEvals[:0]
		}
		if eval.Depth == maxDepth {
			maxDepthEvals = append(maxDepthEvals, eval)
		}
	}

	sort.Slice(maxDepthEvals, func(i, j int) bool {
		score1 := evals[i].Score()
		score2 := evals[j].Score()

		if score1 != score2 {
			return score1 > score2
		}
		return evals[i].UCIMove < evals[j].UCIMove
	})

	return maxDepthEvals
}

func (a *Analyzer) getLines(ctx context.Context, opts AnalysisOptions, fenPos string) []Eval {
	start := time.Now()

	var moves []Eval

	var maxDepth int
	var stopped bool
	var printEngineOutput bool

	showEngineOutputAfter := 20 * time.Second
	floorDepth := opts.MinDepth - opts.DepthDelta + 1
	ignoreDepthsGreaterThan := 255

	minTimeMS := int(opts.MinTime.Milliseconds())
	timeout := time.NewTimer(opts.MaxTime)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case line := <-a.output:
			if strings.HasPrefix(line, "bestmove") {
				a.input <- "stop"
				break loop
			}

			if !printEngineOutput && time.Since(start) > showEngineOutputAfter {
				printEngineOutput = true
			}
			if printEngineOutput {
				if showEngineOutput(line) {
					//logInfo(fmt.Sprintf("t=%v/%v <- %s", time.Since(start).Round(time.Second), check.MaxTime, line))
				}
			}

			if strings.HasPrefix(line, "info ") && strings.Contains(line, "score") {
				eval := parseEval(line)
				eval.Raw = line

				if eval.UpperBound || eval.LowerBound {
					continue
				}
				if eval.Depth > ignoreDepthsGreaterThan {
					continue
				}

				if eval.Depth > maxDepth {
					maxDepth = eval.Depth
				}

				// remove evals at same depth + PV[0] with fewer nodes searched
				for i := 0; i < len(moves); i++ {
					if moves[i].Depth == eval.Depth && moves[i].UCIMove == eval.UCIMove {
						if moves[i].Nodes <= eval.Nodes {
							moves = append(moves[:i], moves[i+1:]...)
							i--
							continue
						}
					}
				}

				moves = append(moves, eval)

				sort.Slice(moves, func(i, j int) bool {
					if moves[i].Depth != moves[j].Depth {
						return moves[i].Depth > moves[j].Depth
					}

					if moves[i].MultiPV != moves[j].MultiPV {
						return moves[i].MultiPV < moves[j].MultiPV
					}

					if moves[i].Nodes != moves[j].Nodes {
						return moves[i].Nodes > moves[j].Nodes
					}

					return moves[i].Time > moves[j].Time
				})

				if eval.Depth >= opts.MinDepth && len(moves) > 0 {
					delta := 1
					move := moves[0].UCIMove
					for i := 1; i < len(moves); i++ {
						if moves[i].MultiPV != 1 || moves[i].Depth < floorDepth {
							continue
						}
						if moves[i].UCIMove == move {
							delta++
						} else {
							break
						}
					}
					if eval.Time >= minTimeMS {
						board := fen.FENtoBoard(fenPos)
						globalCP := eval.POVCP(board.ActiveColor)
						globalMate := eval.POVMate(board.ActiveColor)
						san := board.UCItoSAN(eval.UCIMove)

						t := fmt.Sprintf("t=%5v/%v", time.Since(start).Round(time.Second), opts.MaxTime)
						if delta >= opts.DepthDelta {
							logInfo(fmt.Sprintf("%s delta %d >= %d @ depth %d. move: %7s %s cp: %d mate: %d", t, delta, opts.DepthDelta, eval.Depth, san, eval.UCIMove, globalCP, globalMate))
							ignoreDepthsGreaterThan = eval.Depth
							a.input <- "stop"
						} else {
							logInfo(fmt.Sprintf("%s delta %d < %d  @ depth %d. move: %7s %s cp: %d mate: %d", t, delta, opts.DepthDelta, eval.Depth, san, eval.UCIMove, globalCP, globalMate))
						}
					}
				}
			}

		case <-timeout.C:
			if maxDepth == 0 {
				return nil
			}
			logInfo(fmt.Sprintf("per-move timeout expired (%v), using what we have at depth %d", opts.MaxTime, maxDepth))
			a.input <- "stop"
			stopped = true
		}
	}

	if !stopped {
		// drain timeout channel
		if !timeout.Stop() {
			<-timeout.C
		}
	}

	// remove evals with a lower depth or with less nodes searched
	depth1Count, depth2Count := 0, 0

	for i := 0; i < len(moves); i++ {
		if moves[i].Depth == maxDepth {
			depth1Count++
		}
		if moves[i].Depth == maxDepth-1 {
			depth2Count++
		}
	}

	if depth1Count < depth2Count {
		logInfo(fmt.Sprintf("depth: %d depth1Count (%d) < depth2Count (%d), using depth: %d", maxDepth, depth1Count, depth2Count, maxDepth-1))
		maxDepth--
	}

	for i := 0; i < len(moves); i++ {
		if moves[i].Depth > maxDepth || moves[i].MultiPV != 1 {
			moves = append(moves[:i], moves[i+1:]...)
			i--
			continue
		}
	}

	moves[len(moves)-1].DepthDelta = 1
	prev := moves[len(moves)-1]
	for i := len(moves) - 2; i >= 0; i-- {
		if moves[i].UCIMove == prev.UCIMove {
			moves[i].DepthDelta = prev.DepthDelta + 1
		} else {
			moves[i].DepthDelta = 1
		}
		prev = moves[i]
	}

	cur := prev
	if cur.DepthDelta < opts.DepthDelta {
		for i := 1; i < len(moves); i++ {
			move := moves[i]
			if move.Depth < opts.MinDepth {
				break
			}
			if move.DepthDelta > cur.DepthDelta {
				cur = move
				if cur.DepthDelta >= opts.DepthDelta {
					moves = moves[i:]
				}
			}
		}
	}

	return moves
}

func New() *Analyzer {
	return &Analyzer{
		input:  make(chan string, 512),
		output: make(chan string, 512),
	}
}

type Analyzer struct {
	logEngineMtx     sync.Mutex
	input            chan string
	output           chan string
	stockfishStarted int64
}

func (a *Analyzer) AnalyzePGNFile(ctx context.Context, opts AnalysisOptions, pgnFilename string) error {
	db, err := fen.LoadPGNDatabase(pgnFilename)
	if err != nil {
		return err
	}

	for _, game := range db.Games {
		moves := make([]string, 0, len(game.Moves))
		for _, move := range game.Moves {
			moves = append(moves, move.UCI)
		}

		if err := a.AnalyzeGame(ctx, opts, moves); err != nil {
			return err
		}
	}

	return nil
}

func (a *Analyzer) AnalyzeGame(ctx context.Context, opts AnalysisOptions, moves []string) error {
	logInfo(fmt.Sprintf("start game analysis, %d moves (%d plies)", (len(moves)+1)/2, len(moves)))

	// lowercase all moves
	for i := 0; i < len(moves); i++ {
		moves[i] = strings.ToLower(moves[i])
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return err
	}

	var movesEval Moves

gameMovesLoop:
	for i := len(moves) - 1; i >= 0; i-- {
		playerMoveUCI := moves[i]

		player := plyToColor(i)

		beforeMoves := moves[0:i]

		board := fen.FENtoBoard(startPosFEN)
		board.Moves(beforeMoves...)
		sanMove := board.UCItoSAN(playerMoveUCI)

		if board.Clone().Moves(playerMoveUCI).IsMate() {
			// TODO: stalemate
			movesEval = append(movesEval, Move{
				Ply:      i,
				UCI:      playerMoveUCI,
				SAN:      sanMove,
				IsMate:   true,
				Eval:     Eval{UCIMove: playerMoveUCI, Mated: true},
				BestMove: Eval{UCIMove: playerMoveUCI, Mated: true},
			})
			continue
		}

		if len(movesEval) > 0 {
			startFEN := board.Clone().Moves(playerMoveUCI).FEN()

			pgn := evalToPGN(startFEN, 0, movesEval, false)
			logMultiline(pgn)
			if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
				return err
			}

			tbl := debugEvalTable(startFEN, movesEval)
			logMultiline(tbl)
		}

		// playerMoveUCI will come back
		evals, err := a.AnalyzePosition(ctx, opts, board.FEN(), playerMoveUCI)
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			break gameMovesLoop
		default:
		}

		evals = maxDepthEvals(evals)
		bestMove := bestEval(evals)

		for _, eval := range evals {
			logInfo(fmt.Sprintf("depth=%d move=%s pov_cp=%d", eval.Depth, eval.UCIMove, eval.POVCP(player)))
		}

		newMove := Move{
			Ply:    i,
			UCI:    playerMoveUCI,
			SAN:    sanMove,
			IsMate: board.IsMate(),
		}

		var playerMove Eval
		for _, checkMove := range evals {
			if checkMove.UCIMove == playerMoveUCI {
				playerMove = checkMove.Clone()
				break
			}
		}

		if playerMove.UCIMove == "" {
			panic(fmt.Errorf("playerMove.UCIMove is empty"))
		}

		if playerMove.Score() >= bestMove.Score() {
			bestMove = playerMove.Clone()
		}

		// set played move + best move eval

		newMove.Eval = playerMove
		newMove.BestMove = bestMove

		// show output

		movesEval = append(movesEval, newMove)

		bestMoveSAN := board.UCItoSAN(bestMove.UCIMove)
		logInfo(fmt.Sprintf("%3d/%3d %3d. %-7s played_cp: %6d played_mate: %2d top_move: %-7s top_cp: %6d top_mate: %2d",
			i+1, len(moves), (i+2)/2,
			sanMove, playerMove.POVCP(player), playerMove.POVMate(player),
			bestMoveSAN, bestMove.POVCP(player), bestMove.POVMate(player),
		))
	}

	pgn := evalToPGN(startPosFEN, 0, movesEval, true)
	logMultiline(pgn)

	tbl := debugEvalTable(startPosFEN, movesEval)
	logMultiline(tbl)

	if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
		logMultiline(pgn)
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

func (a *Analyzer) waitReady() {
	a.input <- "isready"
	for line := range a.output {
		if line == "readyok" {
			break
		}
	}
}

func (a *Analyzer) analyzePosition(ctx context.Context, opts AnalysisOptions, fenPos string, moves []string) ([]Eval, error) {
	board := fen.FENtoBoard(fenPos)

	if board.IsMate() {
		return nil, fmt.Errorf("TODO: position '%s' is already game over", fenPos)
	}

	a.input <- fmt.Sprintf("setoption name MultiPV value 1")
	if len(moves) != 0 {
		a.input <- fmt.Sprintf("go depth %d movetime %d searchmoves %s", opts.MaxDepth, opts.MaxTime.Milliseconds(), strings.Join(moves, " "))
	} else {
		a.input <- fmt.Sprintf("go depth %d movetime %d", opts.MaxDepth, opts.MaxTime.Milliseconds())
	}

	evals := a.getLines(ctx, opts, fenPos)
	if len(evals) == 0 {
		return nil, fmt.Errorf("no evaluations returned for fen '%s'", fenPos)
	}

	logInfo("")
	//newestEvals := maxDepthEvals(evals)
	player := board.ActiveColor
	var best Eval
	for _, eval := range evals {
		if eval.Depth > best.Depth {
			best = eval.Clone()
		} else if eval.Depth == best.Depth && eval.Score() > best.Score() {
			best = eval.Clone()
		}
		const wc, diff = 0.0, 0.0
		//wc := evalWinningChances(eval)
		//diff := diffWC(eval, bestMoveAtDepth)

		san := board.UCItoSAN(eval.UCIMove)
		//newestEvals = append(newestEvals, eval.Clone())

		logInfo(fmt.Sprintf("    depth: %2d depth_delta: %2d move: %-7s %s cp: %6d mate: %3d wc: %6.2f wc_diff: %6.2f", eval.Depth, eval.DepthDelta, san, eval.UCIMove, eval.POVCP(player), eval.POVMate(player), wc, diff))
	}
	logInfo("")

	//bestMove := bestEval(newestEvals).Clone()

	/*for _, eval := range newestEvals {
		san := board.UCItoSAN(eval.UCIMove)
		diff := diffWC(eval, bestMove)
		logInfo(fmt.Sprintf("*** depth: %d depth_delta: %d move: %-7s %s cp: %6d mate: %3d wc-diff: %0.2f", eval.Depth, eval.DepthDelta, san, eval.UCIMove, eval.POVCP(player), eval.POVMate(player), diff))
	}*/

	logInfo("")
	logInfo(fmt.Sprintf("%3d/%3d %3d. top_move: %-7s top_cp: %6d top_mate: %3d",
		1, 1, 1, board.UCItoSAN(best.UCIMove), best.POVCP(player), best.POVMate(player)))

	return []Eval{best}, nil
}

func debugEvalTable(startFEN string, movesEval Moves) string {
	sort.Sort(movesEval)

	var sb strings.Builder
	board := fen.FENtoBoard(startFEN)

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
			bestMoveSAN := board.UCItoSAN(move.BestMove.UCIMove)
			sb.WriteString(fmt.Sprintf(" / top: %-7s %7s", bestMoveSAN, move.BestMove.String(color)))
		} else {
			sb.WriteString(fmt.Sprintf("        %-7s %7s", "", ""))
		}

		board.Moves(move.UCI)

		if color == fen.BlackPieces {
			sb.WriteString("\n")
		}
	}

	sort.Sort(MovesReverse(movesEval))

	return sb.String()
}

func evalToPGN(startFEN string, depth int, movesEval Moves, header bool) string {
	sort.Sort(movesEval)

	var sb strings.Builder

	if header {
		sb.WriteString(fmt.Sprintf("[Event \"?\"]\n"))
		sb.WriteString(fmt.Sprintf("[Site \"?\"]\n"))
		sb.WriteString(fmt.Sprintf("[Date \"????.??.??\"]\n"))
		sb.WriteString(fmt.Sprintf("[Round \"?\"]\n"))
		sb.WriteString(fmt.Sprintf("[White \"?\"]\n"))
		sb.WriteString(fmt.Sprintf("[Black \"?\"]\n"))
		sb.WriteString(fmt.Sprintf("[Result \"*\"]\n")) // TODO
		sb.WriteString(fmt.Sprintf("[Event \"?\"]\n"))
	}

	if startFEN != "" && startFEN != startPosFEN {
		sb.WriteString(fmt.Sprintf("[FEN \"%s\"]\n", startFEN))
		sb.WriteString("[Setup \"1\"]\n")
	}

	sb.WriteString(fmt.Sprintf("[Annotator \"Stockfish 15\"]\n"))
	if depth != 0 {
		sb.WriteString(fmt.Sprintf("[Depth \"%d\"]\n", depth))
	}
	sb.WriteString("\n")

	board := fen.FENtoBoard(startFEN)
	prevEval := "0.24"
	finalResult := "*"
	for _, move := range movesEval {
		moveNumber := (move.Ply / 2) + 1
		color := plyToColor(move.Ply)

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
		if !move.IsMate {
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

			showVariations = diff < 0 //diff <= -0.05
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
			if color == fen.WhitePieces {
				finalResult = "1-0"
			} else {
				finalResult = "0-1"
			}
		} else {
			sb.WriteString(fmt.Sprintf("    { [%%eval %s] }\n", move.Eval.String(color)))
		}

		if showVariations {
			//fmt.Printf("board.FullMove: %s\n", board.FullMove)
			writeVariation(&sb, board.Clone(), bestMove, "")
			writeVariation(&sb, board.Clone(), playedMove, annotation)
		}
		board.Moves(move.UCI)

		prevEval = move.Eval.String(color)
	}
	sb.WriteString(fmt.Sprintf("%s\n", finalResult)) // TODO: lazy, make this 1-0, 0-1, 1/2-1/2, or *

	sort.Sort(MovesReverse(movesEval))

	return sb.String()
}

func writeVariation(sb *strings.Builder, board *fen.Board, eval Eval, annotation string) {
	sb.WriteString("    ( ")

	used := 6

	basePly := (board.FullMove - 1) * 2
	if board.ActiveColor == fen.BlackPieces {
		basePly++
	}

	for j := 0; j < len(eval.PV); j++ {
		uci := eval.PV[j]
		san := board.UCItoSAN(uci)

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

		board.Moves(uci)
	}
	sb.WriteString(")\n")
}

func (a *Analyzer) StartStockfish(ctx context.Context) (*sync.WaitGroup, error) {
	if !atomic.CompareAndSwapInt64(&a.stockfishStarted, 0, 1) {
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, stockfishBinary)
	cmd.Dir = stockfishDir

	var wg sync.WaitGroup

	var readyOK int64 = 1

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case line := <-a.input:
				for atomic.LoadInt64(&readyOK) == 0 {
					time.Sleep(10 * time.Millisecond)
				}

				a.LogEngine(line)

				if line == "isready" {
					logInfo("-> isready")
					atomic.StoreInt64(&readyOK, 0)
				}

				_, err := stdin.Write([]byte(fmt.Sprintf("%s\n", line)))
				if err != nil {
					log.Fatalf("stdin.Write ERR: %v", err)
				}

			case <-ctx.Done():
				logInfo("exiting stdin loop")
				return
			}
		}
	}()

	// stderr loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := bufio.NewScanner(stderr)
		for r.Scan() {
			select {
			case <-ctx.Done():
				logInfo("exiting stderr loop (ctx.Done())")
				return
			default:
				line := r.Text()
				log.Printf(fmt.Sprintf("SF STDERR: %s\n", line))
			}
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("SF ERR: stderr: %v\n", err))
		}
		logInfo("exiting stderr loop")
	}()

	// stdout loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := bufio.NewScanner(stdout)
		var sentUCIInit bool
		for r.Scan() {
			select {
			case <-ctx.Done():
				logInfo("exiting stdout loop (ctx.Done())")
				return
			default:
			}

			line := r.Text()
			if showEngineOutput(line) {
				a.LogEngine(line)
			}

			if !sentUCIInit {
				a.input <- "uci"
				sentUCIInit = true
			}

			a.output <- line

			if line == "readyok" {
				logInfo("<- readyok")
				atomic.StoreInt64(&readyOK, 1)
			}
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("ERR: stdout: %v\n", err))
		}
		logInfo("exiting stdout loop")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cmd.Wait(); err != nil {
			if err.Error() != "signal: killed" {
				log.Fatal(fmt.Sprintf("SF CMD ERR: %v\n", err))
			}
		}
	}()

	// initialize parameters

	var sentNewGame bool

readyOKLoop:
	for line := range a.output {
		switch line {
		case "uciok":
			a.input <- fmt.Sprintf("setoption name Threads value %d", threads)
			a.input <- fmt.Sprintf("setoption name Hash value %d", hashMemory)
			a.input <- fmt.Sprintf("setoption name MultiPV value %d", multiPV)
			a.input <- fmt.Sprintf("setoption name SyzygyPath value %s", SyzygyPath)
			a.input <- fmt.Sprintf("setoption name UCI_AnalyseMode value true")
			/*
				setoption name Threads value 8
				setoption name Hash value 14336
				isready
				setoption name SyzygyPath value /data/syz/345:/data/syz/dtz6:/data/syz/wdl6
				setoption name UCI_AnalyseMode value true
				setoption name MultiPV value 12
				ucinewgame
				isready
				ucinewgame
				position fen
				go depth 32
			*/

			// setoption name Threads value 26
			// setoption name Hash value 90000
			// isready

			//
			/*

			 */

			// uci
			// setoption name Threads value 24
			// setoption name Hash value 49152
			// setoption name SyzygyPath value /home/jud/projects/tablebases/3-4-5:/home/jud/projects/tablebases/wdl6:/home/jud/projects/tablebases/dtz6
			// setoption name SyzygyPath value /data/syz/345:/data/syz/dtz6:/data/syz/wdl6
			// setoption name UCI_AnalyseMode value true
			// setoption name MultiPV value 12
			// isready
			// ucinewgame
			// position fen XXXX
			// go infinite
			// stop
			// isready
			// go infinite searchmoves c5d4 f8e7 c6d4 d8c7 b7b5 g7g6 b7b6stop
			// go infinite searchmoves b7b5 c5d4 g7g6 f8e7 d8c7
			// go depth 45 searchmoves b7b5 c5d4 g7g6 d8c7
			// d8b6 - clear loser (Qb6)
			// setoption name MultiPV value 3
			// go depth 50 searchmoves b7b5 c5d4 d8c7
			/*
				fo depth 34 seldepth 48 multipv 1 score cp -17 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv b7b5 a2a3 d8b6 c3a2 f7f6 e3f2 c5d4 e5f6 d7f6 a2b4 c6b4 f2d4 f8c5 a3b4 c5b4 c2c3 b4c5 b2b4 c5d4 d1d4 b6d4 f3d4 c8d7 e2f3 e8e7 e1d2 e7d6 h2h4 h7h6 h4h5 h8e8 a1a5 e6e5 f4e5 d6e5 h1a1 f6e4 f3e4 d5e4 a5a6 a8a6 a1a6
				info depth 34 seldepth 44 multipv 2 score cp -25 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv g7g6 h2h4 h7h5 h1h3 c5d4 f3d4 f8e7 d1d2 e7h4 e1f1 h4e7 g2g4 h5h4 g4g5 c6d4 e3d4 b7b5 e2d3 e7c5 c3e2 d8b6 d2e3 b5b4 f1g2 a6a5 a1h1 c8a6 h3h4 h8h4 h1h4 e8e7 d3a6 c5d4 e2d4 b6a6 a2a3 a6b6 a3b4 a5b4 h4h7
				info depth 34 seldepth 47 multipv 3 score cp -28 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv c5d4 f3d4 f8c5 h2h4 h8f8 d1d2 c6d4 e3d4 c5d4 d2d4 f7f6 e1d2 d8b6 d4b6 d7b6 e5f6 g7f6 a1g1 e6e5 g2g4 e8e7 g4g5 c8f5 g5f6 e7f6 f4e5 f6e6 e2d3 b6c4 d2c1 a8c8 g1g3 c4e5 h1e1 e6d6 d3f5 f8f5
				info depth 34 seldepth 49 multipv 4 score cp -29 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv d8c7 d1d2 c5d4 f3d4 h7h5 h2h4 d7b6 b2b3 c8d7 e2d3 c6d4 e3d4 b6c8 a2a3 f8c5 d4c5 c7c5 a3a4 c8e7 c3e2 e8f8 b3b4 c5c7 c2c3 g7g6 e1f2 a8c8 h1c1 f8g7 e2d4 e7g8 a4a5 c7d8 g2g3 g8h6 f2g1 h6f5
				info depth 33 seldepth 38 multipv 5 score cp -44 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv b7b6 d1d2 c8b7 c3d1 c5d4 f3d4 c6a5 b2b3 d8c7 c2c3 f8e7 a1c1 a8c8 h2h4 h7h5 d1f2 a5c6 d2d1 g7g6 e1f1 e7a3 c1c2 c6d4 e3d4 d7b8 g2g4 b8c6 e2d3 c6d4 c3d4 c7d8 f1g2 c8c2 d1c2 a3e7 g4g5 e7b4
				info depth 33 seldepth 44 multipv 6 score cp -51 nodes 3221860987 nps 18742864 hashfull 422 tbhits 31 time 171898 pv f8e7 a2a3 d8c7 d4c5 e7c5 e3c5 d7c5 d1d2 c5d7 e2d3 f7f6 e5f6 d7f6 e1d1 h8f8 h1e1 e8f7 b2b3 c8d7 d1c1 f7g8 g2g3 a8c8 c1b2 b7b5 c3a2 a6a5 a2c3 b5b4 c3b5 b4a3 a1a3 c7b8 f3d4 c6d4 b5d4 b8b6 d4f3 a5a4
				info depth 34 currmove b7b6 cu
			*/

			// go infinite searchmoves c5d4 b7b5 f8e7
			a.input <- "isready"
		case "readyok":
			if sentNewGame {
				break readyOKLoop
			}
			sentNewGame = true
			a.input <- "ucinewgame"
			a.input <- "isready"
		}
	}

	return &wg, nil
}

func showEngineOutput(line string) bool {
	parts := strings.Split(line, " ")
	if len(parts) == 7 {
		if parts[0] == "info" && parts[1] == "depth" && parts[3] == "currmove" && parts[5] == "currmovenumber" {
			return false
		}
	}
	return true
}

func logInfo(msg string) {
	_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", ts(), strings.TrimRight(msg, "\n"))
}

func logMultiline(s string) {
	parts := strings.Split(s, "\n")
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(fmt.Sprintf("%s\n", part))
	}
	_, _ = fmt.Fprint(os.Stderr, sb.String())
}

func ts() string {
	return fmt.Sprintf("[%s]", time.Now().Format("2006-01-02 15:04:05.000"))
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatal(err)
	}
	return n
}

func (a *Analyzer) LogEngine(s string) {
	a.logEngineMtx.Lock()
	_, _ = fmt.Fprintln(os.Stdout, s)
	a.logEngineMtx.Unlock()
}

func plyToColor(ply int) fen.Color {
	if ply%2 == 0 {
		return fen.WhitePieces
	}
	return fen.BlackPieces
}
