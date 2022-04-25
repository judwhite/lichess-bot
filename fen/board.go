package fen

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
)

const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

type Board struct {
	Pos             [64]byte
	ActiveColor     Color
	Castling        [4]bool
	EnPassantSquare int
	HalfmoveClock   int
	FullMove        int
}

type Color int

func (c Color) String() string {
	if c == WhitePieces {
		return "w"
	} else if c == BlackPieces {
		return "b"
	}
	return "?"
}

const (
	WhitePieces Color = 1
	BlackPieces Color = -1
)

type nav struct {
	file int
	rank int
}

var (
	knightPaths = []nav{
		{file: -1, rank: 2},
		{file: 1, rank: 2},
		{file: -1, rank: -2},
		{file: 1, rank: -2},

		{file: -2, rank: 1},
		{file: 2, rank: 1},
		{file: -2, rank: -1},
		{file: 2, rank: -1},
	}

	bishopPaths = []nav{
		{file: -1, rank: -1},
		{file: 1, rank: -1},
		{file: -1, rank: 1},
		{file: 1, rank: 1},
	}

	rookPaths = []nav{
		{file: -1, rank: 0},
		{file: 1, rank: 0},
		{file: 0, rank: -1},
		{file: 0, rank: 1},
	}

	kingPaths = []nav{
		{file: -1, rank: 0},
		{file: -1, rank: -1},
		{file: -1, rank: 1},
		{file: 1, rank: 0},
		{file: 1, rank: -1},
		{file: 1, rank: 1},
		{file: 0, rank: -1},
		{file: 0, rank: 1},
	}
)

func (b *Board) FENNoMoveClocks() string {
	var fen strings.Builder
	for i := 0; i < 8; i++ {
		if i != 0 {
			fen.WriteByte('/')
		}

		offset := i * 8
		blanks := 0

		for j := 0; j < 8; j++ {
			if b.Pos[offset+j] == ' ' {
				blanks++
				continue
			}

			if blanks != 0 {
				fen.WriteByte('0' + byte(blanks))
				blanks = 0
			}

			fen.WriteByte(b.Pos[offset+j])
		}

		if blanks != 0 {
			fen.WriteByte('0' + byte(blanks))
			blanks = 0
		}
	}

	// active color
	if b.ActiveColor == WhitePieces {
		fen.WriteString(" w ")
	} else {
		fen.WriteString(" b ")
	}

	// castling
	var anyCastling bool
	for i := 0; i < 4; i++ {
		if b.Castling[i] {
			fen.WriteByte(fenCastlingMap[i])
			anyCastling = true
		}
	}
	if !anyCastling {
		fen.WriteByte('-')
	}
	fen.WriteByte(' ')

	// en passant target square
	fen.WriteString(indexToSquare(b.EnPassantSquare))

	return fen.String()
}

func (b *Board) FEN() string {
	return fmt.Sprintf("%s %d %d", b.FENNoMoveClocks(), b.HalfmoveClock, b.FullMove)
}

func (b *Board) UCItoSAN(move string) string {
	fromUCI := move[:2]
	toUCI := move[2:4]
	var promote string
	if len(move) > 4 {
		promote = strings.ToUpper(string(move[4]))
	}

	from, to := uciToIndex(fromUCI), uciToIndex(toUCI)
	piece := b.Pos[from]
	isCapture := b.Pos[to] != ' '

	piece = upper(piece)

	if to == b.EnPassantSquare && piece == 'P' {
		isCapture = true
	}

	var san string

	if piece == 'K' {
		switch move {
		// white
		case "e1g1":
			san = "O-O"
		case "e1c1":
			san = "O-O-O"
			// black
		case "e8g8":
			san = "O-O"
		case "e8c8":
			san = "O-O-O"
		}
	}

	if san == "" { // not castling
		if piece != 'P' {
			san += string(piece)
		}

		legalMoves := b.legalMoves()
		var otherSources []string
		for i := 0; i < len(legalMoves); i++ {
			// check source squares are different
			otherFrom := legalMoves[i].from
			if from == otherFrom {
				continue
			}

			// same destination?
			otherTo := legalMoves[i].to
			if to != otherTo {
				continue
			}

			// same type of piece?
			if b.Pos[from] != b.Pos[otherFrom] {
				continue
			}

			otherSources = append(otherSources, indexToSquare(otherFrom))
		}

		if len(otherSources) > 0 && piece != 'P' {
			var sameFile, sameRank bool
			for _, otherFrom := range otherSources {
				if fromUCI[0] == otherFrom[0] {
					sameFile = true
				}
				if fromUCI[1] == otherFrom[1] {
					sameRank = true
				}
			}

			if !sameFile {
				san += string(fromUCI[0])
			} else {
				if !sameRank {
					san += string(fromUCI[1])
				} else {
					san += fromUCI
				}
			}
		}

		if isCapture {
			if piece == 'P' {
				san += string(move[0])
			}
			san += "x"
		}
		san += toUCI
		if promote != "" {
			san += "=" + promote
		}
	}

	newBoard := b.Clone()
	newBoard.Moves(move)

	if newBoard.IsCheck() {
		if newBoard.IsMate() {
			san += "#"
		} else {
			san += "+"
		}
	}

	return san
}

