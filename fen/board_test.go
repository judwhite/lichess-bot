package fen

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestFENtoBoard(t *testing.T) {
	// arrange
	cases := []struct {
		fen                 string
		wantActiveColor     Color
		wantCastling        [4]bool
		wantEnPassantSquare int
		wantHalfMoveClock   int
		wantFullMove        int
		wantPos             []byte
	}{
		{
			fen:                 "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
			wantActiveColor:     WhitePieces,
			wantCastling:        [4]bool{true, true, true, true},
			wantEnPassantSquare: -1,
			wantHalfMoveClock:   0,
			wantFullMove:        1,
			wantPos: []byte{
				'r', 'n', 'b', 'q', 'k', 'b', 'n', 'r',
				'p', 'p', 'p', 'p', 'p', 'p', 'p', 'p',
				' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ',
				' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ',
				' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ',
				' ', ' ', ' ', ' ', ' ', ' ', ' ', ' ',
				'P', 'P', 'P', 'P', 'P', 'P', 'P', 'P',
				'R', 'N', 'B', 'Q', 'K', 'B', 'N', 'R',
			},
		},
		{
			fen:                 "r1b1kbnr/pppp1ppp/2n5/4P3/1q6/5N2/PPPBPPPP/RN1QKB1R b KQkq - 6 5",
			wantActiveColor:     BlackPieces,
			wantCastling:        [4]bool{true, true, true, true},
			wantEnPassantSquare: -1,
			wantHalfMoveClock:   6,
			wantFullMove:        5,
			wantPos: []byte{
				'r', ' ', 'b', ' ', 'k', 'b', 'n', 'r',
				'p', 'p', 'p', 'p', ' ', 'p', 'p', 'p',
				' ', ' ', 'n', ' ', ' ', ' ', ' ', ' ',
				' ', ' ', ' ', ' ', 'P', ' ', ' ', ' ',
				' ', 'q', ' ', ' ', ' ', ' ', ' ', ' ',
				' ', ' ', ' ', ' ', ' ', 'N', ' ', ' ',
				'P', 'P', 'P', 'B', 'P', 'P', 'P', 'P',
				'R', 'N', ' ', 'Q', 'K', 'B', ' ', 'R',
			},
		},
	}

	sq := func(c byte) string {
		if c == 0 || c == ' ' {
			return " "
		}
		return string(c)
	}

	for _, c := range cases {
		t.Run(c.fen, func(t *testing.T) {
			// act
			board := FENtoBoard(c.fen)

			// assert
			if !bytes.Equal(c.wantPos, board.Pos[:]) {
				var (
					loc       string
					want, got strings.Builder
				)
				writeBoth := func(s string) {
					want.WriteString(s)
					got.WriteString(s)
				}

				var diff strings.Builder
				for i := 0; i < 64; i++ {
					if c.wantPos[i] != board.Pos[i] {
						diff.WriteString(fmt.Sprintf("index: %d want: '%c' %d got: '%c' %d", i, c.wantPos[i], c.wantPos[i], board.Pos[i], board.Pos[i]))
					}
				}

				writeBoth("   abcdefgh\n   --------\n")
				for i := 0; i < 8; i++ {
					offset := i * 8
					rank := '8' - i
					writeBoth(fmt.Sprintf("%c: ", rank))

					for j := 0; j < 8; j++ {
						idx := offset + j
						if c.wantPos[idx] != board.Pos[idx] {
							file := 'a' + j
							if loc != "" {
								loc += ", "
							}
							loc += fmt.Sprintf("%c%c", file, rank)
						}

						want.WriteString(sq(c.wantPos[idx]))
						got.WriteString(sq(board.Pos[idx]))
					}

					writeBoth("\n")
				}

				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("board differs at '%s', %s\nwant:\n%s\ngot:\n%s", loc, diff.String(), want.String(), got.String()))
				t.Error(sb.String())
			}
			if board.ActiveColor != c.wantActiveColor {
				t.Errorf("ActiveColor, want: '%s' got: '%s'", c.wantActiveColor, board.ActiveColor)
			}
			if board.Castling != c.wantCastling {
				t.Errorf("Castling, want: '%v' got: '%v'", c.wantCastling, board.Castling)
			}
			if board.EnPassantSquare != c.wantEnPassantSquare {
				t.Errorf("EnPassantSquare, want: '%d' got: '%d'", c.wantEnPassantSquare, board.EnPassantSquare)
			}
			if board.HalfmoveClock != c.wantHalfMoveClock {
				t.Errorf("HalfmoveClock, want: '%v' got: '%v'", c.wantHalfMoveClock, board.HalfmoveClock)
			}
			if board.FullMove != c.wantFullMove {
				t.Errorf("FullMove, want: '%v' got: '%v'", c.wantFullMove, board.FullMove)
			}
		})
	}
}

