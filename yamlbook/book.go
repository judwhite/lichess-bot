package yamlbook

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"sort"

	"gopkg.in/yaml.v3"

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

type Position struct {
	FEN   string `yaml:"fen"`
	Moves Moves  `yaml:"moves"`
}

type Move struct {
	Move   string  `yaml:"move,omitempty"`
	Weight int     `yaml:"weight,omitempty"`
	CP     int     `yaml:"cp,omitempty"`
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
		// if any have 'move', remove the entries that don't
		for i := 0; i < len(position.Moves); i++ {
			if position.Moves[i].Move == "" {
				position.Moves = append(position.Moves[:i], position.Moves[i+1:]...)
				i--
				continue
			}
		}
	}

	position.Moves = append(position.Moves, moves...)
}

func (b *Book) Save() error {
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
