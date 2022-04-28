package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"trollfish-lichess/api"
	"trollfish-lichess/fen"
	"trollfish-lichess/polyglot"
)

type Game struct {
	sync.Mutex

	gameID       string
	playerNumber int
	rated        bool
	gaveTime     bool
	opponent     api.Player
	finished     bool
	likelyDraw   int

	chatPlayerRoomNoTalking    bool
	chatSpectatorRoomNoTalking bool

	input  chan<- string
	output <-chan string

	book            *polyglot.Book
	bookMovesPlayed int
	ponder          string
	pondering       bool
	ponderHits      int
	totalPonders    int
	humanEval       string
}

func NewGame(gameID string, input chan<- string, output <-chan string, book *polyglot.Book) *Game {
	return &Game{
		gameID:       gameID,
		playerNumber: -1,
		input:        input,
		output:       output,
		book:         book,
	}
}

func (g *Game) StreamGameEvents() {
	endpoint := fmt.Sprintf("https://lichess.org/api/bot/game/stream/%s", g.gameID)

	handler := func(ndjson []byte) bool {
		var event api.Event
		if err := json.Unmarshal(ndjson, &event); err != nil {
			log.Fatal(err)
		}

		switch event.Type {
		case "gameFull":
			g.handleGameFull(ndjson)
		case "gameState":
			g.handleGameState(ndjson)
		case "chatLine":
			g.handleChat(ndjson)
		default:
			fmt.Printf("%s *** unhandled event type: '%s'\n", ts(), event.Type)
		}

		return true
	}

	fmt.Printf("%s start game stream '%s'\n", ts(), g.gameID)
	if err := api.ReadStream(endpoint, handler); err != nil {
		log.Printf("ERR: StreamGame: %v\n", err)
	}
}

func (g *Game) Finish() {
	g.Lock()
	g.finished = true
	g.input <- "stop"
	if g.pondering {
		g.pondering = false
		g.ponder = ""
		// consume 'bestmove' from pondering, so we don't accidentally consume it later
		for line := range g.output {
			if strings.HasPrefix(line, "bestmove") {
				break
			}
		}
	}
	fmt.Printf("%d book move(s) played\n", g.bookMovesPlayed)
	fmt.Printf("%d/%d predictions played\n", g.ponderHits, g.totalPonders)
	g.Unlock()
}

func (g *Game) handleChat(ndjson []byte) {
	var chat api.ChatLine
	if err := json.Unmarshal(ndjson, &chat); err != nil {
		fmt.Printf("%s ERR: chatLine: %v\n", ts(), err)
	}
	fmt.Printf("%s CHAT: #%s <%s> %s\n", ts(), chat.Room, chat.Username, chat.Text)
	if strings.ToLower(chat.Username) == botID {
		return
	}
	if g.opponent.Name != chat.Username {
		return
	}

	msg := fmt.Sprintf("@%s No talking.", g.opponent.Name)
	if chat.Room == "player" {
		if !g.chatPlayerRoomNoTalking {
			g.chatPlayerRoomNoTalking = true
			go func() {
				time.Sleep(4 * time.Second)
				if err := api.Chat(g.gameID, chat.Room, msg); err != nil {
					fmt.Printf("%s ERR: api.Chat: %v\n", ts(), err)
				}
			}()
		}
	} else if chat.Room == "spectator" {
		if !g.chatSpectatorRoomNoTalking {
			g.chatSpectatorRoomNoTalking = true
			go func() {
				time.Sleep(4 * time.Second)
				if err := api.Chat(g.gameID, chat.Room, msg); err != nil {
					fmt.Printf("%s ERR: api.Chat: %v\n", ts(), err)
				}
			}()
		}
	}
}