func (b *Board) checkMoveNotCheck(from, to int) bool {
	uci := indexesToUCI(from, to)
	activeColor := b.ActiveColor
	newBoard := b.Clone()
	newBoard.Moves(uci)
	newBoard.ActiveColor = activeColor
	return !newBoard.IsCheck()
}

func (b *Board) Moves(moves ...string) *Board {
	if len(moves) == 0 {
		return b
	}

	halfMoveClock := b.HalfmoveClock
	fullMove := b.FullMove

	activeColor := b.ActiveColor

	wk := b.Castling[0]
	wq := b.Castling[1]
	bk := b.Castling[2]
	bq := b.Castling[3]

	for moveIdx, move := range moves {
		if activeColor == BlackPieces {
			activeColor = WhitePieces
			fullMove++
		} else {
			activeColor = BlackPieces
		}

		if len(move) < 4 {
			panic(fmt.Errorf("UCI move '%s' is invalid, index=%d, len=%d", move, moveIdx, len(moves)))
		}

		fromUCI := move[:2]
		toUCI := move[2:4]
		var promote byte
		if len(move) > 4 {
			promote = upper(move[4])
		}

		// castling privileges
		if fromUCI == "a1" || toUCI == "a1" {
			wq = false
		} else if fromUCI == "h1" || toUCI == "h1" {
			wk = false
		} else if fromUCI == "a8" || toUCI == "a8" {
			bq = false
		} else if fromUCI == "h8" || toUCI == "h8" {
			bk = false
		} else if fromUCI == "e1" {
			wk, wq = false, false
		} else if fromUCI == "e8" {
			bk, bq = false, false
		}

		from, to := uciToIndex(fromUCI), uciToIndex(toUCI)
		piece := b.Pos[from]

		isCapture := b.Pos[to] != ' '
		b.Pos[to] = b.Pos[from]
		b.Pos[from] = ' '

		if to == b.EnPassantSquare && (piece == 'P' || piece == 'p') {
			var captureOn int
			if activeColor == WhitePieces {
				captureOn = to - 8 // next move is white's, so the target is in black's position
			} else {
				captureOn = to + 8
			}
			b.Pos[captureOn] = ' '
			isCapture = true
		}

		// set halfmove clock and en passant square
		b.EnPassantSquare = -1
		if piece == 'P' || piece == 'p' {
			halfMoveClock = 0
			if abs(to-from) == 16 {
				var internalFile int
				if activeColor == WhitePieces {
					internalFile = 2 // next move is white's, so the target is in black's position
				} else {
					internalFile = 5
				}
				b.EnPassantSquare = to%8 + internalFile*8
			}
		} else {
			if isCapture {
				halfMoveClock = 0
			} else {
				halfMoveClock++
			}
		}

		// promotion
		if promote != 0 {
			if activeColor == WhitePieces { // next move is white's, so black promotes
				b.Pos[to] = lower(promote)
			} else {
				b.Pos[to] = upper(promote)
			}
		}

		// white king castle
		if piece == 'K' && fromUCI == "e1" {
			if toUCI == "g1" {
				// king side
				b.Pos[to+1] = ' '
				b.Pos[to-1] = 'R'
			} else if toUCI == "c1" {
				// queen side
				b.Pos[to-2] = ' '
				b.Pos[to+1] = 'R'
			}
		}

		// black king castle
		if piece == 'k' && fromUCI == "e8" {
			if toUCI == "g8" {
				// king side
				b.Pos[to+1] = ' '
				b.Pos[to-1] = 'r'
			} else if toUCI == "c8" {
				// queen side
				b.Pos[to-2] = ' '
				b.Pos[to+1] = 'r'
			}
		}
	}

	b.ActiveColor = activeColor

	// castling
	b.Castling[0] = wk
	b.Castling[1] = wq
	b.Castling[2] = bk
	b.Castling[3] = bq

	// NOTE: en passant target square handling per move

	b.HalfmoveClock = halfMoveClock
	b.FullMove = fullMove

	return b
}