func TestMakeMoves(t *testing.T) {
	// arrange
	cases := []struct {
		start string
		moves []string
		want  string
	}{
		{
			start: startPosFEN,
			moves: []string{"g1f3"},
			want:  "rnbqkbnr/pppppppp/8/8/8/5N2/PPPPPPPP/RNBQKB1R b KQkq - 1 1",
		},
		{
			start: startPosFEN,
			moves: []string{"g1f3"},
			want:  "rnbqkbnr/pppppppp/8/8/8/5N2/PPPPPPPP/RNBQKB1R b KQkq - 1 1",
		},
		{
			start: startPosFEN,
			moves: strings.Split("g1f3 d7d5 e2e3 c7c5 b1c3 g8f6 d2d4 e7e6 f1e2 b8c6", " "),
			want:  "r1bqkb1r/pp3ppp/2n1pn2/2pp4/3P4/2N1PN2/PPP1BPPP/R1BQK2R w KQkq - 2 6",
		},
		{
			start: startPosFEN,
			moves: strings.Split("g1f3 d7d5 e2e3 c7c5 b1c3 g8f6 d2d4 e7e6 f1e2 b8c6 e1g1", " "),
			want:  "r1bqkb1r/pp3ppp/2n1pn2/2pp4/3P4/2N1PN2/PPP1BPPP/R1BQ1RK1 b kq - 3 6",
		},
		{
			start: "r1bqkb1r/pp3ppp/2n1pn2/2pp4/3P4/2N1PN2/PPP1BPPP/R1BQ1RK1 b kq - 3 6",
			moves: strings.Split("c6b4 h2h4 b7b6 h4h5 g7g5", " "),
			want:  "r1bqkb1r/p4p1p/1p2pn2/2pp2pP/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 w kq g6 0 9",
		},
		{
			start: "r1bqkb1r/p4p1p/1p2pn2/2pp2pP/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 w kq g6 0 9",
			moves: []string{"h5g6"},
			want:  "r1bqkb1r/p4p1p/1p2pnP1/2pp4/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 b kq - 0 9",
		},
		{
			start: "r1bqkb1r/p4p1p/1p2pnP1/2pp4/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 b kq - 0 9",
			moves: strings.Split("h7h5 g6g7 f6e4 g7h8q", " "),
			want:  "r1bqkb1Q/p4p2/1p2p3/2pp3p/1n1Pn3/2N1PN2/PPP1BPP1/R1BQ1RK1 b q - 0 11",
		},
		{
			start: "r1bqkb1r/p4p1p/1p2pnP1/2pp4/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 b kq - 0 9",
			moves: strings.Split("h7h5 g6g7 f6e4 g7h8q e4g3 h8f8", " "),
			want:  "r1bqkQ2/p4p2/1p2p3/2pp3p/1n1P4/2N1PNn1/PPP1BPP1/R1BQ1RK1 b q - 0 12",
		},
		{
			start: "r1bqkb1r/p4p1p/1p2pnP1/2pp4/1n1P4/2N1PN2/PPP1BPP1/R1BQ1RK1 b kq - 0 9",
			moves: strings.Split("h7h5 g6g7 f6e4 g7h8q e4g3 h8f8 e8d7", " "),
			want:  "r1bq1Q2/p2k1p2/1p2p3/2pp3p/1n1P4/2N1PNn1/PPP1BPP1/R1BQ1RK1 w - - 1 13",
		},
		{
			start: startPosFEN,
			moves: strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 f8b4 b1d2 d7d5 f1g2 e8g8 g1f3 b7b6 e1g1 c8b7 f3e5 a7a5 d1c2 c7c5 c4d5 b7d5 e2e4 d5b7 d4c5 d8c8 e5d3 b4d2 c1d2 f6e4 a1c1 e4d2 g2b7 c8b7 c2d2 f8d8 d2e3 b6c5 c1c5 b8d7 c5c4 d7b6 c4c5 a8c8 f1c1 h7h6 b2b3 c8c5 d3c5 b7d5 c5e4 b6c8 e4c3 d5a8 e3e4 a8b8 c1d1 c8e7 d1d8 b8d8 g1g2 d8d2 e4f3 e7d5 c3d5 e6d5 h2h4 d2a2 f3d5 g7g6 h4h5 g6h5 d5d8 g8g7 d8d4 g7h7 d4e4 h7h8 e4e8 h8g7 e8e5 g7g6 e5e4 g6g7 e4e5 g7g8 e5h5 a2d2 g2f1 d2c1 f1g2 c1c6 g2g1 c6c1 g1g2 c1d2 h5e5 g8f8 e5h8 f8e7 h8e5 e7d7 g2f3 d2b4 f3g2 b4b7 g2h2 b7f3 e5e1 d7c6 h2g1 f3d5 e1e8 c6c5 e8e1 c5b6 e1e3 b6b5 e3e8 b5b4 e8a4 b4c3 a4a3 d5d1 g1h2 d1h5 h2g2 c3c2 b3b4 h5d5 g2g1 d5d1 g1g2 a5b4 a3b4 d1d5 g2h2 d5h5 h2g2 c2d3 b4b3 d3d4 b3b6 d4c4 b6c6 c4d4 c6a4 d4d3 a4b3 d3e4 f2f3 e4d4 b3a4 d4c3 a4a3 c3c2 a3e3 h5g6 f3f4 g6h5 g2f2 h5d5 g3g4 d5d2 f2f3 d2d7 f4f5 d7d3 f3f4 d3d6 e3e5 d6d2 e5e3 d2d8 e3e4 c2c3 e4e3 c3c4 e3e4 c4c5 e4e5 c5c6 f4e4 d8d6 e5e8 c6c5 e8c8 c5b5 c8c3 f7f6 e4f3 d6d1 f3f4 d1d6 f4e4 d6c6 c3c6 b5c6 e4f4 c6d7 f4g3 d7e8 g3f4 e8f8 f4g3 f8e7 g3h4 e7f7 h4h3 f7e8 h3g3 e8f7 g3h2 f7e7 h2g3 e7e8 g3g2 e8d7 g2h3 d7e7 h3h4 e7f8 h4h3 f8g7 h3g3 h6h5 g4h5 g7h7 g3g4 h7g7 h5h6 g7h6 g4h4 h6h7 h4g3 h7h6 g3h4 h6g7 h4g3 g7h7 g3f4 h7g7 f4g3 g7h8 g3g4 h8h7 g4h3 h7h6 h3g4 h6g7 g4h3 g7f8 h3g4 f8g8 g4h3 g8f7 h3g4 f7f8 g4f4 f8e7 f4e4 e7d6 e4d4 d6c6 d4c4 c6d6 c4d4 d6c6 d4c4 c6d6 c4d4", " "),
			want:  "8/8/3k1p2/5P2/3K4/8/8/8 b - - 39 135",
		},
		{
			start: startPosFEN,
			moves: strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 f8b4 b1d2 d7d5 f1g2 e8g8 g1f3 b7b6 e1g1 c8b7 f3e5 a7a5 d1c2 c7c5 c4d5 b7d5 e2e4 d5b7 d4c5 d8c8 e5d3 b4d2 c1d2 f6e4 a1c1 e4d2 g2b7 c8b7 c2d2 f8d8 d2e3 b6c5 c1c5 b8d7 c5c4 d7b6 c4c5 a8c8 f1c1 h7h6 b2b3 c8c5 d3c5 b7d5 c5e4 b6c8 e4c3 d5a8 e3e4 a8b8 c1d1 c8e7 d1d8 b8d8 g1g2 d8d2 e4f3 e7d5 c3d5 e6d5 h2h4 d2a2 f3d5 g7g6 h4h5 g6h5 d5d8 g8g7 d8d4 g7h7 d4e4 h7h8 e4e8 h8g7 e8e5 g7g6 e5e4 g6g7 e4e5 g7g8 e5h5 a2d2 g2f1 d2c1 f1g2 c1c6 g2g1 c6c1 g1g2 c1d2 h5e5 g8f8 e5h8 f8e7 h8e5 e7d7 g2f3 d2b4 f3g2 b4b7 g2h2 b7f3 e5e1 d7c6 h2g1 f3d5 e1e8 c6c5 e8e1 c5b6 e1e3 b6b5 e3e8 b5b4 e8a4 b4c3 a4a3 d5d1 g1h2 d1h5 h2g2 c3c2 b3b4 h5d5 g2g1 d5d1 g1g2 a5b4 a3b4", " "),
			want:  "8/5p2/7p/8/1Q6/6P1/2k2PK1/3q4 b - - 0 67",
		},
		{
			start: startPosFEN,
			moves: strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 f8b4 b1d2 d7d5 f1g2 e8g8 g1f3 b7b6 e1g1 c8b7 f3e5 a7a5 d1c2 c7c5 c4d5 b7d5 e2e4 d5b7 d4c5 d8c8 e5d3 b4d2 c1d2 f6e4 a1c1 e4d2 g2b7 c8b7 c2d2 f8d8 d2e3 b6c5 c1c5 b8d7 c5c4 d7b6 c4c5 a8c8 f1c1 h7h6 b2b3 c8c5 d3c5 b7d5 c5e4 b6c8 e4c3 d5a8 e3e4 a8b8 c1d1 c8e7 d1d8 b8d8", " "),
			want:  "3q2k1/4npp1/4p2p/p7/4Q3/1PN3P1/P4P1P/6K1 w - - 0 30",
		},
		{
			start: startPosFEN,
			moves: strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 f8b4 b1d2 d7d5 f1g2", " "),
			want:  "rnbqk2r/ppp2ppp/4pn2/3p4/1bPP4/6P1/PP1NPPBP/R1BQK1NR b KQkq - 1 5",
		},
		{
			start: startPosFEN,
			moves: strings.Split("d2d4 g8f6 c2c4 e7e6 g2g3 f8b4 b1d2 d7d5 f1g2 e8g8", " "),
			want:  "rnbq1rk1/ppp2ppp/4pn2/3p4/1bPP4/6P1/PP1NPPBP/R1BQK1NR w KQ - 2 6",
		},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.moves, " "), func(t *testing.T) {
			// act
			b := FENtoBoard(c.start)
			b.Moves(c.moves...)
			got := b.FEN()

			// assert
			if c.want != got {
				t.Errorf(fmt.Sprintf("\nwant: '%s'\ngot:  '%s'", c.want, got))
			}
		})
	}
}

