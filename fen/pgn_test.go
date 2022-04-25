package fen

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

type PGNMoves struct {
	PGN      string   `json:"pgn"`
	UCIMoves []string `json:"uciMoves"`
	SANMoves []string `json:"sanMoves"`
}

func TestPGNtoMoves(t *testing.T) {
	// arrange
	cases := pgnMovesTestData(t)

	for i, c := range cases {
		t.Run(fmt.Sprintf("%04d", i+1), func(t *testing.T) {
			t.Parallel()

			// act
			moves, err := PGNtoMoves(c.PGN)
			if err != nil {
				t.Error(err)
				return
			}

			// assert
			var uciMoves []string
			for _, m := range moves {
				uciMoves = append(uciMoves, m.UCI)
			}

			if !reflect.DeepEqual(c.UCIMoves, uciMoves) {
				t.Errorf("\nwant:\n%v\ngot:\n%v", c.UCIMoves, uciMoves)
			}

			var board Board
			var sanMoves []string
			for _, uciMove := range uciMoves {
				sanMoves = append(sanMoves, board.UCItoSAN(uciMove))
				board.Moves(uciMove)
			}

			if !reflect.DeepEqual(c.SANMoves, sanMoves) {
				t.Errorf("\nwant:\n%v\ngot:\n%v", c.SANMoves, sanMoves)
			}
		})
	}
}

func pgnMovesTestData(tb testing.TB) []PGNMoves {
	fp, err := os.Open("testdata/pgn_uci_san.json")
	if err != nil {
		tb.Fatal(err)
	}
	defer fp.Close()

	var cases []PGNMoves

	dec := json.NewDecoder(fp)
	if err := dec.Decode(&cases); err != nil {
		tb.Fatal(err)
	}

	return cases
}

func BenchmarkPGNtoMoves(b *testing.B) {
	cases := pgnMovesTestData(b)
	if len(cases) == 0 {
		b.Fatal("no test data")
	}
	pgn := cases[0].PGN

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := PGNtoMoves(pgn)
		if err != nil {
			b.Fatal(err)
		}
	}
}
