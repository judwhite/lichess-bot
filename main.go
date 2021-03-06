package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"trollfish-lichess/analyze"
	"trollfish-lichess/api"
	"trollfish-lichess/epd"
	"trollfish-lichess/fen"
	"trollfish-lichess/yamlbook"
)

var defaultAnalysisOptions = analyze.AnalysisOptions{
	MinDepth:   45,
	MaxDepth:   80,
	MinTime:    60 * time.Second,
	MaxTime:    90 * time.Minute,
	DepthDelta: 3,
	MultiPV:    5,
	MinNodes:   3_600_000_000,
}

func main() {
	rand.Seed(time.Now().UnixNano())

	var (
		botFlag              bool
		updateBookFilename   string
		startingFEN          string
		dedupeEPDFilename    string
		freqPGNFilename      string
		freqMergeEPDFilename string
		freqCount            int
		freqMaxPly           int
		lichessUser          string
		onlyUser             string
		challenge            string
		analyzePGN           string
		analyzeUseBook       string
		extractEPD           string
		extractEPDPlies      int
		tc                   string
		epdToYAMLBook        string
		bustedPGNFile        string
		bustedPlayer         string
		bustedColor          string
		searchMoves          string
	)

	var flags flag.FlagSet

	// bot
	flags.BoolVar(&botFlag, "bot", false, "runs the bot")
	flags.StringVar(&tc, "tc", "1+1", "time control minutes+secs")
	flags.StringVar(&onlyUser, "only-user", "", "only accept challenges from this user")
	flags.StringVar(&challenge, "challenge", "", "challenge lichess user")

	// update yaml book
	flags.StringVar(&updateBookFilename, "update-book", "", "run analysis and update a book")
	flags.StringVar(&startingFEN, "fen", "", "run analysis and update a book on a specific FEN. when used with -bot instead of -update-book creates a challenge with this starting FEN")
	flags.StringVar(&searchMoves, "search-moves", "", "run analysis only on these moves. use SAN and separate with commas")

	// frequency counts
	flags.StringVar(&freqPGNFilename, "freq-pgn", "", "show most common positions from a PGN file in EPD format (see also freq-count)")
	flags.StringVar(&freqMergeEPDFilename, "freq-merge-epd", "", "merge positions with an EPD file. only new positions are added.")
	flags.IntVar(&freqCount, "freq-count", 3, "minimum times a position must occur (see freq-pgn)")
	flags.IntVar(&freqMaxPly, "freq-max-ply", 0, "max ply to analyze, 0 = all (see freq-pgn)")

	// download lichess user's games
	flags.StringVar(&lichessUser, "lichess-user", "", "get all rated games for a lichess user")

	// analyze a PGN file
	flags.StringVar(&analyzePGN, "analyze-pgn", "", "analyze pgn file")
	flags.StringVar(&analyzeUseBook, "analyze-use-book", "", "use saved position eval in YAML book")

	// EPD stuff
	flags.StringVar(&dedupeEPDFilename, "dedupe-epd", "", "show duplicates in EPD file")
	flags.StringVar(&extractEPD, "extract-epd", "", "pgn file name")
	flags.IntVar(&extractEPDPlies, "extract-epd-plies", 0, "number of plies to extract")
	flags.StringVar(&epdToYAMLBook, "epd-to-yamlbook", "", "EPD file name to convert (new file will be <file>.yamlbook)")

	// busted lines from pgn database; work in progress
	flags.StringVar(&bustedPGNFile, "busted-pgn", "", "find busted lines in a PGN file")
	flags.StringVar(&bustedPlayer, "busted-player", "", "player name")
	flags.StringVar(&bustedColor, "busted-color", "", "white or black")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			flags.PrintDefaults()
			os.Exit(1)
		}
		log.Fatal(err)
	}

	if challenge != "" {
		onlyUser = challenge
	}

	if botFlag {
		var timeControl TimeControl
		if err := timeControl.Parse(tc); err != nil {
			log.Fatal(err)
		}

		runLichessBot(onlyUser, challenge, timeControl, startingFEN)
		return
	}

	if updateBookFilename != "" {
		var fens []string
		if startingFEN != "" {
			if strings.Contains(startingFEN, "/") && strings.Contains(startingFEN, " ") {
				fens = append(fens, startingFEN)
			} else {
				b, err := ioutil.ReadFile(startingFEN)
				if err != nil {
					log.Fatal(err)
				}
				lines := strings.Split(string(b), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						fens = append(fens, line)
					}
				}
			}
		}
		if err := UpdateFile(context.Background(), updateBookFilename, defaultAnalysisOptions, fens, searchMoves); err != nil {
			log.Fatal(err)
		}
		return
	}

	if bustedPlayer != "" && bustedPGNFile != "" && bustedColor != "" {
		var color fen.Color
		if bustedColor == "white" || bustedColor == "w" {
			color = fen.WhitePieces
		} else if bustedColor == "black" || bustedColor == "b" {
			color = fen.BlackPieces
		}

		if _, err := Busted(bustedPGNFile, color); err != nil {
			log.Fatal(err)
		}
		return
	}

	if dedupeEPDFilename != "" {
		if err := epd.Dedupe(dedupeEPDFilename); err != nil {
			log.Fatal(err)
		}
		return
	}

	if epdToYAMLBook != "" {
		file, err := epd.LoadFile(epdToYAMLBook)
		if err != nil {
			log.Fatal(err)
		}

		ext := filepath.Ext(epdToYAMLBook)
		yamlBookFilename := strings.TrimSuffix(epdToYAMLBook, ext) + ".yamlbook"

		if err := file.SaveAsYAMLBook(yamlBookFilename, true); err != nil {
			log.Fatal(err)
		}

		return
	}

	if freqPGNFilename != "" && freqCount > 0 {
		if err := GetMostFrequentPGNPositions(freqPGNFilename, freqCount, freqMergeEPDFilename); err != nil {
			log.Fatal(err)
		}
		return
	}

	if lichessUser != "" {
		start := time.Now()

		fn, count, err := api.GetGames(lichessUser, 0)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("saved %s with %d games in %v\n", fn, count, time.Since(start).Round(time.Second))
		return
	}

	if analyzePGN != "" {
		var (
			book *yamlbook.Book
			err  error
		)

		if analyzeUseBook != "" {
			book, err = yamlbook.Load(analyzeUseBook)
			if err != nil {
				log.Fatal(err)
			}
		}

		a := analyze.New()
		if err := a.AnalyzePGNFile(context.Background(), defaultAnalysisOptions, analyzePGN, book); err != nil {
			log.Fatal(err)
		}
		return
	}

	if extractEPD != "" && extractEPDPlies > 0 {
		db, err := fen.LoadPGNDatabase(extractEPD)
		if err != nil {
			log.Fatal(err)
		}

		file, err := epd.LoadFile("book.epd")
		if err != nil {
			log.Fatal(err)
		}

		for _, game := range db.Games {
			board := fen.FENtoBoard(game.SetupFEN)
			for i := 0; i < len(game.Moves) && i < extractEPDPlies; i++ {
				move := game.Moves[i].UCI
				board.Moves(move)
				fenKey := board.FENKey()
				if file.Contains(fenKey) {
					//fmt.Printf("already have: %s\n", file.Find(fenKey))
					continue
				}
				if i < len(game.Moves)-1 {
					uci := game.Moves[i+1].UCI
					san := board.UCItoSAN(uci)
					line := file.Add(fenKey, epd.Operation{OpCode: "sm", Value: san})
					fmt.Printf("%s\n", line.String())
				} else {
					line := file.Add(fenKey)
					fmt.Printf("%s\n", line.String())
				}
			}
		}

		fmt.Println(file.String())

		return
	}

	flags.PrintDefaults()
	os.Exit(1)
}

