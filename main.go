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

var maxTimePerPly = 15 * time.Minute

func main() {
	analysis()
	//runLichessBot()
}

func analysis() {
	/*if err := analyze.ReadBook(); err != nil {
		log.Fatal(err)
	}*/

	a := analyze.New()

	// 24, 32, 40, 41, 42, 43, 44
	if err := a.AnalyzePosition(context.Background(), "r1bqkbnr/pp2pppp/2n5/8/8/2N5/PP1PPPPP/R1BQKBNR b KQkq - 1 4"); err != nil {
		log.Fatal(err)
	}
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
