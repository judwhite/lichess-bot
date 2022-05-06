package polyglot

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"

	"trollfish-lichess/fen"
)

type Book struct {
	book map[string][]*BookEntry

	polyglotBook map[uint64][]*BookEntry
}

type BookEntry struct {
	UCIMove   string
	UCIPonder string
	Weight    uint16
	CP        int
	Mate      int

	polyglotMove uint16
}

func NewBook() *Book {
	return &Book{
		book: make(map[string][]*BookEntry),
	}
}

func (b *Book) BestMove(fenKey string) (string, bool) {
	bes, ok := b.Get(fenKey)
	if !ok {
		return "", false
	}

	n := rand.Intn(len(bes))
	return bes[n].UCIMove, true
}

func (b *Book) Get(fenKey string) ([]*BookEntry, bool) {
	if b == nil || b.book == nil {
		return nil, false
	}

	board := fen.FENtoBoard(fenKey)
	fenKey = board.FENKey()

	be, ok := b.book[fenKey]
	if ok {
		return be, true
	}

	if b.polyglotBook != nil {
		key := Key(board)
		be, ok = b.polyglotBook[key]
		if ok {
			delete(b.polyglotBook, key)

			fp, err := os.OpenFile("extract.epd", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatal(err)
			}
			defer fp.Close()

			for _, entry := range be {
				uciMove := toUCIMove(board, entry.polyglotMove)
				entry.UCIMove = uciMove

				san := board.UCItoSAN(uciMove)

				_, err := fmt.Fprintf(fp, "%s sm %s; weight %d;\n", fenKey, san, entry.Weight)
				if err != nil {
					log.Fatal(err)
				}
			}

			b.book[fenKey] = be
			return be, true
		}
	}

	return nil, false
}

func (b *Book) Add(fenKey, sanMove string, cp, mate int, sanPonder string) error {
	board := fen.FENtoBoard(fenKey)
	fenKey = board.FENKey()

	uci, err := board.SANtoUCI(sanMove)
	if err != nil {
		return err
	}

	var uciPonder string
	if sanPonder != "" {
		board.Moves(uci)
		uciPonder, err = board.SANtoUCI(sanPonder)
		if err != nil {
			return err
		}
	}

	b.book[fenKey] = append(b.book[fenKey], &BookEntry{UCIMove: uci, CP: cp, Mate: mate, UCIPonder: uciPonder})

	return nil
}

func (b *Book) PosCount() int {
	return len(b.book) + len(b.polyglotBook)
}

func (b *Book) AddBook(filename string) error {
	b2, err := LoadBook(filename)
	if err != nil {
		return err
	}

	// TODO: clobbers and only sets the polyglot book
	b.polyglotBook = b2.polyglotBook
	return nil
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
			offset := n
			n, err := r.Read(buf[offset:])
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}

			if n+offset != 32 {
				panic(fmt.Sprintf("n+offset=%d, want 32. need to do something about short reads", n+offset))
			}
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

		book.polyglotBook[key] = append(book.polyglotBook[key], &BookEntry{polyglotMove: move, Weight: freq})
	}

	return &book, nil
}
