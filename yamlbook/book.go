package yamlbook

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"trollfish-lichess/api"
	"trollfish-lichess/fen"
)

type Book struct {
	Positions []*Position

	posMap   map[string]*Position
	filename string
}

func Load(filename string) (*Book, error) {
	book := Book{
		posMap:   make(map[string]*Position),
		filename: filename,
	}

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("'%s': %v", filename, err)
	}

	if err := yaml.Unmarshal(b, &book.Positions); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	for i := 0; i < len(book.Positions); i++ {
		pos := book.Positions[i]

		_, found := seen[pos.FEN]
		if !found {
			seen[pos.FEN] = struct{}{}
			continue
		}
		if len(pos.Moves) > 0 {
			panic(fmt.Errorf("position '%s' duplicated with moves", pos.FEN))
		}
		fmt.Printf("removed duplicate '%s'\n", pos.FEN)
		book.Positions = append(book.Positions[:i], book.Positions[i+1:]...)
		i--
	}

	for _, pos := range book.Positions {
		for _, move := range pos.Moves {
			line := move.GetLastLogLineFor(move.Move)
			var empty LogLine
			if line != empty && line.CP != move.CP {
				move.CP = line.CP
			}
		}

		sort.Stable(pos.Moves)
		book.posMap[pos.FEN] = pos
	}

	if err := book.Save(); err != nil {
		return nil, err
	}

	return &book, nil
}

func (b *Book) Get(fenKey string) (Moves, bool) {
	fenKey = fen.Key(fenKey)

	position, ok := b.posMap[fenKey]
	if !ok {
		return nil, false
	}

	result := make(Moves, 0, len(position.Moves))

	for i := 0; i < len(position.Moves); i++ {
		position.Moves[i].fen = position.FEN
		if position.Moves[i].Move != "" {
			result = append(result, position.Moves[i])
		}
	}

	if len(result) == 0 {
		return nil, false
	}

	return result, true
}

func (b *Book) GetAll(fenKey string) (Moves, bool) {
	fenKey = fen.Key(fenKey)

	position, ok := b.posMap[fenKey]
	if !ok {
		return nil, false
	}

	for i := 0; i < len(position.Moves); i++ {
		position.Moves[i].fen = position.FEN
	}

	return position.Moves, true
}

func (b *Book) Add(fenKey string, moves ...*Move) {
	fenKey = fen.Key(fenKey)

	position, ok := b.posMap[fenKey]
	if !ok {
		position = &Position{FEN: fenKey}
		b.posMap[fenKey] = position
		b.Positions = append(b.Positions, position)
	}

	for _, move := range moves {
		move.fen = fenKey
	}

	var anyHaveMove bool
	for i := 0; i < len(moves); i++ {
		if moves[i].Move != "" {
			anyHaveMove = true
			break
		}
	}

	if anyHaveMove {
		// if any new entries have a SAN Move, remove the existing entries that don't
		for i := 0; i < len(position.Moves); i++ {
			if position.Moves[i].Move == "" {
				position.Moves = append(position.Moves[:i], position.Moves[i+1:]...)
				i--
				continue
			}
		}
	}

	// clobber where move is the same
	for i := 0; i < len(position.Moves); i++ {
		if position.Moves[i].Move == "" {
			continue
		}

		for j := 0; j < len(moves); j++ {
			if moves[j].Move != position.Moves[i].Move {
				continue
			}

			position.Moves[i] = moves[j]
			moves = append(moves[:j], moves[j+1:]...)
			break
		}
	}

	if len(moves) > 0 {
		position.Moves = append(position.Moves, moves...)
		sort.Stable(position.Moves)
	}
}

