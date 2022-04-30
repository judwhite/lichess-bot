package analyze

import (
	"fmt"
	"log"
	"strings"

	"trollfish-lichess/fen"
)

type Evals []Eval

type Eval struct {
	UCIMove    string   `json:"uci"`
	Depth      int      `json:"depth"`
	SelDepth   int      `json:"seldepth"`
	MultiPV    int      `json:"multipv"`
	CP         int      `json:"cp"`
	Mate       int      `json:"mate"`
	Nodes      int      `json:"nodes"`
	NPS        int      `json:"nps"`
	TBHits     int      `json:"tbhits"`
	Time       int      `json:"time"`
	UpperBound bool     `json:"ub,omitempty"`
	LowerBound bool     `json:"lb,omitempty"`
	PV         []string `json:"pv"`
	Mated      bool     `json:"mated,omitempty"`
	Raw        string   `json:"-"`
	DepthDelta int      `json:"delta"`
}

func (e Eval) Score() int {
	if e.Mate > 0 {
		return 400_00 - e.Mate*100 // closer mates equal higher numbers
	} else if e.Mate < 0 {
		return -300_00 + e.Mate*100 // mates further away equal more negative numbers
	}

	return e.CP
}

func (e Eval) Empty() bool {
	return e.UCIMove == ""
}

func (e Eval) GlobalCP(color fen.Color) int {
	return e.CP * int(color)
}

func (e Eval) GlobalMate(color fen.Color) int {
	return e.Mate * int(color)
}

func (e Eval) String(color fen.Color) string {
	if e.Mated {
		return ""
	}

	if e.Mate != 0 {
		return fmt.Sprintf("#%d", e.GlobalMate(color))
	}

	s := fmt.Sprintf("%.2f", float64(e.GlobalCP(color))/100)

	if s == "+0.00" || s == "-0.00" {
		return "0.00"
	}

	return s
}

func parseEval(line string) Eval {
	parts := strings.Split(line, " ")
	var eval Eval

scoreLoop:
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		inc := 1
		switch p {
		case "depth":
			eval.Depth = atoi(parts[i+1])
		case "seldepth":
			eval.SelDepth = atoi(parts[i+1])
		case "multipv":
			eval.MultiPV = atoi(parts[i+1])
		case "score":
			p2 := parts[i+1]
			switch p2 {
			case "cp":
				eval.CP = atoi(parts[i+2])
				inc++
			case "mate":
				eval.Mate = atoi(parts[i+2])
				inc++
			default:
				log.Fatalf("unhandled: 'info ... score %s'", p2)
			}
		case "upperbound":
			eval.UpperBound = true
			inc = 0
		case "lowerbound":
			eval.LowerBound = true
			inc = 0
		case "nodes":
			eval.Nodes = atoi(parts[i+1])
		case "nps":
			eval.NPS = atoi(parts[i+1])
		case "tbhits":
			eval.TBHits = atoi(parts[i+1])
		case "time":
			eval.Time = atoi(parts[i+1])
		case "hashfull":
			// ignore
		case "pv":
			pvMoves := parts[i+1:]
			eval.PV = pvMoves
			eval.UCIMove = pvMoves[0]
			break scoreLoop
		default:
			log.Fatalf("unhandled: 'info ... %s'", p)
		}

		i += inc
	}

	return eval
}
