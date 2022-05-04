package main

import (
	"fmt"
	"sort"

	"trollfish-lichess/fen"
)

func Busted(filename string, color fen.Color) (map[string]MoveChances, error) {
	db, err := fen.LoadPGNDatabase(filename)
	if err != nil {
		return nil, err
	}

	m1, err := busted(db, color)
	if err != nil {
		return nil, err
	}

	return m1, nil
}

func busted(db fen.Database, color fen.Color) (map[string]MoveChances, error) {
	var games []*fen.PGNGame

	var winResult, loseResult fen.GameResult
	var startPly int
	if color == fen.WhitePieces {
		winResult, loseResult = fen.WhiteWon, fen.BlackWon
		startPly = 0
	} else {
		winResult, loseResult = fen.BlackWon, fen.WhiteWon
		startPly = 1
	}

	for _, game := range db.Games {
		if game.Result != winResult {
			continue
		}

		/*if color == fen.WhitePieces {
			if !strings.EqualFold(game.White, playerName) {
				continue
			}
		} else {
			if !strings.EqualFold(game.Black, playerName) {
				continue
			}
		}*/

		termination := game.Tags["Termination"]
		if termination == "Time forfeit" || termination == "Rules infraction" || termination == "Abandoned" {
			continue
		}

		if len(game.Moves) < 5 || len(game.Moves) > 100 {
			continue
		}

		games = append(games, game)
	}

	// given a position, find the move with the "best results"
	// how to count/weigh opportunities to branch into a win?

	m := make(map[string]MoveChances)
	for _, game := range games {
		for i := startPly; i < len(game.Moves); i += 2 {
			move := game.Moves[i]

			fenKey := move.FENKey
			moveUCI := move.UCI

			var moveChance *MoveChance
			for _, test := range m[fenKey] {
				if test.MoveUCI == moveUCI {
					moveChance = test
					break
				}
			}

			if moveChance == nil {
				moveChance = &MoveChance{MoveUCI: moveUCI}

				b := fen.FENtoBoard(fenKey)
				moveChance.MoveSAN = b.UCItoSAN(moveUCI)
				if len(game.Moves) > i+1 {
					moveChance.PonderUCI = game.Moves[i+1].UCI
				}
				moveChance.GameText = fmt.Sprintf("%s vs %s: %s", game.White, game.Black, game.Tags["Result"])
			}

			if game.Result == winResult {
				moveChance.Win++
			} else if game.Result == loseResult {
				moveChance.Lose++
			} else {
				moveChance.Draw++
			}
			moveChance.Update()

			m[fenKey] = append(m[fenKey], moveChance)
		}
	}

	for k, v := range m {
		sort.Slice(v, func(i, j int) bool {
			if v[i].Win != v[j].Win {
				return v[i].Win > v[j].Win
			}

			return v[i].WinPercent > v[j].WinPercent
		})

		m[k] = v
	}

	return m, nil
}

type MoveChances []*MoveChance

func (mc MoveChances) BestMove() *MoveChance {
	if len(mc) == 0 {
		return nil
	}
	return mc[0]
}

type MoveChance struct {
	MoveUCI     string
	MoveSAN     string
	PonderUCI   string
	Total       int
	Win         int
	Lose        int
	Draw        int
	WinPercent  int
	LosePercent int
	DrawPercent int
	GameText    string
}

func (mc *MoveChance) Update() {
	mc.Total = mc.Win + mc.Draw + mc.Lose
	mc.WinPercent = mc.Win * 100 / mc.Total
	mc.DrawPercent = mc.Draw * 100 / mc.Total
	mc.LosePercent = mc.Lose * 100 / mc.Total
}
