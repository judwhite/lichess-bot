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

	whiteKingIndex int
	blackKingIndex int
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

	whiteKingStartIndex = 60
	blackKingStartIndex = 4
	a1                  = 56
	a8                  = 0
	c1                  = 58
	c8                  = 2
	g1                  = 62
	g8                  = 6
	h1                  = 63
	h8                  = 7
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

	pawnPaths = []int{-1, 1}
)

func (b Board) String() string {
	var sb strings.Builder
	for i := 0; i < 8; i++ {
		sb.Write(b.Pos[i*8 : (i*8)+8])
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (b Board) FENKey() string {
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

	// en passant target square (modified for domain reduction)
	ep := b.EnPassantSquare
	if ep == -1 {
		fen.WriteByte('-')
	} else {
		enemyPiece := iif[byte](b.ActiveColor == WhitePieces, 'P', 'p')
		offset := iif[int](b.ActiveColor == WhitePieces, 8, -8)

		var flag bool
		file := ep % 8
		if file != 0 && b.Pos[ep+offset-1] == enemyPiece {
			flag = true
		}
		if file != 7 && b.Pos[ep+offset+1] == enemyPiece {
			flag = true
		}

		if !flag {
			fen.WriteByte('-')
		} else {
			fen.WriteString(indexToSquare(b.EnPassantSquare))
		}
	}

	return fen.String()
}

func (b Board) FEN() string {
	return fmt.Sprintf("%s %d %d", b.FENKey(), b.HalfmoveClock, b.FullMove)
}

func Key(fen string) string {
	b := FENtoBoard(fen)
	return b.FENKey()
}

func (b Board) UCItoSAN(move string) string {
	if b.Pos[0] == 0 {
		b.LoadFEN(startPosFEN)
	}

	fromUCI := move[:2]
	toUCI := move[2:4]
	var promote byte
	if len(move) > 4 {
		promote = upper(move[4])
	}

	from, to := uciToIndex(fromUCI), uciToIndex(toUCI)
	piece := b.Pos[from]
	isCapture := b.Pos[to] != ' '
	movedPawn := piece == 'P' || piece == 'p'
	if piece == ' ' {
		panic(fmt.Errorf("there is no piece at %d (%s). move: %s fen: %s", from, fromUCI, move, b.FEN()))
	}

	if to == b.EnPassantSquare && movedPawn {
		isCapture = true
	}

	var san strings.Builder

	if piece == 'K' {
		// white castling
		switch move {
		case "e1g1":
			san.WriteString("O-O")
		case "e1c1":
			san.WriteString("O-O-O")
		}
	} else if piece == 'k' {
		// black castling
		switch move {
		case "e8g8":
			san.WriteString("O-O")
		case "e8c8":
			san.WriteString("O-O-O")
		}
	}

	if san.Len() == 0 { // not castling
		if !movedPawn {
			san.WriteByte(upper(piece))
		}

		legalMoves := b.pieceLegalMoves(piece)
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

		if len(otherSources) > 0 && !movedPawn {
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
				san.WriteByte(fromUCI[0])
			} else {
				if !sameRank {
					san.WriteByte(fromUCI[1])
				} else {
					san.WriteString(fromUCI)
				}
			}
		}

		if isCapture {
			if movedPawn {
				san.WriteByte(move[0])
			}
			san.WriteByte('x')
		}
		san.WriteString(toUCI)
		if promote != 0 {
			san.WriteByte('=')
			san.WriteByte(promote)
		}
	}

	// NOTE: only okay because this isn't a pointer
	b.Moves(move)

	if b.IsCheck() {
		if b.IsMate() {
			san.WriteByte('#')
		} else {
			san.WriteByte('+')
		}
	}

	return san.String()
}

func (b Board) SANtoUCI(san string) (string, error) {
	if b.Pos[0] == 0 {
		b.LoadFEN(startPosFEN)
	}

	if len(san) < 2 {
		return "", fmt.Errorf("'%s' is not a valid move in '%s'", san, b.FEN())
	}

	piece := san[0]
	if piece >= 'a' && piece <= 'h' {
		piece = 'P'
	}
	castle := false
	if strings.HasPrefix(san, "O-O") {
		piece = 'K'
		castle = true
	}
	if b.ActiveColor == BlackPieces {
		piece = lower(piece)
	}

	moves := b.PieceLegalMoves(piece)
	for _, move := range moves {
		if !castle && !strings.Contains(san, move.To) {
			continue
		}

		testSAN := b.UCItoSAN(move.UCI)
		if testSAN == san {
			return move.UCI, nil
		}
	}

	return "", fmt.Errorf("'%s' is not a valid move in '%s'", san, b.FEN())
}

func (b Board) checkMoveNotCheck(from, to int) bool {
	uci := indexesToUCI(from, to)
	activeColor := b.ActiveColor
	b.Moves(uci)
	b.ActiveColor = activeColor
	return !b.IsCheck()
}

func (b *Board) Moves(moves ...string) *Board {
	if b.Pos[0] == 0 {
		b.LoadFEN(startPosFEN)
	}

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

		from, to := uciToIndex(fromUCI), uciToIndex(toUCI)
		piece := b.Pos[from]

		isCapture := b.Pos[to] != ' '
		b.Pos[to] = piece
		b.Pos[from] = ' '

		// castling privileges
		if from == a1 || to == a1 {
			wq = false
		} else if from == h1 || to == h1 {
			wk = false
		} else if from == a8 || to == a8 {
			bq = false
		} else if from == h8 || to == h8 {
			bk = false
		} else if from == whiteKingStartIndex {
			wk, wq = false, false
		} else if from == blackKingStartIndex {
			bk, bq = false, false
		}

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

		if piece == 'K' {
			b.whiteKingIndex = to
			// white king castle
			if from == whiteKingStartIndex {
				if to == g1 {
					// king side
					b.Pos[to+1] = ' '
					b.Pos[to-1] = 'R'
				} else if to == c1 {
					// queen side
					b.Pos[to-2] = ' '
					b.Pos[to+1] = 'R'
				}
			}
		} else if piece == 'k' {
			b.blackKingIndex = to
			// black king castle
			if from == blackKingStartIndex {
				if to == g8 {
					// king side
					b.Pos[to+1] = ' '
					b.Pos[to-1] = 'r'
				} else if to == c8 {
					// queen side
					b.Pos[to-2] = ' '
					b.Pos[to+1] = 'r'
				}
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
	var b Board
	b.LoadFEN(fen)
	return b
}

func (b *Board) LoadFEN(fen string) {
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

	b.ActiveColor = activeColor
	b.Castling = [4]bool{wk, wq, bk, bq}
	b.EnPassantSquare = epSquare
	b.HalfmoveClock = atoi(parts[4])
	b.FullMove = atoi(parts[5])

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
				if c == 'K' {
					b.whiteKingIndex = offset
				} else if c == 'k' {
					b.blackKingIndex = offset
				}
				b.Pos[offset] = c
				offset++
			}
		}
	}
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

func (b Board) IsCheck() bool {
	var (
		enemyQueen  byte
		enemyRook   byte
		enemyBishop byte
		enemyKnight byte
		enemyPawn   byte
		enemyKing   byte
	)

	var kingIndex, pawnRank int
	if b.ActiveColor == WhitePieces {
		enemyKing, enemyQueen, enemyRook, enemyBishop, enemyKnight, enemyPawn = 'k', 'q', 'r', 'b', 'n', 'p'
		kingIndex = b.whiteKingIndex
		pawnRank = -1
	} else {
		enemyKing, enemyQueen, enemyRook, enemyBishop, enemyKnight, enemyPawn = 'K', 'Q', 'R', 'B', 'N', 'P'
		kingIndex = b.blackKingIndex
		pawnRank = 1
	}

	kingRank := kingIndex / 8
	kingFile := kingIndex % 8

	pawnRank += kingRank

	// R = same rank or file
	// B = same diagonal (r +/- n, c +/- n)
	// Q = same rank, file, or diagonal
	// P = diagonal and king is one rank ahead (black king has lower index, white king has higher index)
	// N = (r +/- 2, c +/- 1) and (r +/- 1, c +/- 2)

	// pawns
	if pawnRank >= 0 && pawnRank < 8 {
		for _, fileOffset := range pawnPaths {
			pawnFile := kingFile + fileOffset
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
			c := b.Pos[idx]

			if c == enemyBishop || c == enemyQueen {
				// check by bishop or queen
				return true
			}

			if c != ' ' {
				break
			}

			file += path.file
			rank += path.rank
		}
	}

	// rooks and queens
	for _, path := range rookPaths {
		file, rank := kingFile+path.file, kingRank+path.rank
		for file >= 0 && file < 8 && rank >= 0 && rank < 8 {
			idx := rank*8 + file
			c := b.Pos[idx]

			if c == enemyRook || c == enemyQueen {
				// check by rook or queen
				return true
			}

			if c != ' ' {
				break
			}

			file += path.file
			rank += path.rank
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

func (b Board) IsMate() bool {
	if !b.IsCheck() {
		return false
	}

	return len(b.pieceLegalMoves(0)) == 0
}

func indexesToUCI(from, to int) string {
	return string([]byte{
		byte('a' + from%8),
		byte('8' - from/8),
		byte('a' + to%8),
		byte('8' - to/8),
	})
}

func indexToSquare(index int) string {
	file := byte('a' + index%8)
	rank := byte('8' - index/8)
	return string([]byte{file, rank})
}

func indexToRankFile(index int) (int, int) {
	file := index % 8
	rank := index / 8
	return rank, file
}

type LegalMove struct {
	Piece byte
	From  string
	To    string
	UCI   string
}

func (lm LegalMove) String() string {
	return fmt.Sprintf("{san-approx: %c%s piece: %c uci: %s}", upper(lm.Piece), lm.To, lm.Piece, lm.UCI)
}

type legalMove struct {
	from int
	to   int
}

func (b Board) PieceLegalMoves(piece byte) []LegalMove {
	if b.Pos[0] == 0 {
		b.LoadFEN(startPosFEN)
	}

	moves := b.pieceLegalMoves(piece)
	sanMoves := make([]LegalMove, 0, len(moves)+4)
	for _, m := range moves {
		from, to := indexToSquare(m.from), indexToSquare(m.to)
		uci := from + to
		p := b.Pos[m.from]
		if (p == 'p' && m.to >= 56) || (p == 'P' && m.to < 8) {
			for _, promote := range []string{"n", "b", "r", "q"} {
				promoteUCI := uci + promote
				sanMoves = append(sanMoves, LegalMove{Piece: p, From: from, To: to, UCI: promoteUCI})
			}
		} else {
			sanMoves = append(sanMoves, LegalMove{Piece: p, From: from, To: to, UCI: uci})
		}
	}

	return sanMoves
}

func (b Board) pieceLegalMoves(piece byte) []legalMove {
	var king, queen, bishop, knight, rook, pawn byte
	if b.ActiveColor == WhitePieces {
		king, queen, bishop, knight, rook, pawn = 'K', 'Q', 'B', 'N', 'R', 'P'
	} else {
		king, queen, bishop, knight, rook, pawn = 'k', 'q', 'b', 'n', 'r', 'p'
	}

	var moves []legalMove

	for i := 0; i < 64; i++ {
		if piece != 0 && piece != b.Pos[i] {
			continue
		}

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

func (b Board) AllLegalMoves() []LegalMove {
	return b.PieceLegalMoves(0)
}

func (b *Board) isEnemyPiece(p byte) bool {
	if b.ActiveColor == WhitePieces {
		return isLower(p)
	}
	return isUpper(p)
}

var (
	whiteShortCastle = [3]byte{' ', ' ', 'R'}
	whiteLongCastle  = [4]byte{'R', ' ', ' ', ' '}
	blackShortCastle = [3]byte{' ', ' ', 'r'}
	blackLongCastle  = [4]byte{'r', ' ', ' ', ' '}
	fenCastlingMap   = [4]byte{'K', 'Q', 'k', 'q'}
)

func (b Board) kingMoves(idx int) []int {
	moves := make([]int, 0, 8)

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

func (b Board) queenMoves(idx int) []int {
	return b.pathMoves(idx, kingPaths)
}

func (b Board) bishopMoves(idx int) []int {
	return b.pathMoves(idx, bishopPaths)
}

func (b Board) rookMoves(idx int) []int {
	return b.pathMoves(idx, rookPaths)
}

func (b Board) knightMoves(idx int) []int {
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

func (b Board) pawnMoves(idx int) []int {
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

func (b Board) pathMoves(idx int, paths []nav) []int {
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

func isLower(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func isUpper(b byte) bool {
	return b >= 'A' && b <= 'Z'
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

func iif[T any](condition bool, ifTrue, ifFalse T) T {
	if condition {
		return ifTrue
	}
	return ifFalse
}