func FENtoBoard(fen string) Board {
	if fen == "" {
		fen = startPosFEN
	}
	parts := strings.Split(fen, " ")
	ranks := strings.Split(parts[0], "/")

	if len(parts) < 6 {
		if len(parts) < 5 {
			parts = append(parts, "0")
		}
		parts = append(parts, "1")
	}

	// active color
	var activeColor Color
	if parts[1] == "w" {
		activeColor = WhitePieces
	} else if parts[1] == "b" {
		activeColor = BlackPieces
	} else {
		log.Fatalf("active color '%s' is invalid", parts[1])
	}

	var wk, wq, bk, bq bool
	for _, c := range parts[2] {
		switch c {
		case 'K':
			wk = true
		case 'Q':
			wq = true
		case 'k':
			bk = true
		case 'q':
			bq = true
		}
	}

	// en passant target square
	epSquare := -1
	if parts[3] != "-" {
		epSquare = uciToIndex(parts[3])
	}

	b := Board{
		ActiveColor:     activeColor,
		Castling:        [4]bool{wk, wq, bk, bq},
		EnPassantSquare: epSquare,
		HalfmoveClock:   atoi(parts[4]),
		FullMove:        atoi(parts[5]),
	}

	for i := 7; i >= 0; i-- {
		rank := []byte(ranks[i])
		offset := i * 8
		for _, c := range rank {
			if isDigit(c) {
				n := int(c) - 48
				for j := 0; j < n; j++ {
					b.Pos[offset] = ' '
					offset++
				}
			} else {
				b.Pos[offset] = c
				offset++
			}
		}
	}

	return b
}

func uciToIndex(uci string) int {
	file := int(uci[0]) - 'a'
	rank := int(uci[1]) - '0' - 1
	idx := (7-rank)*8 + file
	return idx
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatal(err)
	}
	return n
}

