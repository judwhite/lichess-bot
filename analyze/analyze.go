package analyze

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"trollfish-lichess/fen"
)

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
const threads = 24
const threadsHashMultiplier = 2048

type Move struct {
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

func (m Move) Score() string {
	if m.Mate != 0 {
		return fmt.Sprintf("M%d", m.Mate)
	}

	return fmt.Sprintf("%0.2f", float64(m.CP)/100)
}

func QuickAnalysis(ctx context.Context, moves []string, depth int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startStockfish(ctx, input, output); err != nil {
		return err
	}

	input <- "uci"
	input <- fmt.Sprintf("setoption name Threads value %d", threads)
	input <- fmt.Sprintf("setoption name Hash value %d", threads*threadsHashMultiplier)
	input <- fmt.Sprintf("setoption name MultiPV value 1")
	input <- "ucinewgame"

	sf := make(chan Move)

	go func() {
		for line := range output {
			if strings.HasPrefix(line, "bestmove ") {
				sf <- Move{}
			} else if strings.HasPrefix(line, "info ") && strings.Contains(line, "score") {
				parts := strings.Split(line, " ")
				var move Move

			scoreLoop:
				for i := 1; i < len(parts); i++ {
					p := parts[i]
					inc := 1
					switch p {
					case "depth":
						move.Depth = atoi(parts[i+1])
					case "seldepth":
						move.SelDepth = atoi(parts[i+1])
					case "multipv":
						move.MultiPV = atoi(parts[i+1])
					case "score":
						p2 := parts[i+1]
						switch p2 {
						case "cp":
							move.CP = atoi(parts[i+2])
							inc++
						case "mate":
							move.Mate = atoi(parts[i+2])
							inc++
						default:
							log.Fatalf("unhandled: 'info ... score %s'", p2)
						}
					case "upperbound":
						move.UpperBound = true
						inc = 0
					case "lowerbound":
						move.LowerBound = true
						inc = 0
					case "nodes":
						move.Nodes = atoi(parts[i+1])
					case "nps":
						move.NPS = atoi(parts[i+1])
					case "tbhits":
						move.TBHits = atoi(parts[i+1])
					case "time":
						move.Time = atoi(parts[i+1])
					case "hashfull":
						// ignore
					case "pv":
						pvMoves := parts[i+1:]
						move.PV = pvMoves
						move.UCIMove = pvMoves[0]
						break scoreLoop
					default:
						log.Fatalf("unhandled: 'info ... %s'", p)
					}

					i += inc
				}

				sf <- move
			}
		}
	}()

	for i := len(moves) - 1; i >= 0; i-- {
		playerNumber := i % 2

		m := strings.Join(moves[0:i], " ")
		input <- fmt.Sprintf("position fen %s startpos moves %s", startPosFEN, m)
		input <- fmt.Sprintf("go depth %d", depth)

		board := fen.FENtoBoard(startPosFEN)
		board.Moves(moves[0:i]...)

		playerMove := moves[i]
		moveNumber := (i + 2) / 2

		color := intToColor(playerNumber)
		fmt.Printf("*** move: %d %s: %s (%s) fen: \"%s\"\n", moveNumber, color, board.UCItoSAN(playerMove), playerMove, board.FEN())
		continue

		var topMove Move

	loop:
		for {
			select {
			case move := <-sf:
				if move.UCIMove == "" {
					fmt.Printf("*** move: %d %s: %s (%s) depth: %d score: %s\n", (i+2)/2, color, board.UCItoSAN(topMove.UCIMove), topMove.UCIMove, topMove.Depth, topMove.Score())
					break loop
				}
				if playerNumber == 1 {
					move.CP *= -1
					move.Mate *= -1
				}
				topMove = move

			case <-ctx.Done():
				return nil
			}
		}
	}

	return nil
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