func TestBoard_UCItoSAN(t *testing.T) {
	cases := []struct {
		fen  string
		uci  string
		want string
	}{
		{fen: "rnbqkbnr/pp2pppp/8/2ppP3/8/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 3", uci: "e5d6", want: "exd6"},
		{fen: "rnbqkbnr/pp3ppp/3P4/2p1p3/8/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 4", uci: "d6d7", want: "d7+"},
		{fen: "rnb1kbnr/pp1q1ppp/8/8/2pPp3/2N2N2/PPP2PPP/R1BQKB1R b KQkq d3 0 7", uci: "c4d3", want: "cxd3"},
		{fen: "rnb1kbnr/pp1q1ppp/8/8/2pPp3/2N2N2/PPP2PPP/R1BQKB1R b KQkq d3 0 7", uci: "e4d3", want: "exd3"},
		{fen: "rnb1kbnr/pp1q1ppp/8/8/2p5/4NN2/PPp2PPP/R1BQKB1R b KQkq - 1 9", uci: "c2d1q", want: "cxd1=Q+"},
		{fen: "rnb2bnr/p4ppp/4k3/1p3N2/2pq4/5N2/PP2BPPP/R1B1K2R w KQ - 6 14", uci: "f3d4", want: "N3xd4+"},
		{fen: "rnb2bnr/p4ppp/4k3/1p3N2/2pq4/5N2/PP2BPPP/R1B1K2R w KQ - 6 14", uci: "f5d4", want: "N5xd4+"},
		{fen: "rn4nr/Pb1kbppp/8/1p3N2/2pq4/5N2/P3BPPP/R1B1K2R w KQ - 1 18", uci: "a7b8n", want: "axb8=N+"},
		{fen: "r1b3nr/4bppp/2N1k3/1p3N2/2pq4/5N2/P3BPPP/R1B1K2R w KQ - 3 20", uci: "c6d4", want: "Ncxd4+"},
		{fen: "r1b3nr/4bppp/4k3/1p3N2/2pq4/1N3N2/P3BPPP/R1B1K2R w KQ - 7 22", uci: "b3d4", want: "Nbxd4+"},
		{fen: "r1b3nr/4bppp/4k3/1p3N2/2pq4/1N3N2/P3BPPP/R1B1K2R w KQ - 7 22", uci: "f3d4", want: "Nf3xd4+"},
		{fen: "r1b3nr/4bppp/4k3/1p3N2/2pq4/1N3N2/P3BPPP/R1B1K2R w KQ - 7 22", uci: "f5d4", want: "N5xd4+"},
		{fen: "r1b2b1r/6pp/4k3/1p3N2/2pqN3/1N3N2/P3BP1P/R1B1K2R w KQ - 5 29", uci: "e4d6", want: "Ned6"},
		{fen: "r1b2b1r/6pp/4k3/1N3N2/2pq4/1N3N2/P3BP1P/R1B1K2R w KQ - 1 31", uci: "b5d4", want: "Nb5xd4+"},
		{fen: "r1b2b1r/6pp/4k3/1N3N2/2pq4/1N3N2/P3BP1P/R1B1K2R w KQ - 1 31", uci: "b5d6", want: "Nbd6"},
		{fen: "r1b2b1r/6pp/4k3/1N3N2/2pq4/1N3N2/P3BP1P/R1B1K2R w KQ - 1 31", uci: "f3d2", want: "Nfd2"},
		{fen: "r1b2b1r/6pp/4k3/1N3N2/2pq4/1N3N2/P3BP1P/R1B1K2R w KQ - 1 31", uci: "f3h4", want: "N3h4"},
		{fen: "r1bb1R1r/3k2pp/8/1N6/2pq3N/1N2N3/P3B2P/R1B1K2R w KQ - 1 38", uci: "h1f1", want: "Rhf1"},
		{fen: "r4R1r/qbbk2pp/8/1N6/2B4N/1Np1N3/PR5P/2B1KR2 w - - 4 42", uci: "b2f2", want: "Rbf2"},
		{fen: "r1b2R1r/q1bk2pp/8/1N6/2B4N/1Np1N3/P4R1P/2B1KR2 w - - 6 43", uci: "f8f5", want: "R8f5"},
		{fen: "r1b4r/q1bk2pp/8/1N3R2/2B5/1Np1NN2/P4R1P/2B1KR2 w - - 10 45", uci: "f5f4", want: "Rf4"},
		{fen: "r1bb3r/q2k2pp/8/1N3R2/2B5/1Np1N3/P2N1R1P/2B1KR2 w - - 12 46", uci: "f5f4", want: "R5f4"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/5R2/1Nb5/P1pN1R1P/2B1KR2 w - - 4 50", uci: "d6c4", want: "Nc4"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/5R2/1Nb5/P1pN1R1P/2B1KR2 w - - 4 50", uci: "b5d4", want: "N5d4"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/5R2/1Nb5/P1pN1R1P/2B1KR2 w - - 4 50", uci: "b3d4", want: "N3d4"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/5R2/2b5/P1NN1R1P/2B1KR2 w - - 1 52", uci: "c2a3", want: "Nca3"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/1b3R2/N7/P2N1R1P/2B1KR2 w - - 3 53", uci: "d6c4", want: "Ndc4"},
		{fen: "r6r/qb1k1Bpp/3N4/1N6/5R2/N1b5/P2N1R1P/2BK1R2 w - - 5 54", uci: "d6c4", want: "N6c4"},
		{fen: "r6r/qb1k1Bpp/8/8/N1b1NR2/8/N3NR1P/2B2K2 w - - 7 55", uci: "e4c3", want: "Nec3"},
		{fen: "r6r/qb1k1Bpp/8/8/N1b1NR2/8/N3NR1P/2B2K2 w - - 7 55", uci: "a4c3", want: "Na4c3"},
		{fen: "r6r/qb1k1Bpp/8/8/N1b1NR2/8/N3NR1P/2B2K2 w - - 7 55", uci: "a2c3", want: "N2c3"},
		{fen: "r6r/qb1k1Bpp/8/1b6/N3NR2/8/N3NR1P/2B1K3 w - - 9 56", uci: "e2c3", want: "Ne2c3"},
		{fen: "r6r/qb1k1Bpp/8/1b6/N3NR2/8/N3NR1P/2B1K3 w - - 9 56", uci: "e4c3", want: "Ne4c3"},
		{fen: "r6r/qb1k1Bpp/8/1b6/N3NR2/8/N3NR1P/2B1K3 w - - 9 56", uci: "a4c3", want: "Na4c3"},
		{fen: "r6r/qb1k1Bpp/8/1b6/N3NR2/8/N3NR1P/2B1K3 w - - 9 56", uci: "a2c3", want: "Na2c3"},
		{fen: "2K1k2r/r4ppp/8/8/8/8/5PPP/R6R b k - 0 1", uci: "e8g8", want: "O-O#"},
		{fen: "r3k2K/7p/8/7n/8/8/5PPP/R6R b q - 0 1", uci: "e8c8", want: "O-O-O+"},
		{fen: "r3k2K/7p/6b1/7n/8/8/5PPP/R6R b q - 0 1", uci: "e8c8", want: "O-O-O#"},
		{fen: "r3k2K/7P/8/7n/8/8/5PPP/R6R b q - 0 1", uci: "e8c8", want: "O-O-O#"},
		{fen: "r7/8/8/8/8/8/R4PPP/2k1K2R w K - 0 1", uci: "e1g1", want: "O-O#"},
		{fen: "3r4/8/8/8/8/8/R4PPP/2k1K2R w K - 0 1", uci: "e1g1", want: "O-O+"},
		{fen: "8/8/8/8/8/8/R6r/R3K2k w Q - 0 1", uci: "e1c1", want: "O-O-O#"},
		{fen: "8/8/8/8/8/8/7p/R3K2k w Q - 0 1", uci: "e1c1", want: "O-O-O+"},
		{fen: "8/8/8/8/8/8/7p/R3K2k w Q - 0 1", uci: "e1f2", want: "Kf2#"},
		{fen: "5k2/8/3K2Q1/2B5/8/8/8/8 w - - 0 1", uci: "d6d5", want: "Kd5#"},
		{fen: "4qkn1/4pPp1/8/6Q1/3B4/8/8/K7 w - - 0 1", uci: "g5g7", want: "Qxg7#"},
		{fen: "4qkn1/4pPp1/8/6Q1/3B4/8/8/K7 w - - 0 1", uci: "d4g7", want: "Bxg7+"},
		{fen: "3qkn2/3pPp2/7Q/8/4Q3/8/8/K7 w - - 0 1", uci: "e7f8q", want: "exf8=Q#"},
		{fen: "3qkn2/3pPp2/7Q/8/4Q3/8/8/K7 w - - 0 1", uci: "e7f8n", want: "exf8=N+"},
		{fen: "3qkn2/3pPp2/7Q/8/4Q3/8/8/K7 w - - 0 1", uci: "h6f8", want: "Qxf8#"},
		{fen: "3qkn2/3pPp2/7Q/8/4Q3/8/8/K7 w - - 0 1", uci: "e7d8q", want: "exd8=Q+"},
		{fen: "3rkn2/3pPp2/7Q/8/4Q3/8/8/K7 w - - 0 1", uci: "e7f8n", want: "exf8=N#"},
		{fen: "2n2n2/3kPp2/1Q6/4Q3/8/8/8/K7 w - - 0 1", uci: "e7f8n", want: "exf8=N#"},
		{fen: "5n2/3kP3/1Q6/4Q3/8/8/8/K7 w - - 0 1", uci: "e7e8q", want: "e8=Q#"},
		{fen: "5n2/3kP3/1Q6/4Q3/8/8/8/K7 w - - 0 1", uci: "e7f8q", want: "exf8=Q"},
		{fen: "2k5/8/1Q6/4P3/8/8/8/K7 w - - 4 4", uci: "e5e6", want: "e6"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "e5d6", want: "Qed6"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "b6d6", want: "Qbd6"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "e5e6", want: "Qee6#"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "e5f5", want: "Qf5#"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "b6c7", want: "Qbc7#"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "e5c7", want: "Qec7#"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "b6b8", want: "Qbb8+"},
		{fen: "2k5/8/1Q6/4Q3/8/8/8/K7 w - - 1 2", uci: "e5b8", want: "Qeb8+"},
		{fen: "1Q6/3k4/1Q6/8/8/8/8/K7 w - - 3 3", uci: "b8d8", want: "Q8d8#"},
		{fen: "1Q6/3k4/1Q6/8/8/8/8/K7 w - - 3 3", uci: "b6d6", want: "Q6d6#"},
		{fen: "1Q6/3k4/1Q6/8/8/8/8/K7 w - - 3 3", uci: "b6d8", want: "Q6d8+"},
		{fen: "1Q6/3k4/1Q6/8/8/8/8/K7 w - - 3 3", uci: "b8d6", want: "Q8d6+"},
		{fen: "2k5/8/1Q1Q4/8/8/8/8/K7 w - - 5 4", uci: "d6d7", want: "Qd7+"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "d6e6", want: "Qd6e6#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "f6g6", want: "Qfg6#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "h6g6", want: "Qhg6#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "d3g6", want: "Qdg6#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "d3e4", want: "Q3e4#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "d6e7", want: "Qde7#"},
		{fen: "4k3/8/3Q1Q1Q/3Q4/8/3Q4/8/K7 w - - 5 4", uci: "d5e6", want: "Q5e6#"},
		{fen: "1b2k3/8/3Q1Q1Q/3Q4/8/3Q2K1/8/8 w - - 5 4", uci: "d5e6", want: "Qde6#"},
		{fen: "1b2k3/8/3Q1Q1Q/3Q4/8/3Q2K1/8/8 w - - 5 4", uci: "d5e5", want: "Q5e5#"},
		{fen: "1b2k3/8/3Q1Q1Q/3Q4/8/3Q2K1/8/8 w - - 5 4", uci: "d6e5", want: "Qd6e5+"},
		{fen: "4k3/8/8/8/3pPp2/8/8/4K3 b - e3 0 1", uci: "d4e3", want: "dxe3"},
		{fen: "4k3/8/8/8/3pPp2/8/8/4K3 b - e3 0 1", uci: "f4e3", want: "fxe3"},
		{fen: "4k3/8/8/5p2/3pPp2/5p2/5b2/K7 b - e3 0 1", uci: "f4e3", want: "fxe3"},
		{fen: "4k3/8/8/5p2/3pPp2/5p2/5b2/K7 b - e3 0 1", uci: "f5e4", want: "fxe4"},
		{fen: "3Q4/4pk2/8/6Q1/8/8/6Q1/R4K2 w - - 2 1", uci: "g2d5", want: "Q2d5+"},
		{fen: "6Q1/4p3/5k2/8/6Q1/8/8/R2Q1K2 w - - 2 1", uci: "d1d6", want: "Qd6+"},
		{fen: "5Q2/4p3/4k3/8/3Q4/B7/8/R4K2 w - - 3 1", uci: "d4d6", want: "Qd6+"},
		{fen: "6Q1/4p3/5k2/8/6Q1/8/8/3Q1K2 w - - 3 1", uci: "d1d4", want: "Qdd4+"},
		{fen: "2Q3R1/3pk3/2p5/q3P3/2K5/4q3/8/8 b - - 0 1", uci: "d7d5", want: "d5+"},
		{fen: "2Q3Q1/4k3/2p3Q1/q2pP3/2K5/4q3/8/8 w - d6 0 2", uci: "e5d6", want: "exd6#"},

		{fen: "rnbqk2r/1p3ppp/p2ppb2/8/4P3/2NB1N2/PPP2PPP/R2QK2R b KQkq - 1 9", uci: "e8g8", want: "O-O"},
		{fen: "r1b2rk1/1p1n1ppp/pq1ppb2/8/P3P3/2NB1N2/1PPQ1PPP/R4RK1 w - - 1 13", uci: "a1e1", want: "Rae1"},
		{fen: "r1b1r1k1/1p1q1pp1/3bp2p/8/P7/3N1N2/2PQ1PPP/4RRK1 w - - 0 22", uci: "d3e5", want: "Nde5"},
		{fen: "2bqr1k1/1pb2pp1/r3p2p/4N3/P7/R4N2/2PQ1PPP/5RK1 w - - 6 25", uci: "d2d8", want: "Qxd8"},
		{fen: "6k1/1p2bpp1/4p2p/Pb6/3R3P/1Nr1N1P1/2P2PK1/8 b - - 2 38", uci: "g7g5", want: "g5"},
		{fen: "6k1/1p2bp2/4p2p/Pb4p1/3R3P/1Nr1N1P1/2P2PK1/8 w - g6 0 39", uci: "h4g5", want: "hxg5"},
		{fen: "6k1/1p3p2/2b1p2p/P7/3R2K1/1Nr1b1P1/2P2P2/8 w - - 0 42", uci: "f2e3", want: "fxe3"},
		{fen: "6k1/1p3p2/4p2p/Pb4b1/3R4/1Nr1NKP1/2P2P2/8 b - - 1 40", uci: "b5c6", want: "Bc6+"},
		{fen: "8/1p3pk1/2b1p2p/P6K/3R4/1N2r1P1/2P5/8 w - - 2 44", uci: "d4g4", want: "Rg4+"},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %s", c.fen, c.uci), func(t *testing.T) {
			//fmt.Printf("fen: %s want: %s\n", c.fen, c.want)

			board := FENtoBoard(c.fen)
			got := board.UCItoSAN(c.uci)

			if c.want != got {
				t.Errorf("want: '%s' got: '%s'", c.want, got)
				//fmt.Printf("... got %s instead.\n", got)
			} else {
				//fmt.Printf("OK\n")
			}
		})
	}
}