func (g *Game) handleGameFull(ndjson []byte) {
	var game api.GameFull
	if err := json.Unmarshal(ndjson, &game); err != nil {
		log.Fatal(err)
	}

	state := game.State

	if state.Status != "started" {
		return
	}

	if game.White.ID == botID {
		g.playerNumber = 0
		g.opponent = game.Black
	} else if game.Black.ID == botID {
		g.playerNumber = 1
		g.opponent = game.White
	} else {
		log.Fatalf("not your game %s vs %s\n", game.White.ID, game.Black.ID)
	}

	g.rated = game.Rated

	var rated string
	if g.rated {
		rated = "Rated"
	} else {
		rated = "Unrated"
	}

	initialTime := time.Duration(game.Clock.Initial) * time.Millisecond
	increment := time.Duration(game.Clock.Increment) * time.Millisecond
	timeControl := fmt.Sprintf("%v+%v", initialTime, increment)

	fmt.Printf("%s *** New game! %s (%d) vs. %s (%d) %s %s\n",
		ts(),
		game.White.Name, game.White.Rating,
		game.Black.Name, game.Black.Rating,
		rated,
		timeControl,
	)

	if game.Rated {
		g.input <- "setoption name PlayBad value false"
	} else {
		g.input <- "setoption name PlayBad value true"
	}

	if game.Rated && g.opponent.Title == "BOT" {
		g.input <- "setoption name StartAgro value true"
	} else {
		g.input <- "setoption name StartAgro value false"
	}

	g.input <- "ucinewgame"

	g.playMove(ndjson, state)
}

func (g *Game) handleGameState(ndjson []byte) {
	var state api.State
	if err := json.Unmarshal(ndjson, &state); err != nil {
		log.Fatal(err)
	}

	if state.Winner != "" {
		var color string
		if g.playerNumber == 0 {
			color = "white"
		} else if g.playerNumber == 1 {
			color = "black"
		}

		fmt.Printf("winner: %s rated: %v our_color: %s\n", state.Winner, g.rated, color)
		if !g.rated && state.Winner != color && state.Winner != "" {
			const room = "player"
			const text = "Good game. Want to play rated?"
			if err := api.Chat(g.gameID, room, text); err != nil {
				fmt.Printf("*** ERR: api.Chat: %v\n", err)
			}
		}

		return
	}

	g.playMove(ndjson, state)
}

