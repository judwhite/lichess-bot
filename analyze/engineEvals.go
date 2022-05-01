package analyze

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"trollfish-lichess/fen"
)

func (a *Analyzer) engineEvals(ctx context.Context, opts AnalysisOptions, fenPos string) []Eval {
	start := time.Now()

	var moves []Eval

	var maxDepth int
	var stopped bool
	var printEngineOutput bool

	//showEngineOutputAfter := 20 * time.Second
	floorDepth := opts.MinDepth - opts.DepthDelta + 1
	ignoreDepthsGreaterThan := 255

	minTimeMS := int(opts.MinTime.Milliseconds())
	timeout := time.NewTimer(opts.MaxTime)
	minNodes := 0

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case line := <-a.output:
			if strings.HasPrefix(line, "bestmove") {
				a.input <- "stop"
				break loop
			}

			if !strings.HasPrefix(line, "info") || !strings.Contains(line, "score") {
				continue
			}

			eval := parseEval(line)

			if eval.UpperBound || eval.LowerBound || eval.Nodes < minNodes || eval.Depth > ignoreDepthsGreaterThan {
				continue
			}

			minNodes = eval.Nodes
			if eval.Depth > maxDepth {
				if printEngineOutput {
					logInfo(fmt.Sprintf("depth = %d", eval.Depth))
				}
				maxDepth = eval.Depth
			}

			// remove evals at same depth + PV[0] with fewer nodes searched
			for i := 0; i < len(moves); i++ {
				if moves[i].Depth == eval.Depth && moves[i].UCIMove == eval.UCIMove {
					if moves[i].Nodes <= eval.Nodes {
						moves = append(moves[:i], moves[i+1:]...)
						i--
						continue
					}
				}
			}

			moves = append(moves, eval)

			sort.Slice(moves, func(i, j int) bool {
				if moves[i].Depth != moves[j].Depth {
					return moves[i].Depth > moves[j].Depth
				}

				if moves[i].MultiPV != moves[j].MultiPV {
					return moves[i].MultiPV < moves[j].MultiPV
				}

				if moves[i].Nodes != moves[j].Nodes {
					return moves[i].Nodes > moves[j].Nodes
				}

				return moves[i].Time > moves[j].Time
			})

			depth1Count, depth2Count := 0, 0
			for i := 0; i < len(moves); i++ {
				if moves[i].Depth == maxDepth {
					depth1Count++
				}
				if moves[i].Depth == maxDepth-1 {
					depth2Count++
				}
			}
			depthComplete := depth1Count == depth2Count

			// see if we've crossed the min-depth threshold
			if depthComplete && eval.Depth >= opts.MinDepth && eval.Time >= minTimeMS && len(moves) > 0 {
				delta := 1
				move := moves[0].UCIMove
				for i := 1; i < len(moves); i++ {
					if moves[i].MultiPV != 1 || moves[i].Depth < floorDepth {
						continue
					}
					if moves[i].UCIMove == move {
						delta++
					} else {
						break
					}
				}

				board := fen.FENtoBoard(fenPos)
				globalCP := eval.GlobalCP(board.ActiveColor)
				globalMate := eval.GlobalMate(board.ActiveColor)
				san := board.UCItoSAN(eval.UCIMove)

				t := fmt.Sprintf("t=%5v/%v", time.Since(start).Round(time.Second), opts.MaxTime)
				if delta >= opts.DepthDelta {
					logInfo(fmt.Sprintf("%s delta %d >= %d @ depth %d. move: %7s %s cp: %d mate: %d", t, delta, opts.DepthDelta, eval.Depth, san, eval.UCIMove, globalCP, globalMate))
					ignoreDepthsGreaterThan = eval.Depth
					a.input <- "stop"
				} else {
					logInfo(fmt.Sprintf("%s delta %d < %d  @ depth %d. move: %7s %s cp: %d mate: %d", t, delta, opts.DepthDelta, eval.Depth, san, eval.UCIMove, globalCP, globalMate))
				}
			}

		case <-timeout.C:
			if maxDepth == 0 {
				return nil
			}
			logInfo(fmt.Sprintf("per-move timeout expired (%v), using what we have at depth %d", opts.MaxTime, maxDepth))
			a.input <- "stop"
			stopped = true
		}
	}

	if !stopped {
		// drain timeout channel
		if !timeout.Stop() {
			<-timeout.C
		}
	}

	// only take depths that have full results for when multipv > 1
	depth1Count, depth2Count := 0, 0

	for i := 0; i < len(moves); i++ {
		if moves[i].Depth == maxDepth {
			depth1Count++
		}
		if moves[i].Depth == maxDepth-1 {
			depth2Count++
		}
	}

	if depth1Count < depth2Count {
		logInfo(fmt.Sprintf("depth: %d depth1Count (%d) < depth2Count (%d), using depth: %d", maxDepth, depth1Count, depth2Count, maxDepth-1))
		maxDepth--
	}

	for i := 0; i < len(moves); i++ {
		if moves[i].Depth > maxDepth {
			moves = append(moves[:i], moves[i+1:]...)
			i--
			continue
		}
	}

	return moves
}