func (b *Board) IsCheck() bool {
	var (
		ourKing     byte
		enemyQueen  byte
		enemyRook   byte
		enemyBishop byte
		enemyKnight byte
		enemyPawn   byte
		enemyKing   byte
	)

	var white bool
	if b.ActiveColor == WhitePieces {
		ourKing = 'K'
		enemyKing, enemyQueen, enemyRook, enemyBishop, enemyKnight, enemyPawn = 'k', 'q', 'r', 'b', 'n', 'p'
		white = true
	} else {
		ourKing = 'k'
		enemyKing, enemyQueen, enemyRook, enemyBishop, enemyKnight, enemyPawn = 'K', 'Q', 'R', 'B', 'N', 'P'
	}

	// find the king
	kingIndex := -1
	for i := 0; i < 64; i++ {
		if b.Pos[i] == ourKing {
			kingIndex = i
			break
		}
	}

	kingRank := kingIndex / 8
	kingFile := kingIndex % 8

	// R = same rank or file
	// B = same diagonal (r +/- n, c +/- n)
	// Q = same rank, file, or diagonal
	// P = diagonal and king is one rank ahead (black king has lower index, white king has higher index)
	// N = (r +/- 2, c +/- 1) and (r +/- 1, c +/- 2)

	// pawns
	var pawnRank int
	if white {
		pawnRank = kingRank - 1
	} else {
		pawnRank = kingRank + 1
	}

	if pawnRank >= 0 && pawnRank < 8 {
		pawnFiles := []int{kingFile - 1, kingFile + 1}
		for _, pawnFile := range pawnFiles {
			if pawnFile < 0 || pawnFile >= 8 {
				continue
			}

			idx := pawnRank*8 + pawnFile
			if b.Pos[idx] == enemyPawn {
				// check by pawn
				return true
			}
		}
	}

	// bishops and queens
	for _, path := range bishopPaths {
		file, rank := kingFile+path.file, kingRank+path.rank
		for file >= 0 && file < 8 && rank >= 0 && rank < 8 {
			idx := rank*8 + file
			if b.Pos[idx] == ' ' {
				file += path.file
				rank += path.rank
				continue
			}

			if b.Pos[idx] == enemyBishop || b.Pos[idx] == enemyQueen {
				// check by bishop or queen
				return true
			}

			break
		}
	}

	// rooks and queens
	for _, path := range rookPaths {
		file, rank := kingFile+path.file, kingRank+path.rank
		for file >= 0 && file < 8 && rank >= 0 && rank < 8 {
			idx := rank*8 + file
			if b.Pos[idx] == ' ' {
				file += path.file
				rank += path.rank
				continue
			}

			if b.Pos[idx] == enemyRook || b.Pos[idx] == enemyQueen {
				// check by rook or queen
				return true
			}

			break
		}
	}

	// knights
	for _, path := range knightPaths {
		file, rank := kingFile+path.file, kingRank+path.rank
		if file < 0 || file >= 8 || rank < 0 || rank >= 8 {
			continue
		}

		idx := rank*8 + file
		if b.Pos[idx] == enemyKnight {
			// check by knight
			return true
		}
	}

	// enemy king
	for _, path := range kingPaths {
		file, rank := kingFile+path.file, kingRank+path.rank
		if file < 0 || file >= 8 || rank < 0 || rank >= 8 {
			continue
		}

		idx := rank*8 + file
		if b.Pos[idx] == enemyKing {
			// check by enemy king (used when evaluating if a king move is legal and mate-checks)
			return true
		}
	}

	return false
}

func (b *Board) IsMate() bool {
	if !b.IsCheck() {
		return false
	}

	return len(b.LegalMoves()) == 0
}

func indexesToUCI(from, to int) string {
	fromFile := byte('a' + from%8)
	fromRank := byte('8' - from/8)
	toFile := byte('a' + to%8)
	toRank := byte('8' - to/8)
	return string([]byte{fromFile, fromRank, toFile, toRank})
}

func indexToSquare(index int) string {
	if index == -1 {
		return "-"
	}

	file := byte('a' + index%8)
	rank := byte('8' - index/8)
	return string([]byte{file, rank})
}

func indexToRankFile(index int) (int, int) {
	file := index % 8
	rank := index / 8
	return rank, file
}

func (b *Board) Clone() *Board {
	newBoard := Board{
		Pos:             b.Pos,
		ActiveColor:     b.ActiveColor,
		Castling:        b.Castling,
		EnPassantSquare: b.EnPassantSquare,
		HalfmoveClock:   b.HalfmoveClock,
		FullMove:        b.FullMove,
	}
	return &newBoard
}

type LegalMove struct {
	SAN string
	UCI string
}

type legalMove struct {
	from int
	to   int
}

func (b *Board) LegalMoves() []LegalMove {
	moves := b.legalMoves()
	sanMoves := make([]LegalMove, 0, len(moves)+4)
	for _, m := range moves {
		uci := indexToSquare(m.from) + indexToSquare(m.to)
		p := b.Pos[m.from]
		if (p == 'p' && m.to >= 56) || (p == 'P' && m.to < 8) {
			for _, promote := range []string{"n", "b", "r", "q"} {
				promoteUCI := uci + promote
				sanMoves = append(sanMoves, LegalMove{UCI: promoteUCI, SAN: b.UCItoSAN(promoteUCI)})
			}
		} else {
			sanMoves = append(sanMoves, LegalMove{UCI: uci, SAN: b.UCItoSAN(uci)})
		}
	}

	return sanMoves
}

