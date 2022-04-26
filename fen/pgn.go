package fen

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Database struct {
	Games []*Game
}

type Game struct {
	Moves []LegalMove

	Positions map[string][]Move
}

type Move struct {
	SAN string
	UCI string
}

func (g *Game) populatePositions() {
	if len(g.Moves) == 0 {
		return
	}

	var b Board
	pos := make(map[string][]Move, len(g.Moves))

	b.Moves(g.Moves[0].UCI)
	for i := 1; i < len(g.Moves); i++ {
		key := b.FENNoMoveClocks()
		uci := g.Moves[i].UCI

		//fmt.Printf("fen: '%s' uci: %s i: %d\n", key, uci, i)

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
	var db Database

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
			moves, err := PGNtoMoves(s)
			if err != nil {
				panic(err)
			}

			if len(moves) != 0 {
				game := &Game{Moves: moves}
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

func PGNtoMoves(pgn string) ([]LegalMove, error) {
	pgn = strings.TrimSpace(pgn)
	if len(pgn) == 0 {
		return nil, nil
	}

	var moves []LegalMove

	lines := strings.Split(pgn, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if len(line) > 0 && !strings.HasPrefix(line, "[") {
			// start of move data
			lines = lines[i:]
			break
		}
	}

	pgn = strings.TrimSpace(strings.Join(lines, " "))
	parts := strings.Split(pgn, " ")
	var b Board
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

		legalMoves := b.PieceLegalMoves(piece)
		var move LegalMove
		var uci string
		var lastUCItoSAN string
		for _, legalMove := range legalMoves {
			if legalMove.Piece == piece {
				lastUCItoSAN = b.UCItoSAN(legalMove.UCI)
				if lastUCItoSAN == san {
					uci = legalMove.UCI
					move = legalMove
					break
				}
			}
		}
		if san == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: <empty>", b.FEN(), fullMove, b.ActiveColor, part)
		}
		if san != part {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' want: '%s' got: '%s'", b.FEN(), fullMove, b.ActiveColor, part, san)
		}
		if uci == "" {
			return nil, fmt.Errorf("FEN: '%s' full_move: %d color: '%s' piece: '%c' san: '%s' uci: <empty> move: %v legalMoves: %v last_san_check: %s", b.FEN(), fullMove, b.ActiveColor, piece, part, move, legalMoves, lastUCItoSAN)
		}
		moves = append(moves, move)
		b.Moves(uci)
	}
	return moves, nil
}
