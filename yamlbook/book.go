package yamlbook

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"
)

type Book struct {
	Positions []*Position
}

type Position struct {
	FEN   string  `yaml:"fen"`
	Moves []*Move `yaml:"moves"`
}

type Move struct {
	Move   string  `yaml:"move,omitempty"`
	Weight int     `yaml:"weight,omitempty"`
	CP     int     `yaml:"cp,omitempty"`
	Mate   int     `yaml:"mate,omitempty"`
	Ponder string  `yaml:"ponder,omitempty"`
	TS     int64   `yaml:"ts,omitempty"`
	Engine *Engine `yaml:"engine,omitempty"`
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
	SelDepth int    `yaml:"seldepth"`
	MultiPV  int    `yaml:"multipv"`
	CP       int    `yaml:"cp"`
	Mate     int    `yaml:"mate,omitempty"`
	Nodes    int    `yaml:"nodes"`
	TBHits   int    `yaml:"tbhits"`
	Time     int    `yaml:"time"`
	PV       string `yaml:"pv"`
}

func Load(filename string) (*Book, error) {
	var book Book

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("'%s': %v", filename, err)
	}

	if err := yaml.Unmarshal(b, &book.Positions); err != nil {
		return nil, err
	}

	return &book, nil
}