func GetMostFrequentPGNPositions(filename string, minCount int, epdFilename string) error {
	db, err := fen.LoadPGNDatabase(filename)
	if err != nil {
		return err
	}

	var moves int
	pos := make(map[string]int)

	for _, game := range db.Games {
		moves += len(game.Moves)
		for k := range game.Positions {
			pos[k] += 1
		}
	}

	for fenKey, freq := range pos {
		if freq < minCount {
			delete(pos, fenKey)
		}
	}

	if epdFilename != "" {
		epdFile, err := epd.LoadFile(epdFilename)
		if err != nil {
			return err
		}

		var newPositions int
		for fenKey := range pos {
			if !epdFile.Contains(fenKey) {
				san := db.MostFrequentMove(fenKey)
				epdFile.Add(fenKey, epd.Operation{OpCode: epd.OpCodeSuppliedMove, Value: san})
				newPositions++
			}
		}

		if newPositions == 0 {
			fmt.Printf("no new positions found\n")
		} else {
			if err := epdFile.Save(epdFilename, true); err != nil {
				return err
			}
			fmt.Printf("'%s' saved, %d new position(s)\n", epdFilename, newPositions)
		}
	} else {
		epdFile := epd.New()
		for fenKey := range pos {
			san := db.MostFrequentMove(fenKey)
			epdFile.Add(fenKey, epd.Operation{OpCode: epd.OpCodeSuppliedMove, Value: san})
		}

		fmt.Print(epdFile.String())
	}

	return nil
}