func (g *Game) playMove(ndjson []byte, state api.State) {
	g.Lock()
	if g.finished {
		g.Unlock()
		fmt.Printf("GAME FINISHED: %s\n", string(ndjson))
		return
	}
	g.Unlock()

	var opponentTime, ourTime time.Duration
	if g.playerNumber == 0 {
		ourTime = time.Duration(state.WhiteTime) * time.Millisecond
		opponentTime = time.Duration(state.BlackTime) * time.Millisecond
	} else {
		ourTime = time.Duration(state.BlackTime) * time.Millisecond
		opponentTime = time.Duration(state.WhiteTime) * time.Millisecond
	}

	moves := strings.Split(state.Moves, " ")
	if len(moves) == 1 && len(moves[0]) == 0 {
		moves = nil
	}
	if len(moves)%2 != g.playerNumber {
		fmt.Printf("%s waiting for opponent...\n", ts())
		return
	}

	var ponderHit bool
	var board fen.Board

	if len(moves) > 1 {
		opponentMoveUCI := moves[len(moves)-1]
		board.Moves(moves[:len(moves)-1]...)
		playedSAN := board.UCItoSAN(opponentMoveUCI)

		if g.ponder != "" && g.pondering {
			predictedSAN := board.UCItoSAN(g.ponder)
			fmt.Printf("%s played: %s predicted: %s\n", ts(), playedSAN, predictedSAN)
			if g.ponder == opponentMoveUCI {
				g.ponderHits++
				ponderHit = true
			}
			g.ponder = ""
		} else {
			fmt.Printf("%s played: %s\n", ts(), playedSAN)
		}
		board.Moves(opponentMoveUCI)
	} else {
		board.Moves(moves...)
	}

	var bestMove string

	// check book
	bookMoves, ok := g.book.Get(board.FEN())

	if ok {
		n := rand.Intn(len(bookMoves)) // TODO: use freq field
		bookMove := bookMoves[n]
		bestMove = bookMove.UCIMove

		fmt.Printf("!!! ^^^ !!! ^^^ %s %s %s came from book\n", board.FEN(), board.UCItoSAN(bestMove), bestMove)
		g.bookMovesPlayed++
	} else {
		if ponderHit {
			g.input <- "ponderhit"
			g.pondering = false
		} else {
			g.input <- "stop"
			if g.pondering {
				g.pondering = false
				// consume 'bestmove' from pondering, so we don't accidentally consume it later
				for line := range g.output {
					if strings.HasPrefix(line, "bestmove") {
						break
					}
				}
			}
			var pos string
			if state.Moves == "" {
				pos = fmt.Sprintf("position fen %s", startPosFEN)
			} else {
				pos = fmt.Sprintf("position fen %s moves %s", startPosFEN, state.Moves)
			}

			goCmd := fmt.Sprintf("go wtime %d winc %d btime %d binc %d",
				state.WhiteTime, state.WhiteInc,
				state.BlackTime, state.BlackInc,
			)

			g.input <- pos
			g.input <- goCmd
		}

		start := time.Now()

		fmt.Printf("%s thinking...\n", ts())

		for item := range g.output {
			// bestmove and ponder
			if strings.HasPrefix(item, "bestmove") {
				p := strings.Split(item, " ")
				bestMove = p[1]
				for i := 2; i < len(p)-1; i++ {
					if p[i] == "ponder" {
						g.ponder = p[i+1]
						g.totalPonders++

						var pos string
						if state.Moves == "" {
							pos = fmt.Sprintf("position fen %s moves %s %s", startPosFEN, bestMove, g.ponder)
						} else {
							pos = fmt.Sprintf("position fen %s moves %s %s %s", startPosFEN, state.Moves, bestMove, g.ponder)
						}

						var goCmd string
						elapsed := int(time.Since(start).Milliseconds())
						if g.playerNumber == 0 {
							goCmd = fmt.Sprintf("go ponder wtime %d winc %d btime %d binc %d",
								state.WhiteTime-elapsed, state.WhiteInc,
								state.BlackTime, state.BlackInc,
							)
						} else {
							goCmd = fmt.Sprintf("go ponder wtime %d winc %d btime %d binc %d",
								state.WhiteTime, state.WhiteInc,
								state.BlackTime-elapsed, state.BlackInc,
							)
						}

						g.input <- pos
						g.input <- goCmd

						g.pondering = true
					} else if p[i] == "eval" {
						g.humanEval = p[i+1]
						if g.humanEval == "0.00" {
							g.likelyDraw++
						} else {
							g.likelyDraw = 0
						}
					}
				}
				break
			}
		}
	}

	offerDraw := g.likelyDraw > 12 && board.FullMove > 40 && board.HalfmoveClock > 20
	if err := g.sendMoveToServer(bestMove, offerDraw); err != nil {
		// '{"error":"Not your turn, or game already over"}'
		// TODO: we should handle the opponent resigning, flagging or aborting while we're thinking
		fmt.Printf("*** ERR: api.PlayMove: %v: %s\n", err, string(ndjson))

		g.Finish()
		return
	}

	g.maybeGiveTime(ourTime, opponentTime)

	bestMoveSAN := board.UCItoSAN(bestMove)
	fmt.Printf("%s game: %s (%d) our_time: %6v opp_time: %6v move: %s eval: %s\n",
		ts(), g.opponent.Name, g.opponent.Rating, ourTime, opponentTime, bestMoveSAN, g.humanEval)
}

func (g *Game) sendMoveToServer(bestMove string, offerDraw bool) error {
	if bestMove == "" {
		return nil
	}

	if err := api.PlayMove(g.gameID, bestMove, offerDraw); err != nil {
		return err
	}

	return nil
}

func (g *Game) maybeGiveTime(ourTime, opponentTime time.Duration) {
	// add time for human players :D
	if opponentTime < 15*time.Second && ourTime > opponentTime && !g.gaveTime && g.opponent.Title != "BOT" {
		g.gaveTime = true
		fmt.Printf("%s *** attempting to give time!\n", ts())
		for i := 0; i < 6; i++ {
			go func() {
				if err := api.AddTime(g.gameID, 15); err != nil {
					log.Printf("AddTime: %v\n", err)
				}
			}()
		}
	}
}

func (l *Listener) Playing() bool {
	l.activeGameMtx.Lock()
	defer l.activeGameMtx.Unlock()
	if l.activeGame == nil {
		return false
	}
	return !l.activeGame.finished
}