func TestBoard_KingMoves(t *testing.T) {
	cases := []struct {
		fen         string
		startSquare string
		want        []string
	}{
		{
			fen:         "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"d2", "e2", "f2", "d1", "f1", "c1", "g1"},
		},
		{
			fen:         "1r2k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"d2", "e2", "f2", "d1", "f1", "c1", "g1"},
		},
		{
			fen:         "2r1k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"d2", "e2", "f2", "d1", "f1", "g1"},
		},
		{
			fen:         "3rk2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"e2", "f2", "f1", "g1"},
		},
		{
			fen:         "4k2r/4r3/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"d2", "f2", "d1", "f1"},
		},
		{
			fen:         "r3k1r1/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"c1", "d1", "d2", "e2", "f1", "f2"},
		},
		{
			fen:         "r3kr2/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"c1", "d1", "d2", "e2"},
		},
		{
			fen:         "r3k3/4r3/8/8/8/8/8/R3K2R w KQkq - 0 1",
			startSquare: "e1",
			want:        []string{"d1", "d2", "f1", "f2"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/R3K2R b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"c8", "d7", "d8", "e7", "f7", "f8", "g8"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/R3K1R1 b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"c8", "d7", "d8", "e7", "f7", "f8"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/R3KR2 b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"c8", "d7", "d8", "e7"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/1R2K2R b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"c8", "d7", "d8", "e7", "f7", "f8", "g8"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/2R1K2R b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"d7", "d8", "e7", "f7", "f8", "g8"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/8/3RK2R b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"e7", "f7", "f8", "g8"},
		},
		{
			fen:         "r3k2r/8/8/8/8/8/4R3/4K2R b KQkq - 0 1",
			startSquare: "e8",
			want:        []string{"d7", "d8", "f7", "f8"},
		},
		{
			fen:         "8/8/8/8/8/8/7p/2KR3k b - - 1 1",
			startSquare: "h1",
			want:        []string{"g2"},
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %s", c.fen, strings.Join(c.want, " ")), func(t *testing.T) {
			// arrange
			startIndex := uciToIndex(c.startSquare)

			b := FENtoBoard(c.fen)

			// act
			got := b.kingMoves(startIndex)

			// assert
			gotMoves := make([]string, 0, len(got))
			for _, gotMove := range got {
				gotMoves = append(gotMoves, indexToSquare(gotMove))
			}
			sort.Strings(gotMoves)
			sort.Strings(c.want)
			sort.Ints(got)

			if !reflect.DeepEqual(c.want, gotMoves) {
				t.Errorf("want: %v got: %v (%v)", c.want, gotMoves, got)
			}
		})
	}
}

func TestBoard_LegalMoves(t *testing.T) {
	cases := []struct {
		fen  string
		want []string
	}{
		{
			fen:  "8/8/8/8/8/7p/6N1/R4K1k b - - 1 1",
			want: []string{"h2", "hxg2+", "Kh2"},
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %s", c.fen, strings.Join(c.want, " ")), func(t *testing.T) {
			// arrange
			b := FENtoBoard(c.fen)

			// act
			legalMoves := b.legalMoves()

			// assert
			var got []string
			for _, move := range legalMoves {
				from, to := move.from, move.to
				uciMove := fmt.Sprintf("%s%s", indexToSquare(from), indexToSquare(to))
				san := b.UCItoSAN(uciMove)
				got = append(got, san)
			}

			sort.Strings(got)
			sort.Strings(c.want)

			if !reflect.DeepEqual(c.want, got) {
				t.Errorf("want: %v got: %v", c.want, got)
			}
		})
	}
}
