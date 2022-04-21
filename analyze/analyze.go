package analyze

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"trollfish-lichess/fen"
)

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 24
const threadsHashMultiplier = 2048

// TODO: get path from config file
const syzygyPath = "/home/jud/projects/tablebase/3-4-5"

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

func getLines(ctx context.Context, ply, totalPlies int, output <-chan string, input chan<- string, maxTimePerPly time.Duration) []Eval {
	start := time.Now()

	var moves []Eval

	var depth int
	var printEngineOutput bool
	var stopped bool

	timeout := time.NewTimer(maxTimePerPly)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case line := <-output:
			if strings.HasPrefix(line, "bestmove") {
				input <- "stop"
				break loop
			}

			if !printEngineOutput && time.Since(start) > 30*time.Second {
				printEngineOutput = true
			}
			if printEngineOutput {
				logInfo(fmt.Sprintf("ply=%d/%d t=%v/%v <- %s", ply+1, totalPlies, time.Since(start).Round(time.Second), maxTimePerPly, line))
			}

			if strings.HasPrefix(line, "info ") && strings.Contains(line, "score") {
				eval := parseEval(line)
				eval.Raw = line

				if eval.UpperBound || eval.LowerBound {
					continue
				}

				if eval.Depth > depth {
					depth = eval.Depth
				}

				// remove evals with less nodes searched
				for i := 0; i < len(moves); i++ {
					if moves[i].Depth == eval.Depth && moves[i].MultiPV == eval.MultiPV {
						if moves[i].Nodes < eval.Nodes {
							moves = append(moves[:i], moves[i+1:]...)
							i--
							continue
						}
					}
				}

				moves = append(moves, eval)
			}

		case <-timeout.C:
			if depth == 0 {
				return nil
			}
			logInfo(fmt.Sprintf("per-move timeout expired (%v), using what we have at depth %d", maxTimePerPly, depth))
			input <- "stop"
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
		if moves[i].Depth == depth {
			depth1Count++
		}
		if moves[i].Depth == depth-1 {
			depth2Count++
		}
	}

	if depth1Count < depth2Count {
		logInfo(fmt.Sprintf("depth: %d depth1Count (%d) < depth2Count (%d), using depth: %d", depth, depth1Count, depth2Count, depth-1))
		depth--
	}

	for i := 0; i < len(moves); i++ {
		if moves[i].Depth != depth {
			moves = append(moves[:i], moves[i+1:]...)
			i--
			continue
		}
	}

	return moves
}

func New() *Analyzer {
	return &Analyzer{}
}

type Analyzer struct {
}

func (a *Analyzer) AnalyzeGame(ctx context.Context, moves []string, depth int, maxTimePerPly time.Duration) error {
	logInfo("start")

	input := make(chan string, 512)
	output := make(chan string, 512)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg, err := startStockfish(ctx, input, output)
	if err != nil {
		return err
	}

	input <- "uci"

readyOKLoop:
	for line := range output {
		switch line {
		case "uciok":
			logInfo("uciok")
			input <- fmt.Sprintf("setoption name Threads value %d", threads)
			input <- fmt.Sprintf("setoption name Hash value %d", threads*threadsHashMultiplier)
			input <- fmt.Sprintf("setoption name MultiPV value 1")
			input <- fmt.Sprintf("setoption name SyzygyPath value %s", syzygyPath)
			input <- fmt.Sprintf("setoption name UCI_AnalyseMode value true")
			input <- "isready"
		case "readyok":
			input <- "ucinewgame"
			break readyOKLoop
		}
	}

	sf := make(chan []Eval)

	var curPly int64

	go func() {
		totalPlies := len(moves)
		for {
			ply := int(atomic.LoadInt64(&curPly))
			evals := getLines(ctx, ply, totalPlies, output, input, maxTimePerPly)
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
			movesEval = append(movesEval, Move{
				Ply:    i,
				UCI:    playerMoveUCI,
				SAN:    sanMove,
				IsMate: true,
				Eval:   Eval{Mated: true},
			})
			continue
		}

		if len(movesEval) > 1 {
			pgn := evalToPGN(board.Clone().Moves(playerMoveUCI).FEN(), depth, movesEval, false)
			fmt.Println(pgn)
			sort.Sort(MovesReverse(movesEval))
		}

		m := strings.Join(beforeMoves, " ")
		input <- fmt.Sprintf("position fen %s moves %s", startPosFEN, m)
		input <- fmt.Sprintf("go depth %d", depth)

	loop:
		for {
			select {
			case evals := <-sf:
				if len(evals) > 1 {
					for _, eval := range evals {
						fmt.Printf("%s\n", eval.Raw)
					}
					log.Fatalf("len(evals) = %d, expected 1", len(evals))
				}
				if len(evals) == 0 {
					break loop
				}

				bestMove := evals[0]

				newMove := Move{
					Ply:      i,
					UCI:      playerMoveUCI,
					SAN:      sanMove,
					BestMove: bestMove,
					IsMate:   board.IsMate(),
				}

				// not the best move, eval player's move
				if bestMove.UCIMove != playerMoveUCI {
					input <- fmt.Sprintf("go depth %d searchmoves %s", depth, playerMoveUCI)

					evals := <-sf
					if len(evals) != 1 {
						for _, eval := range evals {
							fmt.Printf("%s\n", eval.Raw)
						}
						log.Fatalf("len(evals) = %d, expected 1", len(evals))
					}

					playerMove := evals[0]

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
						bestMove = playerMove
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

	fmt.Println(pgn)

	if err := ioutil.WriteFile("eval.pgn", []byte(pgn), 0644); err != nil {
		fmt.Println(pgn)
		log.Fatal(err)
	}

	input <- "quit"

	//logInfo("quit sent, calling wg.Wait")
	cancel()

	wg.Wait()
	//logInfo("wg.Wait returned")

	return nil
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

			showVariations = diff <= -0.05
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

func startStockfish(ctx context.Context, input <-chan string, output chan<- string) (*sync.WaitGroup, error) {
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
			case line := <-input:
				for atomic.LoadInt64(&readyok) == 0 {
					time.Sleep(1 * time.Millisecond)
				}

				writeLog := true
				if line == "uci" || line == "ucinewgame" || line == "stop" {
					writeLog = false
				}
				if strings.HasPrefix(line, "go depth") {
					writeLog = false
				}
				if strings.HasPrefix(line, "position fen") {
					writeLog = false
				}
				if strings.HasPrefix(line, "setoption name") {
					writeLog = false
				}

				if writeLog || atomic.LoadInt64(&readyok) == 0 {
					logInfo(fmt.Sprintf("-> %s", line))
				}

				if line == "isready" {
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
		for r.Scan() {
			select {
			case <-ctx.Done():
				logInfo("exiting stdout loop (ctx.Done())")
				return
			default:
			}

			line := r.Text()
			output <- line

			if line == "readyok" {
				logInfo("<- readyok")
				atomic.StoreInt64(&readyok, 1)
			} else if atomic.LoadInt64(&readyok) == 0 {
				logInfo(line)
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

func logInfo(msg string) {
	fmt.Printf("%s %s\n", ts(), msg)
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
