package analyze

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"trollfish-lichess/fen"
)

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 24
const threadsHashMultiplier = 2048

// TODO: get path from config file
const syzygyPath = "/home/jud/projects/tablebase/3-4-5"

type Move struct {
	UCI             string
	SAN             string
	NextMoveTopEval Eval
	IsMate          bool
}

type Eval struct {
	UCIMove    string
	Depth      int
	SelDepth   int
	MultiPV    int
	CP         int
	Mate       int
	Nodes      int
	NPS        int
	TBHits     int
	Time       int
	UpperBound bool
	LowerBound bool
	PV         []string
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

/*func (e Eval) Score(color int) string {
	if e.MatePOV != 0 {
		return fmt.Sprintf("#%d", e.MatePOV)
	}

	return fmt.Sprintf("%0.1f", float64(e.CP)/100)
}*/

func QuickAnalysis(ctx context.Context, moves []string, depth int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startStockfish(ctx, input, output); err != nil {
		return err
	}

	input <- "uci"

	for line := range output {
		if line == "uciok" {
			input <- fmt.Sprintf("setoption name Threads value %d", threads)
			input <- fmt.Sprintf("setoption name Hash value %d", threads*threadsHashMultiplier)
			input <- fmt.Sprintf("setoption name MultiPV value 1")
			input <- fmt.Sprintf("setoption name SyzygyPath value %s", syzygyPath)
			input <- "ucinewgame"
			break
		}
	}

	sf := make(chan Eval)

	go func() {
		for line := range output {
			if strings.HasPrefix(line, "bestmove ") {
				sf <- Eval{}
			} else if strings.HasPrefix(line, "info ") && strings.Contains(line, "score") {
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

				sf <- eval
			}
		}
	}()

	var movesEval []Move

	for i := 0; i < len(moves); i++ {
		playerMove := moves[i]
		var povMultiplier int
		if i%2 == 0 {
			povMultiplier = -1
		} else {
			povMultiplier = 1
		}

		playedMoves := moves[0 : i+1] // include this move, eval what comes after

		m := strings.Join(playedMoves, " ")
		input <- fmt.Sprintf("position fen %s startpos moves %s", startPosFEN, m)
		input <- fmt.Sprintf("go depth %d", depth)

		board := fen.FENtoBoard(startPosFEN)
		board.Moves(playedMoves[0 : len(playedMoves)-1]...)
		sanMove := board.UCItoSAN(playerMove)
		board.Moves(playerMove)

		var topEval Eval

	loop:
		for {
			select {
			case move := <-sf:
				if move.UCIMove == "" {
					topEval.CP *= povMultiplier
					topEval.Mate *= povMultiplier

					movesEval = append(movesEval, Move{
						UCI:             playerMove,
						SAN:             sanMove,
						NextMoveTopEval: topEval,
						IsMate:          board.IsMate(),
					})

					logInfo(fmt.Sprintf("%d/%d\n", i+1, len(moves)))

					break loop
				}
				topEval = move

			case <-ctx.Done():
				return nil
			}
		}
	}

	printEval(movesEval)
	return nil
}

func printEval(movesEval []Move) {
	for i, move := range movesEval {
		playerNumber := i % 2

		if playerNumber == 0 {
			moveNumber := (i + 2) / 2
			fmt.Printf("%3d. ", moveNumber)
		} else {
			fmt.Printf(" | ")
		}

		var e1 Eval
		if i == 0 {
			e1 = Eval{CP: 15}
		} else {
			e1 = movesEval[i-1].NextMoveTopEval
		}
		e2 := move.NextMoveTopEval

		eval := func(e Eval) string {
			if move.IsMate {
				return ""
			}

			if e.Mate != 0 {
				return fmt.Sprintf("#%d", e.Mate)
			}

			var sb strings.Builder
			if e.CP > 0 {
				sb.WriteRune('+')
			}
			sb.WriteString(fmt.Sprintf("%.1f", float64(e.CP)/100))
			s := sb.String()

			if s == "+0.0" || s == "-0.0" {
				return "0.0"
			}

			return s
		}

		// $1 = !  (good move)
		// $2 = ?  (poor move, mistake)
		// $4 = ?? (very poor move or blunder)
		// $6 = ?! (questionable or dubious move, inaccuracy)
		var annotation string
		if !move.IsMate {
			diff := povDiff(playerNumber, e2, e1)
			if diff <= -0.3 {
				annotation = " ??" // $4
			} else if diff <= -0.2 {
				annotation = " ?" // $2
			} else if diff <= -0.1 {
				annotation = " ?!" // $6
			}
		}

		fmt.Printf("%-9s %6s", move.SAN+annotation, eval(move.NextMoveTopEval))
		if playerNumber == 1 {
			fmt.Println()
		}
	}

	if len(movesEval)%2 == 1 {
		fmt.Println()
	}
}

func startStockfish(ctx context.Context, input <-chan string, output chan<- string) error {
	binary := "/home/jud/projects/trollfish/stockfish/stockfish"
	dir := "/home/jud/projects/trollfish/stockfish"

	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		for {
			select {
			case line := <-input:
				writeLog := true
				if line == "uci" || line == "ucinewgame" {
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

				if writeLog {
					logInfo(fmt.Sprintf("-> %s", line))
				}
				_, err := stdin.Write([]byte(fmt.Sprintf("%s\n", line)))
				if err != nil {
					log.Fatalf("stdin.Write ERR: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// stderr loop
	go func() {
		r := bufio.NewScanner(stderr)
		for r.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := r.Text()
				log.Printf(fmt.Sprintf("SF STDERR: %s\n", line))
			}
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("SF ERR: stderr: %v\n", err))
		}
	}()

	// stdout loop
	go func() {
		r := bufio.NewScanner(stdout)
		for r.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := r.Text()
			output <- line
		}
		if err := r.Err(); err != nil {
			log.Printf(fmt.Sprintf("ERR: stdout: %v\n", err))
		}
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Fatal(fmt.Sprintf("ERR: %v\n", err))
		}
	}()

	return nil
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

func intToColor(n int) string {
	switch n {
	case 0:
		return "white"
	case 1:
		return "black"
	default:
		log.Fatalf("invalid player number %d", n)
		return "unknown"
	}
}
