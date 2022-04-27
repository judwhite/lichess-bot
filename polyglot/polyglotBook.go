package polyglot

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"trollfish-lichess/fen"
)

type Book struct {
	book map[string][]*BookEntry

	polyglotBook map[uint64][]*BookEntry
}

func NewBook() *Book {
	return &Book{
		book: make(map[string][]*BookEntry),
	}
}

func (b *Book) Get(fenKey string) ([]*BookEntry, bool) {
	if b == nil || b.book == nil {
		return nil, false
	}

	board := fen.FENtoBoard(fenKey)
	fenKey = board.FENNoMoveClocks()

	be, ok := b.book[fenKey]
	if ok {
		return be, true
	}

	if b.polyglotBook != nil {
		key := Key(board)
		be, ok = b.polyglotBook[key]
		if ok {
			delete(b.polyglotBook, key)
			for _, entry := range be {
				uciMove := toUCIMove(board, entry.polyglotMove)
				entry.UCIMove = uciMove
			}
			b.book[fenKey] = be
			return be, true
		}
	}

	return nil, false
}

func (b *Book) Add(fenKey, sanMove string) error {
	board := fen.FENtoBoard(fenKey)
	fenKey = board.FENNoMoveClocks()

	uci, err := board.SANtoUCI(sanMove)
	if err != nil {
		return err
	}

	b.book[fenKey] = append(b.book[fenKey], &BookEntry{FEN: fenKey, UCIMove: uci})

	return nil
}

func (b *Book) PosCount() int {
	return len(b.book) + len(b.polyglotBook)
}

type BookEntry struct {
	FEN     string `json:"fen"`
	UCIMove string `json:"uci"`
	Freq    uint16 `json:"freq"`

	polyglotMove uint16
}

func LoadBook(filename string) (*Book, error) {
	fp, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	book := Book{
		book:         make(map[string][]*BookEntry),
		polyglotBook: make(map[uint64][]*BookEntry),
	}

	r := bufio.NewReaderSize(fp, 16384)
	buf := make([]byte, 32)
	for {
		n, err := r.Read(buf)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		if n != 32 {
			panic(fmt.Sprintf("n=%d, want 32. need to do something about short reads", n))
		}

		key := (uint64(buf[0]) << (7 * 8)) +
			(uint64(buf[1]) << (6 * 8)) +
			(uint64(buf[2]) << (5 * 8)) +
			(uint64(buf[3]) << (4 * 8)) +
			(uint64(buf[4]) << (3 * 8)) +
			(uint64(buf[5]) << (2 * 8)) +
			(uint64(buf[6]) << (1 * 8)) +
			uint64(buf[7])

		move := (uint16(buf[8]) << 8) | uint16(buf[9])
		freq := (uint16(buf[10]) << 8) | uint16(buf[11])

		book.polyglotBook[key] = append(book.polyglotBook[key], &BookEntry{polyglotMove: move, Freq: freq})
	}

	return &book, nil
}
