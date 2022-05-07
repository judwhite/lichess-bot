package analyze

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trollfish-lichess/fen"
)

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
				log.Fatal(fmt.Sprintf("SF CMD ERR: %v", err))
			}
		}
	}()

	// initialize parameters

	var sentNewGame bool

readyOKLoop:
	for line := range a.output {
		switch line {
		case "uciok":
			if useFullResources {
				a.input <- fmt.Sprintf("setoption name Threads value %d", threads)
				a.input <- fmt.Sprintf("setoption name Hash value %d", hashMemory)
				a.input <- fmt.Sprintf("setoption name SyzygyPath value %s", SyzygyPath)
			}
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

	return &wg, nil
}

func (a *Analyzer) LogEngine(s string) {
	if !a.logEngineOutput {
		return
	}

	a.logEngineMtx.Lock()
	_, _ = fmt.Fprintln(os.Stderr, s)
	a.logEngineMtx.Unlock()
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

func (a *Analyzer) waitReady() {
	a.input <- "isready"
	for line := range a.output {
		if line == "readyok" {
			break
		}
	}
}

func logInfo(msg string) {
	_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", ts(), strings.TrimRight(msg, "\n"))
}

func logMultiline(s string) {
	parts := strings.Split(s, "\n")
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(fmt.Sprintf("%s\n", part))
	}
	_, _ = fmt.Fprint(os.Stdout, sb.String())
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

func plyToColor(ply int) fen.Color {
	if ply%2 == 0 {
		return fen.WhitePieces
	}
	return fen.BlackPieces
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