func (b *Board) legalMoves() []legalMove {
	var king, queen, bishop, knight, rook, pawn byte
	if b.ActiveColor == WhitePieces {
		king, queen, bishop, knight, rook, pawn = 'K', 'Q', 'B', 'N', 'R', 'P'
	} else {
		king, queen, bishop, knight, rook, pawn = 'k', 'q', 'b', 'n', 'r', 'p'
	}

	var moves []legalMove

	for i := 0; i < 64; i++ {
		var pieceMoves []int
		switch b.Pos[i] {
		case king:
			pieceMoves = b.kingMoves(i)
		case queen:
			pieceMoves = b.queenMoves(i)
		case bishop:
			pieceMoves = b.bishopMoves(i)
		case knight:
			pieceMoves = b.knightMoves(i)
		case rook:
			pieceMoves = b.rookMoves(i)
		case pawn:
			pieceMoves = b.pawnMoves(i)
		default:
			continue
		}

		for _, pieceMove := range pieceMoves {
			moves = append(moves, legalMove{from: i, to: pieceMove})
		}
	}

	return moves
}

func (b *Board) isEnemyPiece(p byte) bool {
	var king, queen, bishop, knight, rook, pawn byte
	if b.ActiveColor == WhitePieces {
		king, queen, bishop, knight, rook, pawn = 'k', 'q', 'b', 'n', 'r', 'p'
	} else {
		king, queen, bishop, knight, rook, pawn = 'K', 'Q', 'B', 'N', 'R', 'P'
	}

	return p == king || p == queen || p == bishop || p == knight || p == rook || p == pawn
}

var (
	whiteShortCastle    = [3]byte{' ', ' ', 'R'}
	whiteLongCastle     = [4]byte{'R', ' ', ' ', ' '}
	blackShortCastle    = [3]byte{' ', ' ', 'r'}
	blackLongCastle     = [4]byte{'r', ' ', ' ', ' '}
	whiteKingStartIndex int
	blackKingStartIndex int
	fenCastlingMap      = [4]byte{'K', 'Q', 'k', 'q'}
)

func init() {
	whiteKingStartIndex = uciToIndex("e1")
	blackKingStartIndex = uciToIndex("e8")
}

func (b *Board) kingMoves(idx int) []int {
	var moves []int

	startRank, startFile := indexToRankFile(idx)

	// the 8 1-move diagonal positions
	for _, path := range kingPaths {
		rank, file := startRank+path.rank, startFile+path.file
		if rank < 0 || rank > 7 || file < 0 || file > 7 {
			continue
		}

		i := rank*8 + file
		p := b.Pos[i]

		if p == ' ' || b.isEnemyPiece(p) {
			moves = append(moves, i)
			continue
		}
	}

	// castling options
	var canCastleShort, canCastleLong bool
	var castleShortPattern [3]byte
	var castleLongPattern [4]byte
	var fileOffset int

	if b.ActiveColor == WhitePieces && idx == whiteKingStartIndex {
		fileOffset = 56
		canCastleShort, canCastleLong = b.Castling[0], b.Castling[1]
		castleShortPattern = whiteShortCastle
		castleLongPattern = whiteLongCastle
	} else if b.ActiveColor == BlackPieces && idx == blackKingStartIndex {
		fileOffset = 0
		canCastleShort, canCastleLong = b.Castling[2], b.Castling[3]
		castleShortPattern = blackShortCastle
		castleLongPattern = blackLongCastle
	}

	canCastleLong = canCastleLong && bytes.Equal(b.Pos[fileOffset:fileOffset+4], castleLongPattern[:])
	canCastleShort = canCastleShort && bytes.Equal(b.Pos[fileOffset+5:fileOffset+8], castleShortPattern[:])

	if (canCastleShort || canCastleLong) && b.IsCheck() {
		canCastleShort, canCastleLong = false, false
	}

	if canCastleShort {
		toIndex := idx + 2
		inbetweenSquare := toIndex - 1
		if b.checkMoveNotCheck(idx, inbetweenSquare) {
			moves = append(moves, toIndex)
		}
	}

	if canCastleLong {
		toIndex := idx - 2
		inbetweenSquare := toIndex + 1
		if b.checkMoveNotCheck(idx, inbetweenSquare) {
			moves = append(moves, toIndex)
		}
	}

	for i := 0; i < len(moves); i++ {
		if !b.checkMoveNotCheck(idx, moves[i]) {
			moves = append(moves[:i], moves[i+1:]...)
			i--
		}
	}

	return moves
}

