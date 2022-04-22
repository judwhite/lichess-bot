package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"
	"trollfish-lichess/analyze"
)

var maxTimePerPly = 10 * time.Minute

func main() {
	analysis()
	//runLichessBot()
}

func analysis() {
	/*if err := analyze.ReadBook(); err != nil {
		log.Fatal(err)
	}*/

	a := analyze.New()

	if err := a.AnalyzePosition(context.Background(), "r1bqkb1r/1p1n1ppp/p1n1p3/2ppP3/3P1P2/2N1BN2/PPP1B1PP/R2QK2R b KQkq -", 60, maxTimePerPly); err != nil {
		log.Fatal(err)
	}

	/*moves := strings.Split("e2e4 c7c5 g1f3 b8c6 f1b5 e7e6 e1g1 g8e7 c2c3 a7a6 b5a4 d7d5 e4d5 e7d5 a4c6 b7c6 c3c4 d5e7 b1c3 f7f6 c3a4 e7g6 d2d3 e6e5 c1e3 c8f5 f3e1 g6f4 d1f3 g7g5 e3f4 g5f4 a4c5 e8f7 c5e4 h8g8 f3e2 f5g4 f2f3 g4e6 g1h1 a8b8 b2b3 c6c5 f1g1 f8e7 e1c2 g8g6 a1b1 d8c7 e4f2 e6d7 f2e4 d7c6 e4c3 c7d7 c3d5 b8g8 d5e7 f7e7 c2e1 g6h6 e2f2 e7f7 g1f1 g8g5 b1b2 g5h5 h1g1 h5h2 f2c5 h2h5 c5b6 f6f5 g1f2 d7e7 b3b4 e5e4 b6c5 e7f6 b2c2 h5h1 d3e4 f5e4 f1g1 f6h4 f2f1 h1g1 c5g1 e4e3 e1d3 h4g3 c2e2 f7f8 e2e3 f4e3 g1e3 h6h1 f1e2 g3g2 e3f2 c6f3 e2e3 h1h3 f2g2 f3g2 e3d4 g2f1 d3e5 f8e7 b4b5 a6b5 c4b5 f1b5 d4e4 h3h5 e4f4 h5e5 f4e5 e7f7 e5f5 h7h6 f5e4 f7f6 e4d5 h6h5 d5c5 b5e8 c5d4 f6f5 a2a4 e8a4 d4e3 f5g4 e3d3 h5h4 d3e3 h4h3 e3f2 a4b5 f2e1 h3h2 e1d1 h2h1Q d1c2 h1f3 c2b2 b5a4 b2b1 f3c3 b1a2 a4b3 a2a3 b3e6 a3a4 e6d7", " ")
	moves = moves[0:11]

	// first pass, very quick
	if err := a.AnalyzeGame(context.Background(), moves, 24, 30*time.Second); err != nil {
		log.Fatal(err)
	}

	depths := []int{50}
	for _, depth := range depths {
		if err := a.AnalyzeGame(context.Background(), moves, depth, maxTimePerPly); err != nil {
			log.Fatal(err)
		}
	}*/
}

func runLichessBot() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCtx, sigCancel := context.WithCancel(context.Background())
	defer sigCancel()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startTrollFish(ctx, input, output); err != nil {
		log.Fatal(err)
	}

	listener := New(sigCtx, input, output)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				sigCancel()
				signal.Stop(sig)

				if listener.Playing() {
					fmt.Printf("shutting down after this game\n")
				}
				return
			}
		}
	}()

	if err := listener.Events(); err != nil {
		log.Fatal(err)
	}
}

func startTrollFish(ctx context.Context, input <-chan string, output chan<- string) error {
	binary := "/home/jud/projects/trollfish/trollfish"
	dir := "/home/jud/projects/trollfish"

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
		log.Fatalf("cmd.Start: %v\n", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for {
			select {
			case line := <-input:
				//fmt.Printf("-> %s\n", line)
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
		defer wg.Done()
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
		defer wg.Done()
		r := bufio.NewScanner(stdout)
		for r.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := r.Text()
			if strings.HasPrefix(line, "info string") {
				fmt.Printf("%s <- %s\n", ts(), line)
			}
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

func ts() string {
	return fmt.Sprintf("[%s]", time.Now().Format("2006-01-02 15:04:05.000"))
}
