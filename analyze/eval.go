package analyze

import (
	"fmt"
	"log"
	"strings"
	"time"

	"trollfish-lichess/commas"
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
	HashFull   int      `json:"hashfull"`
	TBHits     int      `json:"tbhits"`
	Time       int      `json:"time"`
	UpperBound bool     `json:"ub,omitempty"`
	LowerBound bool     `json:"lb,omitempty"`
	PV         []string `json:"pv"`
	Mated      bool     `json:"mated,omitempty"`
	Raw        string   `json:"-"`
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

func (e Eval) AsLog(fenPos string) string {
	b := fen.FENtoBoard(fenPos)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("info depth %3d seldepth %3d multipv %3d score ", e.Depth, e.SelDepth, e.MultiPV))
	if e.Mate == 0 {
		sb.WriteString(fmt.Sprintf("cp   %6d ", e.CP))
	} else {
		sb.WriteString(fmt.Sprintf("mate %6d ", e.Mate))
	}
	t := time.Duration(e.Time) * time.Millisecond
	if t >= 5*time.Second {
		t = t.Round(time.Second)
	} else if t >= 1*time.Second {
		t = t.Round(100 * time.Millisecond)
	}
	sb.WriteString(fmt.Sprintf(" nodes %14s  nps %10s  hashfull %5.1f%%  tbhits %10s  time %7v  pv ", commas.Int(e.Nodes), commas.Int(e.NPS), float64(e.HashFull)/10, commas.Int(e.TBHits), t))
	sb.WriteString(fmt.Sprintf("%6s", b.UCItoSAN(e.PV[0])))
	maxMoves := min(9, len(e.PV))
	for i := 1; i < maxMoves; i++ {
		b.Moves(e.PV[i-1])
		sb.WriteString(fmt.Sprintf(" %6s", b.UCItoSAN(e.PV[i])))

		if i == maxMoves-1 && len(e.PV) > maxMoves {
			sb.WriteString(" ...")
		}
	}

	return sb.String()
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
	eval := Eval{Raw: line}

	parts := strings.Split(line, " ")

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
		case "hashfull":
			eval.HashFull = atoi(parts[i+1])
		case "tbhits":
			eval.TBHits = atoi(parts[i+1])
		case "time":
			eval.Time = atoi(parts[i+1])
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
