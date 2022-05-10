package fen

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Database struct {
	Games []*PGNGame

	Positions map[string][]PGNMove
}

type GameResult int

const (
	OtherResult = 0
	WhiteWon    = 1
	BlackWon    = 2
	Draw        = 3
)

func (gr GameResult) String() string {
	switch gr {
	case WhiteWon:
		return "1-0"
	case BlackWon:
		return "0-1"
	case Draw:
		return "1/2-1/2"
	default:
		return "*"
	}
}

type PGNGame struct {
	SetupFEN string
	White    string
	Black    string
	WhiteElo int
	BlackElo int
	Result   GameResult

	Tags  Tags
	Moves []PGNMove

	Positions map[string][]Move
}

type Move struct {
	SAN string
	UCI string
}

func (g *PGNGame) populatePositions() {
	if len(g.Moves) == 0 {
		return
	}

	b := FENtoBoard(g.SetupFEN)
	pos := make(map[string][]Move, len(g.Moves))

	b.Moves(g.Moves[0].UCI)
	for i := 1; i < len(g.Moves); i++ {
		key := b.FENKey()
		uci := g.Moves[i].UCI

		san := b.UCItoSAN(uci)

		pos[key] = append(pos[key], Move{UCI: uci, SAN: san})

		b.Moves(uci)
	}

	g.Positions = pos
}

func (db *Database) MostFrequentMove(fen string) string {
	type moveFreq struct {
		san  string
		freq int
	}

	m := make(map[string]int)
	for _, game := range db.Games {
		moves, ok := game.Positions[fen]
		if !ok {
			continue
		}

		for _, move := range moves {
			m[move.SAN] += 1
		}
	}

	var list []moveFreq
	for k, v := range m {
		list = append(list, moveFreq{san: k, freq: v})
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].freq > list[j].freq
	})

	if len(list) == 0 {
		return "-"
	}

	return list[0].san
}

func LoadPGNDatabase(filename string) (Database, error) {
	db := Database{
		Positions: make(map[string][]PGNMove),
	}

	fp, err := os.Open(filename)
	if err != nil {
		return db, err
	}
	defer fp.Close()

	r := bufio.NewScanner(fp)

	var (
		pgn    strings.Builder
		mtx    sync.Mutex
		wg     sync.WaitGroup
		isGame bool
	)

	addGame := func() error {
		if pgn.Len() == 0 {
			return nil
		}

		s := pgn.String()

		wg.Add(1)
		go func() {
			defer wg.Done()
			game, err := ParsePGN(s)
			if err != nil {
				fmt.Printf("PGN:\n\n%s\n\n\n", s)
				panic(err)
			}

			if len(game.Moves) != 0 {
				game.populatePositions()
				mtx.Lock()
				db.Games = append(db.Games, game)
				mtx.Unlock()
			}
		}()

		pgn.Reset()
		isGame = false
		return nil
	}

	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if !strings.HasPrefix(line, "[") && len(line) != 0 {
			isGame = true
		}

		if len(line) == 0 && isGame {
			if err := addGame(); err != nil {
				return db, err
			}
			continue
		}

		if pgn.Len() != 0 {
			pgn.WriteRune('\n')
		}
		pgn.WriteString(line)
	}

	if err := r.Err(); err != nil {
		return db, err
	}

	if err := addGame(); err != nil {
		return db, err
	}

	wg.Wait()

	return db, nil
}

type Tags map[string]string

func (g *PGNGame) ParseTags(pgn string) string {
	lines := strings.Split(strings.TrimSpace(pgn), "\n")
	var sb strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "[") {
			line = strings.Trim(line, "[]")
			idx := strings.Index(line, " ")
			if idx == -1 {
				continue
			}
			key := line[:idx]
			value := line[idx+2 : len(line)-1]

			g.Tags[key] = value

			switch key {
			case "FEN":
				g.SetupFEN = value
			case "White":
				g.White = value
			case "WhiteElo":
				g.WhiteElo = atoi(value)
			case "Black":
				g.Black = value
			case "BlackElo":
				g.BlackElo = atoi(value)
			case "Result":
				switch value {
				case "1-0":
					g.Result = WhiteWon
				case "0-1":
					g.Result = BlackWon
				case "1/2-1/2":
					g.Result = Draw
				default:
					g.Result = OtherResult
				}
			}
		} else if line != "" {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

type PGNMove struct {
	FENKey string
	UCI    string
}

func ParsePGN(pgn string) (*PGNGame, error) {
	game := PGNGame{
		Tags: make(Tags),
	}

	pgn = game.ParseTags(pgn)
	pgn = strings.TrimSpace(pgn)
	if len(pgn) == 0 {
		return nil, nil
	}

	lines := strings.Split(pgn, "\n")
	pgn = strings.TrimSpace(strings.Join(lines, " "))
	parts := strings.Split(pgn, " ")
	b := FENtoBoard(game.SetupFEN)
	var fullMove int
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if part == "1-0" || part == "0-1" || part == "1/2-1/2" || part == "*" || part == "" {
			continue
		}

		if strings.HasSuffix(part, ".") {
			moveNum := strings.TrimRight(part, ".")
			n, err := strconv.Atoi(moveNum)
			if err != nil {
				return nil, fmt.Errorf("%v: '%s'", err, moveNum)
			}
			fullMove = n
			continue
		}

		if strings.HasPrefix(part, "{") {
			for i = i + 1; i < len(parts); i++ {
				if strings.HasSuffix(parts[i], "}") {
					break
				}
			}
			continue
		}

		san := part

		piece := san[0]
		if piece >= 'a' && piece <= 'h' {
			piece = 'P'
		} else if piece == 'O' {
			piece = 'K'
		}
		if b.ActiveColor == BlackPieces {
			piece = lower(piece)
		}

		uci, err := b.SANtoUCI(san)
		if err != nil {
			log.Fatal(err)
		}
		move := PGNMove{FENKey: b.FENKey(), UCI: uci}

		if san == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: <empty>", b.FEN(), fullMove, b.ActiveColor, part)
		}
		if san != part {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: '%s'", b.FEN(), fullMove, b.ActiveColor, part, san)
		}
		if uci == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' piece: '%c' san: '%s' uci: <empty> move: %v", b.FEN(), fullMove, b.ActiveColor, piece, part, move)
		}

		game.Moves = append(game.Moves, move)
		b.Moves(uci)
	}
	return &game, nil
}
