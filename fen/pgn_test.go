package fen

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestPGNtoMoves(t *testing.T) {
	// arrange
	type testData struct {
		PGN      string   `json:"pgn"`
		UCIMoves []string `json:"uciMoves"`
		SANMoves []string `json:"sanMoves"`
	}
	fp, err := os.Open("testdata/pgn_uci_san.json")
	if err != nil {
		t.Fatal(err)
	}
	defer fp.Close()

	var cases []testData

	dec := json.NewDecoder(fp)
	if err := dec.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	fp.Close()

	for i, c := range cases {
		t.Run(fmt.Sprintf("%04d", i+1), func(t *testing.T) {
			moves, err := PGNtoMoves(c.PGN)
			if err != nil {
				t.Error(err)
				return
			}

			var uciMoves, sanMoves []string
			for _, m := range moves {
				uciMoves = append(uciMoves, m.UCI)
				sanMoves = append(sanMoves, m.SAN)
			}

			if !reflect.DeepEqual(c.UCIMoves, uciMoves) {
				t.Errorf("\nwant:\n%v\ngot:\n%v", c.UCIMoves, uciMoves)
			}
			if !reflect.DeepEqual(c.SANMoves, sanMoves) {
				t.Errorf("\nwant:\n%v\ngot:\n%v", c.SANMoves, sanMoves)
			}
		})
	}
}
