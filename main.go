package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"trollfish-lichess/epd"

	"trollfish-lichess/api"
	"trollfish-lichess/fen"
)

func main() {
	var botFlag bool
	var epdFilename string

	var flags flag.FlagSet
	flags.BoolVar(&botFlag, "bot", false, "runs the bot")
	flags.StringVar(&epdFilename, "epd", "", "run analysis and update an epd file")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			flags.PrintDefaults()
			os.Exit(1)
		}
		log.Fatal(err)
	}

	if botFlag {
		runLichessBot()
		return
	}

	if epdFilename != "" {
		opts := epd.AnalysisOptions{
			MinDepth:   32,
			MaxDepth:   64,
			MinTime:    10 * time.Second,
			MaxTime:    3 * time.Minute,
			DepthDelta: 5,
		}
		if err := epd.UpdateFile(context.TODO(), epdFilename, opts); err != nil {
			log.Fatal(err)
		}
		return
	}

	flags.PrintDefaults()
	os.Exit(1)

	//GetMostFrequentPGNPositions("strip.pgn")

	//getGames()

	//analysis()

	//positionLookup()
}

func GetMostFrequentPGNPositions(filename string) {
	db, err := fen.LoadPGNDatabase(filename)
	if err != nil {
		log.Fatal(err)
	}

	var moves int
	pos := make(map[string]int)

	for _, game := range db.Games {
		moves += len(game.Moves)
		for k := range game.Positions {
			pos[k] = pos[k] + 1
		}
	}

	for fenKey, freq := range pos {
		if freq < 3 {
			delete(pos, fenKey)
		}
	}

	for fenKey := range pos {
		san := db.MostFrequentMove(fenKey)
		fmt.Printf("%s; bm %s;\n", fenKey, san)
	}
}

func getGames() {
	if err := api.GetGames("TrollololFish", time.Now(), 2); err != nil {
		log.Fatal(err)
	}
}

func positionLookup() {
	results, err := api.Lookup("", "e2e4,c7c5,d2d4,c5d4,c2c3,d4c3,b1c3")
	if err != nil {
		log.Fatal(err)
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", b)
}

func analysis() {
	//moves := strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 d7d5 f1g2 f8b4 c1d2 b4e7 g1f3 e8g8 e1g1 b8d7 d1c2 c7c6 d2f4 b7b6 c4d5 c6d5 f1c1 c8a6 b1c3 f6h5 f4e5 h5f6 c2d1 d7e5 f3e5 a6b7 e2e3 f6d7 e5d3 a8c8 g2f1 a7a6 a2a4 c8c4 c1c2 h7h6 b2b3 c4c6 b3b4 c6c4 a1b1 d8b8 a4a5 f8c8 d1e1 b6b5 f2f4 e7f6 c2c1 b8c7 c3d1 c7d8 d1b2 c4c1 d3c1 f6e7 c1b3 e7d6 e1d2 c8c7 b2d3 d7f6 d2g2 f6e4 b3c5 d6e7 b1e1 g7g6 d3e5 e7f6 e1c1 d8e7 f1d3 e4c5 b4c5 b7c6 e5f3 c7c8 h2h4 g8g7 h4h5 g6h5 g1f2 c8g8 c1h1 e7c7 g2h2 h5h4 f3h4 g7f8 h4f3 c7a5 f4f5 h6h5 f5e6 a5a2 d3e2 b5b4 h2h5 f7e6 f3e5 c6e8 h5f3 f8e7 e5g4 f6g7 h1h7 e7d8 g4f6 g8f8 h7g7 e8b5 g3g4 a2e2 f3e2 f8f6 f2e1 b5e2 e1e2 f6h6 g4g5 h6h1 e2d2 e6e5 g5g6 h1h2 d2c1 e5d4 e3d4 h2g2 g7g8 d8e7 c5c6 e7d6 g6g7 d6c7 g8a8 g2g1 c1c2 c7c6 g7g8Q g1g8 a8g8 c6d7 c2b3 d7e6 b3b4 e6f5 g8a8 f5g4 a8a6 g4f5 b4c5 f5e4 a6a3 e4f5 c5d5 f5f4 d5c6 f4g5 d4d5 g5f4 d5d6 f4g5 d6d7 g5f4 d7d8Q f4f5 a3f3 f5g4 f3f8 g4h3 f8g8 h3h2 d8h4", " ")
	/*moves := strings.Split("e2e4 c7c5 e4e5 b8c6 f2f4 d7d6 g1f3 c8g4 h2h3 g4f3 d1f3 d6e5 f4f5 g8f6 d2d3 c6d4 f3d1 e5e4 c2c3 d4f5 d3e4 f6e4 d1d8 a8d8 c1f4 e7e6 f1b5 e8e7 e1g1 e7f6 b1a3 f8d6 f4d6 d8d6 a3c4 d6d8 g1h2 e4d2 c4d2 d8d2 b2b4 c5b4 c3b4 f6e7 a2a3 h8d8 b5e2 d2e2 a1c1 d8d7 f1d1 d7d1 c1d1 f5e3 d1g1 e2g2 g1g2 e3g2 h2g2 e7d6", " ")

	if err := a.AnalyzeGame(context.Background(), moves); err != nil {
		log.Fatal(err)
	}*/

	//_, _ = fmt.Fprintf(os.Stderr, "total time: %v\n", time.Since(start).Round(time.Second))
}

func runLichessBot() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startTrollFish(ctx, input, output); err != nil {
		log.Fatal(err)
	}

	listener := New(ctx, input, output)

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
