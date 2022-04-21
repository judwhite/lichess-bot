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

const depth = 40

var maxTimePerPly = 2 * time.Minute

func main() {
	moves := strings.Split("e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1g5 e7e6 f1d3 f8e7 g5f6 e7f6 d4f3 e8g8 d1d2 d8a5 e1g1 a5b6 a2a4 b8d7 a1e1 h7h6 c3a2 f8e8 b2b4 a6a5 d3b5 f6e7 e4e5 a5b4 a2b4 b6c7 b4d3 e7f8 e5d6 f8d6 b5d7 c7d7 d3e5 d7d8 e1a1 a8a6 a1a3 d6c7 d2d8 c7d8 a3c3 d8f6 c3c4 a6a5 e5g4 f6e7 f3d4 e8d8 d4b3 a5a8 a4a5 e7f8 g4e3 c8d7 f1d1 d7b5 d1d8 a8d8 c4d4 d8c8 h2h4 f8e7 g2g3 c8c3 g1g2 g7g5 h4g5 e7g5 g2f3 b5c6 f3g4 g5e3 f2e3 c3e3 g4h5 g8g7 d4g4 g7h7 b3d4 c6e4 d4b5 e4f3 b5d6 e3e1", " ")
	err := analyze.QuickAnalysis(context.Background(), moves, 20)
	if err != nil {
	a := analyze.New()

	// first pass, very quick
	if err := a.AnalyzeGame(context.Background(), moves, 18, 8*time.Second); err != nil {
		log.Fatal(err)
	}

	if err := a.AnalyzeGame(context.Background(), moves, depth, maxTimePerPly); err != nil {
		log.Fatal(err)
	}
}

func main2() {
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