func positionLookup() {
	results, err := api.Lookup(api.Masters, "", "e2e4", "c7c5", "d2d4", "c5d4", "c2c3", "d4c3", "b1c3")
	if err != nil {
		log.Fatal(err)
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", b)
}

func runLichessBot(onlyUser, challenge string, tc TimeControl, fenPos string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan string, 512)
	output := make(chan string, 512)

	if err := startTrollFish(ctx, input, output); err != nil {
		log.Fatal(err)
	}

	listener := New(ctx, input, output, onlyUser, challenge, tc, fenPos)

	if err := listener.Events(); err != nil {
		log.Fatal(err)
	}
}

func startTrollFish(ctx context.Context, input <-chan string, output chan<- string) error {
	// TODO: put in config
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

func UpdateFile(ctx context.Context, filename string, opts analyze.AnalysisOptions, fens []string, searchMoves string) error {
	if len(fens) != 1 && searchMoves != "" {
		return fmt.Errorf("-search-moves can only be used with -fen")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var searchMovesUCI []string
	if searchMoves != "" {
		b := fen.FENtoBoard(fens[0])

		var searchMovesSAN []string
		if searchMoves == "all" {
			allMoves := b.AllLegalMoves()
			for _, move := range allMoves {
				searchMovesSAN = append(searchMovesSAN, b.UCItoSAN(move.UCI))
			}
		} else {
			searchMovesSAN = strings.Split(searchMoves, ",")
		}

		seen := make(map[string]struct{})
		for i := 0; i < len(searchMovesSAN); i++ {
			moveSAN := searchMovesSAN[i]

			_, found := seen[moveSAN]
			if found {
				fmt.Printf("duplicate '%s' removed\n", moveSAN)
				searchMovesSAN = append(searchMovesSAN[:i], searchMovesSAN[i+1:]...)
				i--
				continue
			}
			seen[moveSAN] = struct{}{}

			moveUCI, err := b.SANtoUCI(moveSAN)
			if err != nil {
				return err
			}
			searchMovesUCI = append(searchMovesUCI, moveUCI)
		}

		fmt.Printf("FEN: %v\n", b.FEN())
		fmt.Printf("SAN searchmoves: %v len: %d\n", searchMovesSAN, len(searchMovesSAN))
		fmt.Printf("UCI searchmoves: %v len: %d\n", searchMovesUCI, len(searchMovesUCI))
	}

	file, err := yamlbook.Load(filename)
	if err != nil {
		return err
	}

	a := analyze.New()

	wg, err := a.StartStockfish(ctx)
	if err != nil {
		return err
	}

	if len(fens) == 0 {
		fens = file.NeedMoves()
	}

	fmt.Printf("%d positions to analyze\n", len(fens))
	pieceCountToPosCount := make(map[int]int)
	for i := 0; i < len(fens); i++ {
		b := fen.FENtoBoard(fens[i])

		pc := b.PieceCount()
		pieceCountToPosCount[pc] += 1
	}
	for i := 32; i >= 0; i-- {
		posCount := pieceCountToPosCount[i]
		if posCount == 0 {
			continue
		}
		fmt.Printf("%2d pieces: %5d\n", i, posCount)
	}

	for i := 0; i < len(fens); i++ {
		start := time.Now()
		boardFEN := fens[i]
		fmt.Printf("%s FEN: %s  piece_count: %d\n%s\n", ts(), boardFEN, fen.FENtoBoard(boardFEN).PieceCount(), ts())

		fenKey := fen.Key(boardFEN)
		evals, err := a.AnalyzePosition(ctx, opts, fenKey, searchMovesUCI...)
		if err != nil {
			return err
		}

		if err := a.SaveEvalsToBook(file, fenKey, evals); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		fmt.Printf("%s\n%s FEN: %s complete in %v\n%s -----\n%s\n", ts(), ts(), boardFEN, time.Since(start).Round(time.Second), ts(), ts())
	}

	cancel()

	if wg != nil {
		wg.Wait()
	}

	return nil
}

func order(m map[string]int) []string {
	type asdf struct {
		key   string
		value int
	}

	list1 := make([]asdf, 0, len(m))
	for k, v := range m {
		list1 = append(list1, asdf{key: k, value: v})
	}

	sort.Slice(list1, func(i, j int) bool {
		return list1[i].value > list1[j].value
	})

	list2 := make([]string, 0, len(list1))
	for _, item := range list1 {
		list2 = append(list2, item.key)
	}

	return list2
}