func (b *Board) queenMoves(idx int) []int {
	return b.pathMoves(idx, kingPaths)
}

func (b *Board) bishopMoves(idx int) []int {
	return b.pathMoves(idx, bishopPaths)
}

func (b *Board) rookMoves(idx int) []int {
	return b.pathMoves(idx, rookPaths)
}

func (b *Board) knightMoves(idx int) []int {
	var moves []int

	startRank, startFile := indexToRankFile(idx)

	// the 8 1-move diagonal positions
	for _, path := range knightPaths {
		rank, file := startRank+path.rank, startFile+path.file
		if rank < 0 || rank > 7 || file < 0 || file > 7 {
			continue
		}

		i := rank*8 + file
		p := b.Pos[i]

		if p == ' ' || b.isEnemyPiece(p) {
			moves = append(moves, i)
			continue
		}
	}

	// make sure move doesn't put us in check
	for i := 0; i < len(moves); i++ {
		if !b.checkMoveNotCheck(idx, moves[i]) {
			moves = append(moves[:i], moves[i+1:]...)
			i--
		}
	}

	return moves
}

func (b *Board) pawnMoves(idx int) []int {
	var moves []int

	var direction, homeRank int
	if b.ActiveColor == WhitePieces {
		direction = -1
		homeRank = 6
	} else {
		direction = 1
		homeRank = 1
	}

	startRank, startFile := indexToRankFile(idx)

	// one or two squares
	rank := startRank + direction
	oneSquareIndex := rank*8 + startFile

	// TODO: debug code
	if oneSquareIndex < 0 || oneSquareIndex >= len(b.Pos) {
		fmt.Printf("FEN: \"%s\" idx: %d rank: %d startRank: %d direction: %d startFile: %d oneSquareIndex: %d\n",
			b.FEN(), idx, rank, startRank, direction, startFile, oneSquareIndex)
	}

	if b.Pos[oneSquareIndex] == ' ' {
		moves = append(moves, oneSquareIndex)

		if startRank == homeRank {
			rank = startRank + direction*2
			twoSquareIndex := rank*8 + startFile
			if b.Pos[twoSquareIndex] == ' ' {
				moves = append(moves, twoSquareIndex)
			}
		}
	}

	// captures
	rank = startRank + direction
	enPassantIndex := b.EnPassantSquare

	for _, fileChange := range []int{-1, 1} {
		file := startFile + fileChange
		if file < 0 || file > 7 {
			continue
		}

		i := rank*8 + file
		p := b.Pos[i]

		if b.isEnemyPiece(p) || enPassantIndex == i {
			moves = append(moves, i)
		}
	}

	// make sure move doesn't put us in check
	for i := 0; i < len(moves); i++ {
		if !b.checkMoveNotCheck(idx, moves[i]) {
			moves = append(moves[:i], moves[i+1:]...)
			i--
		}
	}

	return moves
}

func (b *Board) pathMoves(idx int, paths []nav) []int {
	var moves []int

	startRank, startFile := indexToRankFile(idx)

	// check paths
	for _, path := range paths {
		rank, file := startRank+path.rank, startFile+path.file
		for rank >= 0 && rank < 8 && file >= 0 && file < 8 {
			i := rank*8 + file
			p := b.Pos[i]

			if b.isEnemyPiece(p) {
				moves = append(moves, i)
				break
			}

			if p != ' ' {
				break
			}

			moves = append(moves, i)
			rank += path.rank
			file += path.file
		}
	}

	// make sure move doesn't put us in check
	for i := 0; i < len(moves); i++ {
		if !b.checkMoveNotCheck(idx, moves[i]) {
			moves = append(moves[:i], moves[i+1:]...)
			i--
		}
	}

	return moves
}

func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
