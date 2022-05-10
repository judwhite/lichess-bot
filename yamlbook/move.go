package yamlbook

import (
	"fmt"
	"strings"

	"trollfish-lichess/fen"
)

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

func NewMove(boardFEN string, move Move) *Move {
	return &Move{
		Move:   move.Move,
		Weight: move.Weight,
		CP:     move.CP,
		Mate:   move.Mate,
		TS:     move.TS,
		Engine: move.Engine,
		fen:    boardFEN,
	}
}

func (m *Move) UCI() string {
	if m.uci != "" {
		return m.uci
	}

	if m.fen == "" {
		panic(fmt.Errorf("internal error: fen not set %#v", m))
	}

	if m.Move == "" {
		panic(fmt.Errorf("internal error: m.Move is '', key: '%s' m: %#v", m.fen, m))
	}

	uci, err := fen.FENtoBoard(m.fen).SANtoUCI(m.Move)
	if err != nil {
		panic(err)
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
		panic(fmt.Errorf("internal error: fen not set %#v", m))
	}

	return m.fen
}
