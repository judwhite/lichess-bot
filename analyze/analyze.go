package analyze

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
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

//const hashMemory = 95_000
const multiPV = 12

const Engine_Stockfish_15_NN_6e0680e = 1

const depthTier1 = 18
const depthTier2 = 31
const depthTier3 = 41
const cutoff = -0.12

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
	Ply        int    `json:"ply"`
	UCI        string `json:"uci"`
	SAN        string `json:"san"`
	Eval       Eval   `json:"eval"`
	BestMove   Eval   `json:"best_move"`
	IsMate     bool   `json:"mate,omitempty"`
	PV         string `json:"pv,omitempty"`
	OtherEvals []Eval `json:"-"`
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
	}
	clone.PV = make([]string, len(e.PV))
	copy(clone.PV, e.PV)
	return clone
}

func (e Eval) String() string {
	if e.Mated {
		return ""
	}

	if e.Mate != 0 {
		return fmt.Sprintf("#%d", e.Mate)
	}

	var sbEval strings.Builder
	/*if e.CP > 0 {
		sbEval.WriteRune('+')
	}*/
	sbEval.WriteString(fmt.Sprintf("%.2f", float64(e.CP)/100))
	s := sbEval.String()

	if s == "+0.00" || s == "-0.00" {
		return "0.00"
		//return fmt.Sprintf("0.00 %#v", e)
	}

	return s
}

func rawWinningChances(cp float64) float64 {
	return 2/(1+math.Exp(-0.004*cp)) - 1
}

func cpWinningChances(cp int) float64 {
	return rawWinningChances(math.Min(math.Max(-1000, float64(cp)), 1000))
}

func mateWinningChances(mate int) float64 {
	cp := (21 - math.Min(10, math.Abs(float64(mate)))) * 100
	signed := cp
	if mate < 0 {
		signed *= -1
	}
	return rawWinningChances(signed)
}

func evalWinningChances(eval Eval) float64 {
	if eval.Mate != 0 {
		return mateWinningChances(eval.Mate)
	}
	return cpWinningChances(eval.CP)
}

// povChances computes winning chances for a color
// 1  infinitely winning
// -1 infinitely losing
func povChances(color int, eval Eval) float64 {
	chances := evalWinningChances(eval)
	switch color {
	case 0:
		return chances
	default:
		return -chances
	}
}

// povDiff computes the difference, in winning chances, between two evaluations
// 1  = e1 is infinitely better than e2
// -1 = e1 is infinitely worse  than e2
func povDiff(color int, e2 Eval, e1 Eval) float64 {
	return povChances(color, e2) - povChances(color, e1)
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

	evals = evalsWithHighestDepth(evals)
	return evals[0].Clone()
}

func evalsWithHighestDepth(evals []Eval) []Eval {
	var maxDepth int
	var hdEvals []Eval

	// find maxDepth
	for _, eval := range evals {
		if eval.Depth > maxDepth {
			maxDepth = eval.Depth
		}
	}

	// make new list.. could do this one-shot, but mehh + memory allocation
	for _, eval := range evals {
		if eval.Depth == maxDepth {
			hdEvals = append(hdEvals, eval)
		}
	}

	sort.Slice(evals, func(i, j int) bool {
		if evals[i].Depth != evals[j].Depth {
			return evals[i].Depth < evals[j].Depth
		}
		if evals[i].CP != evals[j].CP {
			return evals[i].CP < evals[j].CP
		}
		return evals[i].UCIMove < evals[j].UCIMove
	})

	return hdEvals
}

