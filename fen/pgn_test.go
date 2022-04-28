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

type FENMoves struct {
	FEN string `json:"fen"`
	UCI string `json:"uci"`
	SAN string `json:"san"`
}

func TestPGNtoMoves(t *testing.T) {
	// arrange
	cases := pgnMovesTestData(t)

	for i, c := range cases {
		t.Run(fmt.Sprintf("%04d", i+1), func(t *testing.T) {
			t.Parallel()

			// act
			pgn, err := ParsePGN(c.PGN)
			if err != nil {
				t.Error(err)
				return
			}

			// assert
			var uciMoves []string
			for _, m := range pgn.Moves {
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

			// now test SAN to UCI. should break this out into a separate test.
			board.LoadFEN("")
			for i, sanMove := range sanMoves {
				gotUCI, err := board.SANtoUCI(sanMove)
				if err != nil {
					t.Fatal(err)
				}
				wantUCI := uciMoves[i]
				if wantUCI != gotUCI {
					t.Errorf("want: '%s' got: '%s'", wantUCI, gotUCI)
				}
				board.Moves(wantUCI)
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

func fenMovesTestData(tb testing.TB) []FENMoves {
	fp, err := os.Open("testdata/fen_uci_san.json")
	if err != nil {
		tb.Fatal(err)
	}
	defer fp.Close()

	var cases []FENMoves

	dec := json.NewDecoder(fp)
	if err := dec.Decode(&cases); err != nil {
		tb.Fatal(err)
	}

	return cases
}

func TestSANtoUCI(t *testing.T) {
	cases := fenMovesTestData(t)

	for _, c := range cases {
		t.Run(c.FEN+" "+c.SAN, func(t *testing.T) {
			board := FENtoBoard(c.FEN)
			uci, err := board.SANtoUCI(c.SAN)
			if err != nil {
				t.Fatal(err)
			}

			if c.UCI != uci {
				t.Errorf("want: '%s' got: '%s'", c.UCI, uci)
			}
		})
	}
}

func BenchmarkPGNtoMoves(b *testing.B) {
	const pgn = `1. e4 e5 2. Nf3 Nc6 3. Bb5 Nf6 4. O-O Nxe4 5. d4 Nd6 6. Bxc6 dxc6 7. dxe5
Nf5 8. Qxd8+ Kxd8 9. Nc3 Be7 10. Bf4 Be6 11. g4 Nh4 12. Nxh4 Bxh4 13. g5
Ke7 14. Ne4 Rhd8 15. Rfe1 a5 16. a4 b6 17. Kg2 Rd4 18. c3 Rd3 19. Bg3 Bxg3
20. hxg3 Bg4 21. f4 c5 22. Kg1 Rad8 23. Rac1 c4 24. Nf2 Rxg3+ 25. Kh2 Rf3
26. Nxg4 Rd2+ 27. Kg1 Rg3+ 28. Kf1 Rxg4 29. Re4 Rgg2 30. Rxc4 Rdf2+ 31. Ke1
Re2+ 32. Kf1 Ref2+ 33. Ke1 Rxb2 34. Kf1 Rbf2+ 35. Ke1 Ra2 36. Kf1 Raf2+ 37.
Ke1 Rh2 38. Rxc7+ Kd8 39. Rc4 Rb2 40. Rd1+ Ke7 41. Rc7+ Ke6 42. Rd6+ Kf5
43. Rd2 Rbxd2 44. Rxf7+ Kg4 45. Rf6 Ra2 46. Rxb6 Rh1# 0-1`

	for i := 0; i < b.N; i++ {
		_, err := ParsePGN(pgn)
		if err != nil {
			b.Fatal(err)
		}
	}
}
