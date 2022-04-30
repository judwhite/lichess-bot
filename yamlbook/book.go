package yamlbook

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
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

type Moves []*Move

func (m Moves) Less(i, j int) bool {
	if m[i].Weight != m[j].Weight {
		return m[i].Weight > m[j].Weight
	}

	if m[i].Mate != m[j].Mate {
		return m[i].Mate > m[j].Mate
	}

	return m[i].CP > m[j].CP
}

func (m Moves) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

func (m Moves) Len() int {
	return len(m)
}

func (m Moves) ContainsEvalsFrom(engineID string) bool {
	for _, move := range m {
		if move.Engine != nil && move.Engine.ID == engineID {
			return true
		}
	}
	return false
}

func (m Moves) ContainsSAN(san string) bool {
	for _, move := range m {
		if move.Move == san {
			return true
		}
	}
	return false
}

func (m Moves) GetSAN(san string) *Move {
	for _, move := range m {
		if move.Move == san {
			return move
		}
	}
	return nil
}

func (m Moves) GetBestMoveByEval() *Move {
	var bestMove *Move
	for _, move := range m {
		if bestMove == nil {
			bestMove = move
			continue
		}

		if move.Mate > bestMove.Mate {
			bestMove = move
			continue
		}

		if move.CP > bestMove.CP {
			bestMove = move
			continue
		}
	}

	return bestMove
}

type Position struct {
	FEN   string `yaml:"fen"`
	Moves Moves  `yaml:"moves,omitempty"`
}

type Move struct {
	Move   string  `yaml:"move,omitempty"`
	Weight int     `yaml:"weight,omitempty"`
	CP     int     `yaml:"cp"`
	Mate   int     `yaml:"mate,omitempty"`
	TS     int64   `yaml:"ts,omitempty"`
	Engine *Engine `yaml:"engine,omitempty"`

	uci string
	fen string
}

func (m *Move) UCI() string {
	if m.uci != "" {
		return m.uci
	}

	if m.fen == "" {
		log.Fatalf("internal error: fen not set %#v", m)
	}

	if m.Move == "" {
		panic(fmt.Errorf("internal error: m.Move is '', key: '%s' m: %#v", m.fen, m))
	}

	uci, err := fen.FENtoBoard(m.fen).SANtoUCI(m.Move)
	if err != nil {
		log.Fatal(err)
	}

	m.uci = uci

	return m.uci
}

func (m *Move) GetLastLogLineFor(move string) LogLine {
	if m.Engine == nil {
		return LogLine{}
	}

	for _, output := range m.Engine.Output {
		pvSANs := strings.Split(output.Line.PV, " ")
		if len(pvSANs) == 0 {
			continue
		}
		if pvSANs[0] == move {
			return output.Line
		}
	}

	return LogLine{}
}

func (m *Move) FEN() string {
	if m.fen == "" {
		log.Fatalf("internal error: fen not set %#v", m)
	}

	return m.fen
}

type Engine struct {
	ID     string          `yaml:"id,omitempty"`
	Output []*EngineOutput `yaml:"output"`
}

func (e *Engine) Log(logLine LogLine) {
	e.Output = append(e.Output, &EngineOutput{Line: logLine})
}

type EngineOutput struct {
	Line LogLine `yaml:"log,flow"`
}

type LogLine struct {
	Depth    int    `yaml:"depth"`
	SelDepth int    `yaml:"seldepth,omitempty"`
	MultiPV  int    `yaml:"multipv,omitempty"`
	CP       int    `yaml:"cp"`
	Mate     int    `yaml:"mate,omitempty"`
	Nodes    int    `yaml:"nodes,omitempty"`
	TBHits   int    `yaml:"tbhits,omitempty"`
	Time     int    `yaml:"time,omitempty"`
	PV       string `yaml:"pv"`
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

	for _, pos := range book.Positions {
		sort.Sort(pos.Moves)
		book.posMap[pos.FEN] = pos
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
	}
}

func (b *Book) Save() error {
	// remove blank moves (and any other data they might contain)
	for _, pos := range b.Positions {
		for i := 0; i < len(pos.Moves); i++ {
			if pos.Moves[i].Move == "" || pos.Moves[i].Engine.ID == "lichess" && pos.Moves[i].Engine.Output[0].Line.Depth < 28 {
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