func (a *Analyzer) getLines(ctx context.Context, ply, totalPlies int, maxTimePerPly time.Duration, allDepths bool) []Eval {
	start := time.Now()

	var moves []Eval

	var maxDepth int
	var stopped bool
	//var printEngineOutput bool

	timeout := time.NewTimer(maxTimePerPly)
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

			/*if !printEngineOutput && time.Since(start) > showEngineOutputAfter {
				printEngineOutput = true
			}
			if printEngineOutput {
				logInfo(fmt.Sprintf("ply=%d/%d t=%v/%v <- %s", ply+1, totalPlies, time.Since(start).Round(time.Second), maxTimePerPly, line))
			}*/
			if showEngineOutput(line) {
				logInfo(fmt.Sprintf("ply=%d/%d t=%v/%v <- %s", ply+1, totalPlies, time.Since(start).Round(time.Second), maxTimePerPly, line))
			}

			if strings.HasPrefix(line, "info ") && strings.Contains(line, "score") {
				eval := parseEval(line)
				eval.Raw = line

				if eval.UpperBound || eval.LowerBound {
					continue
				}

				if eval.Depth > maxDepth {
					maxDepth = eval.Depth
				}

				// remove evals with less nodes searched
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
			}

		case <-timeout.C:
			if maxDepth == 0 {
				return nil
			}
			logInfo(fmt.Sprintf("per-move timeout expired (%v), using what we have at depth %d", maxTimePerPly, maxDepth))
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

	remove := func(i int) bool {
		if moves[i].Depth < 10 {
			return true
		}
		if allDepths {
			return moves[i].Depth > maxDepth
		}
		return moves[i].Depth != maxDepth
	}

	for i := 0; i < len(moves); i++ {
		if remove(i) {
			moves = append(moves[:i], moves[i+1:]...)
			i--
			continue
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
	logEngineMtx sync.Mutex
	input        chan string
	output       chan string
}

func (a *Analyzer) AnalyzeGame(ctx context.Context, moves []string, depth int, maxTimePerPly time.Duration) error {
	logInfo("start")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg, err := a.startStockfish(ctx)
	if err != nil {
		return err
	}

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

	sf := make(chan []Eval)

	var curPly int64

	go func() {
		totalPlies := len(moves)
		for {
			ply := int(atomic.LoadInt64(&curPly))
			evals := a.getLines(ctx, ply, totalPlies, maxTimePerPly, true)
			select {
			case <-ctx.Done():
				logInfo("debug: getLines loop exited")
				return
			default:
			}
			sf <- evals
		}
	}()

	var movesEval Moves

	logInfo(fmt.Sprintf("depth = %d", depth))

	//for i := 0; i < len(moves); i++ {
	for i := len(moves) - 1; i >= 0; i-- {
		atomic.StoreInt64(&curPly, int64(i))

		playerMoveUCI := moves[i]
		var povMultiplier int
		if i%2 == 0 {
			povMultiplier = 1
		} else {
			povMultiplier = -1
		}

		beforeMoves := moves[0:i]

		board := fen.FENtoBoard(startPosFEN)
		board.Moves(beforeMoves...)
		sanMove := board.UCItoSAN(playerMoveUCI)

		if board.Clone().Moves(playerMoveUCI).IsMate() {
			// TODO: stalemate
			movesEval = append(movesEval, Move{
				Ply:    i,
				UCI:    playerMoveUCI,
				SAN:    sanMove,
				IsMate: true,
				Eval:   Eval{Mated: true},
			})
			continue
		}

		if len(movesEval) > 0 {
			startFEN := board.Clone().Moves(playerMoveUCI).FEN()

			pgn := evalToPGN(startFEN, depth, movesEval, false)
			logMultiline(pgn)

			tbl := debugEvalTable(startFEN, movesEval)
			logMultiline(tbl)

			/*bookMoves := createOpeningBook(startFEN, movesEval)
			if err := saveBookMoves(bookMoves); err != nil {
				log.Fatal(err)
			}*/
		}

		// TODO: check if player's move was in initial multipv/depth check

		m := strings.Join(beforeMoves, " ")
		a.input <- fmt.Sprintf("position fen %s moves %s", startPosFEN, m)
		a.input <- fmt.Sprintf("go depth %d", depth)

	loop:
		for {
			select {
			case evals := <-sf:
				if len(evals) == 0 {
					break loop
				}

				bestMove := bestEval(evals)

				for _, eval := range evals {
					logInfo(fmt.Sprintf("depth=%d move=%s cp=%d", eval.Depth, eval.UCIMove, eval.CP))
				}

				newMove := Move{
					Ply:        i,
					UCI:        playerMoveUCI,
					SAN:        sanMove,
					BestMove:   bestMove,
					IsMate:     board.IsMate(),
					OtherEvals: append([]Eval(nil), evals...),
				}

				// not the best move, eval player's move
				if bestMove.UCIMove != playerMoveUCI {
					a.input <- fmt.Sprintf("go depth %d searchmoves %s", depth, playerMoveUCI)

					evals := <-sf

					highestDepth := evalsWithHighestDepth(evals)

					bestMove = bestEval(highestDepth)

					var playerMove Eval
					for i := 0; i < len(highestDepth); i++ {
						e := highestDepth[i]
						if e.UCIMove == playerMoveUCI {
							playerMove = e
							break
						}
					}
					newMove.OtherEvals = append(newMove.OtherEvals, evals...)

					playerScore := playerMove.CP
					bestMoveScore := bestMove.CP

					if playerMove.Mate != 0 {
						if playerMove.Mate > 0 {
							playerScore = 100_000 - playerMove.Mate
						} else {
							playerScore = -100_000 - playerMove.Mate
						}
					}
					if bestMove.Mate != 0 {
						if bestMove.Mate > 0 {
							bestMoveScore = 100_000 - bestMove.Mate
						} else {
							bestMoveScore = -100_000 - bestMove.Mate
						}
					}

					if playerScore > bestMoveScore {
						logInfo(fmt.Sprintf("- player's move was better than suggested line, %3d is better than %3d", playerMove.CP, bestMove.CP))
						bestMove = playerMove.Clone()
					}

					playerMove.CP *= povMultiplier
					playerMove.Mate *= povMultiplier

					bestMove.CP *= povMultiplier
					bestMove.Mate *= povMultiplier

					newMove.Eval = playerMove
					newMove.BestMove = bestMove
				} else {
					bestMove.CP *= povMultiplier
					bestMove.Mate *= povMultiplier

					newMove.Eval = bestMove
				}

				// we found the best move

				for j := 0; j < len(newMove.OtherEvals); j++ {
					newMove.OtherEvals[j].CP *= povMultiplier
					newMove.OtherEvals[j].Mate *= povMultiplier
				}

				movesEval = append(movesEval, newMove)
				playerMove := newMove.Eval

				bestMoveSAN := board.UCItoSAN(bestMove.UCIMove)
				logInfo(fmt.Sprintf("%3d/%3d %3d. %-7s played_cp: %6d played_mate: %2d top_move: %-7s top_cp: %6d top_mate: %2d",
					i+1, len(moves), (i+2)/2, sanMove, playerMove.CP, playerMove.Mate, bestMoveSAN, bestMove.CP, bestMove.Mate))

				break loop

			case <-ctx.Done():
				return nil
			}
		}
	}

	pgn := evalToPGN(startPosFEN, depth, movesEval, true)
	logMultiline(pgn)

	tbl := debugEvalTable(startPosFEN, movesEval)
	logMultiline(tbl)

	bookMoves := createOpeningBook(startPosFEN, movesEval)
	/*bookIndent, err := json.MarshalIndent(bookMoves, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n\n", string(bookIndent))*/

	if err := saveBookMoves(bookMoves); err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
		logMultiline(pgn)
		log.Fatal(err)
	}

	a.input <- "quit"

	cancel()
	wg.Wait()

	if err := ReadBook(); err != nil {
		return err
	}

	return nil
}

func (a *Analyzer) AnalyzePosition(ctx context.Context, fenPos string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg, err := a.startStockfish(ctx)
	if err != nil {
		return err
	}

	var sentNewGame bool
	var playerNumber int

	_ = playerNumber

	board := fen.FENtoBoard(fenPos)
	if board.ActiveColor == "w" {
		playerNumber = 0
	} else {
		playerNumber = 1
	}

readyOKLoop:
	for line := range a.output {
		switch line {
		case "uciok":
			a.input <- fmt.Sprintf("setoption name Threads value %d", threads)
			a.input <- fmt.Sprintf("setoption name Hash value %d", hashMemory)
			a.input <- fmt.Sprintf("setoption name MultiPV value %d", multiPV)
			a.input <- fmt.Sprintf("setoption name SyzygyPath value %s", SyzygyPath)
			a.input <- fmt.Sprintf("setoption name UCI_AnalyseMode value true")
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

	a.input <- fmt.Sprintf("position fen %s", fenPos)

	var searchMoves string

	for i := 0; i < 3; i++ {
		var (
			depth      int
			maxTime    time.Duration
			maxSurvive int
		)

		switch i {
		case 0:
			depth = 30
			maxTime = 10 * time.Minute
			maxSurvive = 5
		case 1:
			depth = 40
			maxTime = 10 * time.Minute
			maxSurvive = 4
		case 2:
			depth = 60
			maxTime = 20 * time.Minute
			maxSurvive = 3
		}

		logInfo(fmt.Sprintf("start depth=%d searchmoves %s fen %s", depth, searchMoves, fenPos))

		evals, err := a.analyzePosition(ctx, fenPos, depth, searchMoves, maxTime)
		if err != nil {
			return fmt.Errorf("searchmoves '%s': %v", searchMoves, err)
		}

		searchMoves = ""

		if len(evals) > maxSurvive {
			evals = evals[:maxSurvive]
		}
		for _, eval := range evals {
			if searchMoves != "" {
				searchMoves += " "
			}
			searchMoves += eval.UCIMove
		}

		logInfo(fmt.Sprintf("new searchMoves: '%s'", searchMoves))
	}

	logInfo("sending quit")
	a.input <- "quit"

	cancel()
	wg.Wait()

	return nil
}

func (a *Analyzer) waitReady() {
	a.input <- "isready"
	for line := range a.output {
		if line == "readyok" {
			break
		}
	}
}

var timeAround = 0

func (a *Analyzer) analyzePosition(ctx context.Context, fenPos string, depth int, searchMoves string, maxTime time.Duration) ([]Eval, error) {
	board := fen.FENtoBoard(fenPos)
	timeAround++

	if board.IsMate() {
		return nil, fmt.Errorf("TODO: position '%s' is already game over", fenPos)
	}

	var (
		playerNumber  int
		povMultiplier int
	)
	if board.ActiveColor == "w" {
		playerNumber = 0
		povMultiplier = 1
	} else if board.ActiveColor == "b" {
		playerNumber = 1
		povMultiplier = -1
	} else {
		return nil, fmt.Errorf("active color '%s'; expected w or b", board.ActiveColor)
	}

	if searchMoves == "" {
		fmt.Printf("HAM path")
		a.input <- fmt.Sprintf("setoption name MultiPV value %d", multiPV)
		a.input <- fmt.Sprintf("go depth %d", depth)
		if timeAround > 1 {
			log.Fatal("figure it out")
		}
		timeAround++
	} else {
		fmt.Printf("SPAM path")
		a.input <- fmt.Sprintf("setoption name MultiPV value %d", len(strings.Split(searchMoves, " ")))
		a.input <- fmt.Sprintf("go depth %d searchmoves %s", depth, searchMoves)
	}

	evals := a.getLines(ctx, 0, 1, maxTime, true)
	if len(evals) == 0 {
		return nil, fmt.Errorf("no evaluations returned for fen '%s'", fenPos)
	}

	for j := 0; j < len(evals); j++ {
		evals[j].CP *= povMultiplier
		evals[j].Mate *= povMultiplier
	}

	curDepth := 0
	logInfo("")
	newestEvals := evalsWithHighestDepth(evals)
	var bestMoveAtDepth Eval
	for _, eval := range evals {
		san := board.UCItoSAN(eval.UCIMove)
		if eval.Depth > curDepth {
			curDepth = eval.Depth
			newestEvals = nil
			bestMoveAtDepth = eval.Clone()
		} else if eval.Depth != curDepth {
			continue
		}
		diff := povDiff(playerNumber, eval, bestMoveAtDepth)

		var mark string
		if diff < cutoff {
			mark = "XXX"
		} else {
			newestEvals = append(newestEvals, eval.Clone())
		}

		logInfo(fmt.Sprintf("depth: %d move: %-7s %s cp: %4d wc-diff: %6.2f %s", eval.Depth, san, eval.UCIMove, eval.CP, diff, mark))
	}
	logInfo("")

	bestMove := bestEval(newestEvals).Clone()

	for _, eval := range newestEvals {
		san := board.UCItoSAN(eval.UCIMove)
		diff := povDiff(playerNumber, eval, bestMove)
		logInfo(fmt.Sprintf("*** depth: %d move: %-7s %s cp: %4d wc-diff: %0.2f", eval.Depth, san, eval.UCIMove, eval.CP, diff))
	}

	logInfo(fmt.Sprintf("%3d/%3d %3d. top_move: %-7s top_cp: %6d top_mate: %2d",
		1, 1, 1, board.UCItoSAN(bestMove.UCIMove), bestMove.CP, bestMove.Mate))

	return newestEvals, nil
}

type BookMove struct {
	FEN       string              `json:"fen"`
	UCI       string              `json:"uci"`
	SAN       string              `json:"san"`
	Depth     int                 `json:"d"`
	CP        int                 `json:"cp"`
	DateTime  int64               `json:"dt"`
	Mate      int                 `json:"m,omitempty"`
	Mated     bool                `json:"cm,omitempty"`
	Stalemate bool                `json:"sm,omitempty"`
	Details   BookMoveEvalDetails `json:"x"`
}

func (bm BookMove) Equals(a BookMove) bool {
	return bm.FEN == a.FEN &&
		bm.UCI == a.UCI &&
		bm.Depth == a.Depth &&
		bm.CP == a.CP &&
		bm.Mate == a.Mate &&
		bm.Mated == a.Mated &&
		bm.Stalemate == a.Stalemate &&
		bm.Details.ID == a.Details.ID &&
		bm.Details.PV == a.Details.PV
}

type BookMoveEvalDetails struct {
	ID int `json:"id"`

	SelDepth int    `json:"sd"`
	Nodes    int    `json:"n"`
	TBHits   int    `json:"tb,omitempty"`
	Time     int    `json:"t"`
	PV       string `json:"pv,omitempty"`
}

func saveBookMoves(book []BookMove) error {
	var existingBook []BookMove

	b, err := ioutil.ReadFile("book.json")
	if err == nil {
		//fmt.Printf("book.json %d bytes\n", len(b))
		if err := json.Unmarshal(b, &existingBook); err != nil {
			return err
		}
		//fmt.Printf("book.json existing length %d\n", len(existingBook))
	}

	c := make([]BookMove, len(existingBook))
	if len(existingBook) > 0 {
		copy(c, existingBook)
	}

	//fmt.Printf("len(c)=%d len(existingBook)=%d len(book)=%d\n", len(c), len(existingBook), len(book))

	// combine and filter dupes
	for _, move := range book {
		found := false
		for _, existingMove := range existingBook {
			if move.FEN == existingMove.FEN && move.UCI == existingMove.UCI && move.Depth == existingMove.Depth && move.CP == existingMove.CP && move.Mate == existingMove.Mate && move.Mated == existingMove.Mated && move.Stalemate == existingMove.Stalemate {
				//fmt.Printf("de-dupe: found fen %s move %s (%s) depth %d cp %d mate %d\n", move.FEN, move.UCI, move.SAN, move.Depth, move.CP, move.Mate)
				found = true
				break
			}
		}
		if found {
			continue
		}
		c = append(c, move)
	}

	//fmt.Printf("len(c)=%d len(existingBook)=%d len(book)=%d\n", len(c), len(existingBook), len(book))

	bookJSON, err := json.Marshal(c)
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("book.json", bookJSON, 0644); err != nil {
		return err
	}

	return nil
}

func createOpeningBook(startFEN string, movesEval Moves) []BookMove {
	sort.Sort(movesEval)

	board := fen.FENtoBoard(startFEN)
	dt := time.Now().UnixMilli()

	var book []BookMove
	for _, move := range movesEval {
		evals := []Eval{move.BestMove}
		if move.Eval.UCIMove != move.BestMove.UCIMove {
			evals = append(evals, move.Eval)
		}
		if len(move.OtherEvals) > 0 {
			evals = append(evals, move.OtherEvals...)
		}

		for _, e := range evals {
			bookMove := BookMove{
				FEN:       board.FENNoMoveClocks(),
				UCI:       e.UCIMove,
				SAN:       board.UCItoSAN(e.UCIMove),
				Depth:     e.Depth,
				CP:        e.CP,
				DateTime:  dt, // TODO: store at time found
				Mate:      e.Mate,
				Mated:     e.Mated,
				Stalemate: false, // TODO
				Details: BookMoveEvalDetails{
					ID:       Engine_Stockfish_15_NN_6e0680e, // TODO: make an external reference to save space
					SelDepth: e.SelDepth,
					Nodes:    e.Nodes,
					TBHits:   e.TBHits,
					Time:     e.Time,
					PV:       strings.Join(e.PV, " "),
				},
			}

			book = append(book, bookMove)
		}

		board.Moves(move.UCI)
	}

	sort.Sort(MovesReverse(movesEval))

	return book
}

func debugEvalTable(startFEN string, movesEval Moves) string {
	sort.Sort(movesEval)

	var sb strings.Builder
	board := fen.FENtoBoard(startFEN)

	firstMove := movesEval[0]
	firstMoveNumber := (firstMove.Ply / 2) + 1
	sb.WriteString(fmt.Sprintf("%3d.", firstMoveNumber))
	firstPlayer := firstMove.Ply % 2
	if firstPlayer == 1 {
		sb.WriteString(fmt.Sprintf("%-7s%-2s %7s", "", "", ""))
		sb.WriteString(fmt.Sprintf("      %-7s%-2s %7s", "", "", ""))
	}

	for _, move := range movesEval {
		playerNumber := move.Ply % 2

		moveNumber := (move.Ply / 2) + 1
		if playerNumber == 0 {
			if moveNumber != firstMoveNumber {
				sb.WriteString(fmt.Sprintf("%3d.", moveNumber))
			}
		} else {
			sb.WriteString(" | ")
		}

		e1 := move.BestMove
		e2 := move.Eval

		var annotation string
		if !move.IsMate {
			diff := povDiff(playerNumber, e2, e1)
			if diff <= -0.3 {
				annotation = "??" // $4
			} else if diff <= -0.2 {
				annotation = "?" // $2
			} else if diff <= -0.1 {
				annotation = "?!" // $6
			}
		}

		sb.WriteString(fmt.Sprintf("%-7s%-2s %7s", move.SAN, annotation, move.Eval.String()))

		if move.UCI != move.BestMove.UCIMove {
			bestMoveSAN := board.UCItoSAN(move.BestMove.UCIMove)
			sb.WriteString(fmt.Sprintf(" top: %-7s%-2s %7s", bestMoveSAN, "", move.BestMove.String()))
		} else {
			sb.WriteString(fmt.Sprintf("      %-7s%-2s %7s", "", "", ""))
		}

		board.Moves(move.UCI)

		if playerNumber == 1 {
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
	sb.WriteString(fmt.Sprintf("[Depth \"%d\"]\n", depth))
	sb.WriteString("\n")

	board := fen.FENtoBoard(startFEN)
	prevEval := "0.24"
	for _, move := range movesEval {
		playerNumber := move.Ply % 2

		moveNumber := (move.Ply / 2) + 1
		if playerNumber == 0 {
			sb.WriteString(fmt.Sprintf("%d. ", moveNumber))
		} else {
			sb.WriteString(fmt.Sprintf("%d. ... ", moveNumber))
		}

		e1 := move.BestMove
		e2 := move.Eval

		// $1 = !  (good move)
		// $2 = ?  (poor move, mistake)
		// $4 = ?? (very poor move or blunder)
		// $6 = ?! (questionable or dubious move, inaccuracy)
		var annotation, annotationWord string
		var showVariations bool
		if !move.IsMate {
			diff := povDiff(playerNumber, e2, e1)
			if diff <= -0.3 {
				annotation = "??" // $4
				annotationWord = "Blunder"
				if e1.Mate != 0 && e2.Mate == 0 {
					annotationWord = "Lost forced checkmate sequence"
				} else if e1.Mate == 0 && e2.Mate != 0 {
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

			curEval := move.Eval.String()
			if strings.HasPrefix(curEval, "#") {
				mate := strings.TrimLeft(curEval, "#-")
				curEval = "Mate in " + mate
			}

			sb.WriteString(fmt.Sprintf("    { (%s → %s) %s. %s was best. }\n", prevEval, curEval, annotationWord, bestMoveSAN))
		}
		sb.WriteString(fmt.Sprintf("    { [%%eval %s] }\n", move.Eval.String()))

		if showVariations {
			//fmt.Printf("board.FullMove: %s\n", board.FullMove)
			writeVariation(&sb, board.Clone(), e1, "")
			writeVariation(&sb, board.Clone(), e2, annotation)
		}
		board.Moves(move.UCI)

		prevEval = move.Eval.String()
	}
	sb.WriteString("*\n") // TODO: lazy, make this 1-0, 0-1, 1/2-1/2, or *

	sort.Sort(MovesReverse(movesEval))

	return sb.String()
}

func writeVariation(sb *strings.Builder, board *fen.Board, eval Eval, annotation string) {
	sb.WriteString("    ( ")

	used := 6

	basePly := (atoi(board.FullMove) - 1) * 2
	if board.ActiveColor == "b" {
		basePly++
	}

	//fmt.Printf("variation base_ply: %d\n", basePly)

	for j := 0; j < len(eval.PV); j++ {
		uci := eval.PV[j]
		san := board.UCItoSAN(uci)

		ply := basePly + j
		moveNumber := (ply + 2) / 2

		//fmt.Printf("variation ply: %d\n", ply)
		//fmt.Printf("variation fullmove: %d\n", moveNumber)

		if j == 0 {
			sb.WriteString(fmt.Sprintf("%d. ", moveNumber))
			used += 5
			if ply%2 == 1 {
				sb.WriteString("... ")
				used += 4
			}
		} else if ply%2 == 0 {
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
			variationEval := fmt.Sprintf("{ [%%eval %s] } ", eval.String())
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

func (a *Analyzer) startStockfish(ctx context.Context) (*sync.WaitGroup, error) {
	binary := "/home/jud/projects/trollfish/stockfish/stockfish"
	dir := "/home/jud/projects/trollfish/stockfish"

	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = dir

	var wg sync.WaitGroup

	var readyok int64 = 1

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
				for atomic.LoadInt64(&readyok) == 0 {
					time.Sleep(10 * time.Millisecond)
				}

				a.LogEngine(line)

				if line == "isready" {
					logInfo("-> isready")
					atomic.StoreInt64(&readyok, 0)
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
				atomic.StoreInt64(&readyok, 1)
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
	_, _ = fmt.Fprintf(os.Stderr, "# %s %s\n", ts(), strings.TrimRight(msg, "\n"))
}

func logMultiline(s string) {
	parts := strings.Split(s, "\n")
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(fmt.Sprintf("# %s\n", part))
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

func ReadBook() error {
	var book []BookMove

	bookJSON, err := ioutil.ReadFile("book.json")
	if err != nil {
		return err
	}

	//fmt.Printf("book.json %d bytes\n", len(bookJSON))
	if err := json.Unmarshal(bookJSON, &book); err != nil {
		return err
	}
	//fmt.Printf("book.json existing length %d\n", len(book))

	for i := 0; i < len(book); i++ {
		move := book[i]
		if move.Depth < 18 {
			book = append(book[:i], book[i+1:]...)
			i--
			continue
		}
	}

	// de-dupe; TODO: sorting would make this more efficient
	for i := 0; i < len(book)-1; i++ {
		move1 := book[i]
		for j := i + 1; j < len(book); j++ {
			move2 := book[j]
			if move1.Equals(move2) {
				book = append(book[:j], book[j+1:]...)
				j--
				//fmt.Printf("de-dupe:\n- %#v equals\n- %#v\n", move1, move2)
				continue
			}
		}
	}

	//fmt.Printf("new length after de-dupe %d\n", len(book))

	sort.Slice(book, func(i, j int) bool {
		a := book[i]
		b := book[j]

		if a.FEN != b.FEN {
			return a.FEN < b.FEN
		}

		if a.UCI != b.UCI {
			return a.UCI < b.UCI
		}

		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}

		if a.Details.ID != b.Details.ID {
			return a.Details.ID < b.Details.ID
		}

		if a.CP != b.CP {
			return a.CP < b.CP
		}

		if a.Mate != b.Mate {
			return a.Mate < b.Mate
		}

		if len(a.Details.PV) != len(b.Details.PV) {
			return len(a.Details.PV) < len(b.Details.PV)
		}

		if a.Details.PV != b.Details.PV {
			return a.Details.PV < b.Details.PV
		}

		return a.DateTime < b.DateTime
	})

	bookJSON, err = json.Marshal(book)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile("book.json", bookJSON, 0644); err != nil {
		return err
	}

	bookFormattedJSON, err := json.MarshalIndent(book, "", "  ")
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile("book-formatted.json", bookFormattedJSON, 0644); err != nil {
		return err
	}

	///

	var fens []string
bookLoop:
	for i := 0; i < len(book); i++ {
		move := book[i]
		if move.FEN == startPosFEN {
			continue
		}

		for j := 0; j < len(fens); j++ {
			if fens[j] == move.FEN {
				continue bookLoop
			}
		}

		fens = append(fens, move.FEN)
	}

	/*_, err = getBlessedMove("rnbqkb1r/pp3ppp/2p1pn2/3p2B1/2PP4/2N2N2/PP2PPPP/R2QKB1R b KQkq -", book)
	if err != nil {
		return err
	}*/

	var blessedBook []BlessedMove

	for _, posFEN := range fens {
		blessedMove, err := getBlessedMove(posFEN, book)
		if err != nil {
			return err
		}
		blessedBook = append(blessedBook, blessedMove)
	}

	blessedJSON, err := json.MarshalIndent(blessedBook, "", "  ")
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile("blessed-book.json", blessedJSON, 0644); err != nil {
		return err
	}

	return nil
}

type BlessedMove struct {
	FEN  string `json:"fen"`
	UCI  string `json:"uci"`
	SAN  string `json:"san"`
	CP   int    `json:"cp"`
	Mate int    `json:"mate,omitempty"`
}

func getBlessedMove(posFEN string, book []BookMove) (BlessedMove, error) {
	type depthRange struct {
		minDepth int
		maxDepth int
	}

	white := strings.Contains(posFEN, " w ")
	var povMultiplier int
	if white {
		povMultiplier = 1
	} else {
		povMultiplier = -1
	}

	//fmt.Printf("FEN: %s\n", posFEN)

	uq := make(map[string][]BookMove)
	for i := 0; i < len(book); i++ {
		move := book[i]

		if move.FEN != posFEN {
			continue
		}
		if move.Depth < 18 {
			continue
		}

		move.CP *= povMultiplier
		move.Mate *= povMultiplier

		//fmt.Printf("d=%2d move=%-7s uci=%-4s cp=%3d mate=%3d\n", move.Depth, move.SAN, move.UCI, move.CP, move.Mate)

		uq[move.UCI] = append(uq[move.UCI], move)
	}

	ranges := []depthRange{
		{minDepth: depthTier1, maxDepth: depthTier1 - 1},
		{minDepth: depthTier2, maxDepth: depthTier3 - 1},
		{minDepth: depthTier3, maxDepth: 100},
	}

	ensemble := make(map[string]int)
	ensembleCount := make(map[string]int)

	for _, depths := range ranges {
		minDepth, maxDepth := depths.minDepth, depths.maxDepth
		//fmt.Printf("depth = [%d, %d]\n", minDepth, maxDepth)
		for uci, v := range uq {
			//board := fen.FENtoBoard(posFEN)
			//san := board.UCItoSAN(uci)

			var num, denom, samples int
			for depth := minDepth; depth <= maxDepth; depth++ {
				var depthSum int
				var depthCount int
				for _, eval := range v {
					if eval.Depth != depth {
						continue
					}
					//fmt.Printf("depth: %2d san: %5s cp: %3d\n", eval.Depth, san, eval.CP)
					depthSum += eval.CP
					depthCount++
					samples++
				}
				if depthCount != 0 {
					depthAverage := depthSum / depthCount
					num += depthAverage * depth
					denom += depth
				}
			}

			if samples != 0 {
				cp := num / denom
				ensemble[uci] += cp
				ensembleCount[uci] += 1

				//fmt.Printf("depth: [%d, %d] uci: %s san: %5s cp: %3d samples: %d\n", minDepth, maxDepth, uci, san, cp, samples)
			}
		}
	}

	var moves []BookMove
	for uci, count := range ensembleCount {
		ensemble[uci] /= count
		cp := ensemble[uci]
		moves = append(moves, BookMove{UCI: uci, CP: cp})
	}

	sort.Slice(moves, func(i, j int) bool {
		return moves[i].CP > moves[j].CP
	})

	bestMove := moves[0]
	board := fen.FENtoBoard(posFEN)

	var sb strings.Builder
	for _, move := range moves {
		sb.WriteString(fmt.Sprintf("%7s %s: %0.2f\n", board.UCItoSAN(move.UCI), move.UCI, float64(move.CP)/100))
	}
	logMultiline(sb.String())

	san := board.UCItoSAN(bestMove.UCI)

	blessed := BlessedMove{FEN: posFEN, UCI: bestMove.UCI, SAN: san, CP: bestMove.CP * povMultiplier, Mate: bestMove.Mate * povMultiplier}
	blessedJSON, err := json.Marshal(blessed)
	if err != nil {
		return BlessedMove{}, err
	}

	logMultiline(string(blessedJSON))

	return blessed, nil
}

func (a *Analyzer) LogEngine(s string) {
	a.logEngineMtx.Lock()
	_, _ = fmt.Fprintln(os.Stdout, s)
	a.logEngineMtx.Unlock()
}

func (a *Analyzer) howMichael(evals []Eval) []string {
	var s []string
	for _, eval := range evals {
		s = append(s, eval.UCIMove)
	}
	return s
}
