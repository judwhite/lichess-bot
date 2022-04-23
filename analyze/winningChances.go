package analyze

import "math"

func rawWinningChances(cp float64) float64 {
	return 2/(1+math.Exp(-0.004*cp)) - 1
}

func cpWinningChances(cp int) float64 {
	return rawWinningChances(math.Min(math.Max(-1000, float64(cp)), 1000))
}

func mateWinningChances(mate int) float64 {
	cp := (21 - math.Min(10, math.Abs(float64(mate)))) * 100
	signed := cp
	if mate < 0 {
		signed *= -1
	}
	return rawWinningChances(signed)
}

func evalWinningChances(eval Eval) float64 {
	if eval.Mate != 0 {
		return mateWinningChances(eval.Mate)
	}
	return cpWinningChances(eval.CP)
}

// povChances computes winning chances for a color
// 1  infinitely winning
// -1 infinitely losing
func povChances(color int, eval Eval) float64 {
	chances := evalWinningChances(eval)
	switch color {
	case 0:
		return chances
	default:
		return -chances
	}
}

// povDiff computes the difference, in winning chances, between two evaluations
// 1  = e1 is infinitely better than e2
// -1 = e1 is infinitely worse  than e2
func povDiff(color int, e2 Eval, e1 Eval) float64 {
	return povChances(color, e2) - povChances(color, e1)
}