func (b *Book) Save() error {
	// remove blank moves (and any other data they might contain)
	for _, pos := range b.Positions {
		for i := 0; i < len(pos.Moves); i++ {
			if pos.Moves[i].Move == "" {
				pos.Moves = append(pos.Moves[:i], pos.Moves[i+1:]...)
				i--
				continue
			}
		}

		if len(pos.Moves) == 0 {
			pos.Moves = nil
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(b.Positions); err != nil {
		return fmt.Errorf("'%s': %v", b.filename, err)
	}

	if err := ioutil.WriteFile(b.filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write file '%s': %v", b.filename, err)
	}

	return nil
}

func (b *Book) CheckOnlineDatabase(ctx context.Context, boardFEN string) error {
	results, err := api.CloudEval(boardFEN, 5)
	if err != nil {
		if err == api.ErrNotFound {
			return nil
		}
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// minDepth
	if results.Depth < 28 || len(results.PVs) == 0 {
		return nil
	}

	board := fen.FENtoBoard(boardFEN)
	povMultiplier := iif(board.ActiveColor == fen.WhitePieces, 1, -1)

	for i, pv := range results.PVs {
		pvUCI := strings.Split(pv.Moves, " ")
		pvSAN := board.UCItoSANs(pvUCI...)

		cp := pv.CP * povMultiplier
		mate := pv.Mate * povMultiplier
		ts := time.Now().Unix()

		move := Move{
			Move: pvSAN[0],
			CP:   cp,
			Mate: mate,
			TS:   ts,
			Engine: &Engine{
				ID: "lichess",
				Output: []*EngineOutput{{
					Line: LogLine{
						Depth:   results.Depth,
						MultiPV: i + 1,
						CP:      cp,
						Mate:    mate,
						Nodes:   results.KNodes * 1024,
						PV:      strings.Join(pvSAN, " "),
					},
				}},
			},
		}

		b.Add(boardFEN, &move)

		fmt.Printf("attempting to update '%s' cp: %d with ts = %d\n", move.Move, move.CP, move.TS)
	}

	fmt.Printf("just called save... go check it out\n")

	if err := b.Save(); err != nil {
		return err
	}

	return nil
}

func (b *Book) BestMove(fenPos string) (*Move, string) {
	if b == nil || b.posMap == nil {
		return nil, ""
	}
	board := fen.FENtoBoard(fenPos)
	fenKey := board.FENKey()
	pos, ok := b.posMap[fenKey]
	if !ok {
		return nil, ""
	}

	sort.Stable(pos.Moves)
	moves := pos.Moves

	if len(moves) == 0 {
		return nil, ""
	}

	var bestMove *Move

	// TODO: add variance by weight
	bestMove = moves[0]

	if bestMove.Weight == 0 {
		i := 1
		text := bestMove.Move
		for ; i < len(moves); i++ {
			if moves[i].CP != bestMove.CP || moves[i].Mate != bestMove.Mate {
				break
			}
			text += ", " + moves[i].Move
		}
		if i > 1 {
			n := rand.Intn(i)
			bestMove = moves[n]
			fmt.Printf("moves: '%s' count: %d pick: '%s' eval: %d\n", text, i, bestMove.Move, bestMove.CP)
		}
	} else {
		type weightedMove struct {
			start int
			end   int
			index int
		}
		var deck []weightedMove

		sum := 0
		for i := 0; i < len(moves); i++ {
			if moves[i].Weight <= 0 {
				break
			}

			start := sum

			sum += moves[i].Weight
			end := sum - 1

			deck = append(deck, weightedMove{start: start, end: end, index: i})
		}

		num := rand.Intn(sum)
		for _, card := range deck {
			if card.start <= num && card.end >= num {
				bestMove = moves[card.index]
				chancePercent := float64(card.end-card.start) / float64(sum)
				fmt.Printf("WEIGHTED CHOICE: choices: %d sum: %d num: %d<=%d<=%d %4.1f%% pick: %s cp: %d\n",
					len(deck), sum, card.start, num, card.end, chancePercent*100, bestMove.Move, bestMove.CP)
				break
			}
		}
	}

	bestMove.fen = fenKey

	line := bestMove.GetLastLogLineFor(bestMove.Move)
	pvSANs := strings.Split(line.PV, " ")

	if len(pvSANs) > 1 {
		board.Moves(bestMove.UCI())
		ponder, err := board.SANtoUCI(pvSANs[1])
		if err != nil {
			fmt.Printf("ERROR: %v !!!!!!!!!!!!\n", err)
			ponder = ""
		}
		return bestMove, ponder
	}

	return bestMove, ""
}

func (b *Book) PosCount() int {
	return len(b.posMap)
}

func (b *Book) NeedMoves() []string {
	var fens []string

	for _, pos := range b.Positions {
		if len(pos.Moves) == 0 {
			fens = append(fens, pos.FEN)
		}
	}

	return fens
}
