package polyglot

import "trollfish-lichess/fen"

const castleKeyOffset = 768
const enPassantKeyOffset = 772
const turnKeyOffset = 780

func Key(board *fen.Board) uint64 {
	piece := pieceKey(board.Pos)
	castle := castleKey(board.Castling)
	ep := enPassantKey(board.Pos, board.EnPassantSquare)
	turn := turnKey(board.ActiveColor)

	return piece ^ castle ^ ep ^ turn
}

func pieceKey(pos [64]byte) uint64 {
	var key uint64

	for i, p := range pos {
		// our board has black at index=0, so we flip it
		file := i % 8
		rank := 7 - (i / 8)

		if p == ' ' {
			continue
		}

		piece := pieceToInt(p)
		idx := 64*piece + 8*rank + file
		key ^= pgTable[idx]
	}

	return key
}

func castleKey(castling [4]bool) uint64 {
	var key uint64

	if castling[0] { // K
		key ^= pgTable[castleKeyOffset]
	}
	if castling[1] { // Q
		key ^= pgTable[castleKeyOffset+1]
	}
	if castling[2] { // k
		key ^= pgTable[castleKeyOffset+2]
	}
	if castling[3] { // q
		key ^= pgTable[castleKeyOffset+3]
	}

	return key
}

func enPassantKey(pos [64]byte, idx int) uint64 {
	if idx == -1 {
		return 0
	}

	humanRank, humanFile := indexToHumanRankFile(idx)
	var flag bool
	var enemyPiece byte
	if humanRank == 6 {
		// black pushed
		idx += 8
		enemyPiece = 'P'
	} else {
		// white pushed
		idx -= 8
		enemyPiece = 'p'
	}

	if humanFile != 1 {
		if pos[idx-1] == enemyPiece {
			flag = true
		}
	}
	if humanFile != 8 {
		if pos[idx+1] == enemyPiece {
			flag = true
		}
	}

	if !flag {
		return 0
	}

	return pgTable[enPassantKeyOffset+humanFile-1]
}

func turnKey(color fen.Color) uint64 {
	if color == fen.BlackPieces {
		return 0
	}

	return pgTable[turnKeyOffset]
}

func indexToHumanRankFile(index int) (int, int) {
	file := (index % 8) + 1
	rank := 8 - (index / 8)
	return rank, file
}
