package polyglot

import "trollfish-lichess/fen"

const castleKeyOffset = 768
const enPassantKeyOffset = 772
const turnKeyOffset = 780

func Key(board fen.Board) uint64 {
	piece := pieceKey(board.Pos)
	castle := castleKey(board.Castling)
	ep := enPassantKey(board.Pos, board.EnPassantSquare)
	turn := turnKey(board.ActivePlayer())

	return piece ^ castle ^ ep ^ turn
}

func pieceKey(pos []rune) uint64 {
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

func castleKey(castling string) uint64 {
	var key uint64

	for _, r := range castling {
		switch r {
		case 'K':
			key ^= pgTable[castleKeyOffset]
		case 'Q':
			key ^= pgTable[castleKeyOffset+1]
		case 'k':
			key ^= pgTable[castleKeyOffset+2]
		case 'q':
			key ^= pgTable[castleKeyOffset+3]
		}
	}

	return key
}

func enPassantKey(pos []rune, square string) uint64 {
	if square == "" || square == "-" {
		return 0
	}

	idx := uciToIndex(square)
	file := int(square[0]) - 'a'
	var flag bool
	var enemyPiece rune
	if square[1] == '6' {
		// black pushed
		idx += 8
		enemyPiece = 'P'
	} else {
		// white pushed
		idx -= 8
		enemyPiece = 'p'
	}

	if file != 0 {
		if pos[idx-1] == enemyPiece {
			flag = true
		}
	}
	if file != 7 {
		if pos[idx+1] == enemyPiece {
			flag = true
		}
	}

	if !flag {
		return 0
	}

	return pgTable[enPassantKeyOffset+file]
}

func turnKey(color fen.Color) uint64 {
	if color == fen.BlackPieces {
		return 0
	}

	return pgTable[turnKeyOffset]
}

func uciToIndex(uci string) int {
	file := int(uci[0]) - 'a'
	rank := int(uci[1]) - '0' - 1
	idx := (7-rank)*8 + file
	return idx
}
