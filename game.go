package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"trollfish-lichess/api"
	"trollfish-lichess/fen"
	"trollfish-lichess/yamlbook"
)

type Game struct {
	sync.Mutex

	gameID       string
	playerNumber int
	rated        bool
	gaveTime     bool
	opponent     api.Player
	finished     bool

	chatPlayerRoomNoTalking    bool
	chatSpectatorRoomNoTalking bool

	input  chan<- string
	output <-chan string

	book            *yamlbook.Book
	bookMovesPlayed int
	ponder          string
	pondering       bool
	ponderHits      int
	totalPonders    int
	humanEval       string
	lastStateEvent  time.Time

	consecutiveFullMovesWithZeroEval int

	moves      []SavedMove
	seenPos    map[string]int
	playerBook map[string]MoveChances
}

type SavedMove struct {
	FEN     string
	MoveSAN string
}

func NewGame(gameID string, input chan<- string, output <-chan string, book *yamlbook.Book) *Game {
	return &Game{
		gameID:       gameID,
		playerNumber: -1,
		input:        input,
		output:       output,
		book:         book,
		seenPos:      make(map[string]int),
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

func (g *Game) IsFinished() bool {
	g.Lock()
	defer g.Unlock()
	return g.finished
}

func (g *Game) Finish() {
	g.Lock()
	defer g.Unlock()

	if g.finished {
		return
	}
	g.stopPondering()
	g.finished = true

	fp, err := os.OpenFile("recent.epd", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = fp.Sync()
		_ = fp.Close()
	}()

	var sb strings.Builder
	for i, move := range g.moves {
		b := fen.FENtoBoard(move.FEN)

		ourMove := i%2 == g.playerNumber
		_, found := g.book.GetAll(move.FEN)
		if !found && ourMove && b.FullMove <= 15 {
			_, err = fmt.Fprintf(fp, "- fen: %s\n", fen.Key(move.FEN))
			if err != nil {
				log.Fatal(err)
			}
		}

		if b.ActiveColor == fen.WhitePieces {
			sb.WriteByte('\n')
			sb.WriteString(fmt.Sprintf("%3d. %7s", b.FullMove, move.MoveSAN))
		} else {
			sb.WriteString(fmt.Sprintf("  %7s", move.MoveSAN))
		}
	}
	sb.WriteByte('\n')

	sb.WriteString(fmt.Sprintf("%d book move(s) played\n", g.bookMovesPlayed))
	sb.WriteString(fmt.Sprintf("%d/%d predictions played\n", g.ponderHits, g.totalPonders))

	fmt.Print(sb.String())
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

	//msg := fmt.Sprintf("@%s No talking.", g.opponent.Name)
	/*if chat.Room == "player" {
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
	}*/
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

	g.waitReady()

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

	g.waitReady()

	g.playMove(ndjson, state)
}

func (g *Game) handleGameState(ndjson []byte) {
	g.lastStateEvent = time.Now()

	var state api.State
	if err := json.Unmarshal(ndjson, &state); err != nil {
		log.Fatal(err)
	}
	state.MessageReceived = time.Now()

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

	if state.Status != "started" {
		fmt.Printf("%s state.Status: '%s'\n", ts(), state.Status)
	}

	g.playMove(ndjson, state)
}

func (g *Game) playMove(ndjson []byte, state api.State) {
	start := time.Now()

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
	if len(moves) > 0 && len(moves) == len(g.moves) {
		fmt.Printf("%s *** Duplicate message??? %s\n", ts(), ndjson)
		return
	}

	var ponderHit bool
	var board fen.Board

	if len(moves) > 1 {
		opponentMoveUCI := moves[len(moves)-1]
		board.Moves(moves[:len(moves)-1]...)
		playedSAN := board.UCItoSAN(opponentMoveUCI)

		g.storeMove(board.FEN(), playedSAN)

		if g.ponder != "" && g.pondering {
			predictedSAN := board.UCItoSAN(g.ponder)
			fmt.Printf("%s their move: %s predicted: %s\n", ts(), playedSAN, predictedSAN)
			if g.ponder == opponentMoveUCI {
				g.ponderHits++
				ponderHit = true
			}
		} else {
			fmt.Printf("%s their move: %s\n", ts(), playedSAN)
		}
		board.Moves(opponentMoveUCI)
	} else if len(moves) > 0 {
		opponentMoveUCI := moves[len(moves)-1]
		playedSAN := board.UCItoSAN(opponentMoveUCI)
		g.storeMove(board.FEN(), playedSAN)

		board.Moves(moves...)

		fmt.Printf("%s their move: %s\n", ts(), playedSAN)
	}

	g.ponder = ""

	var bestMove string

	// check book
	fenKey := board.FENKey()
	var bookMoveUCI, bookPonderUCI string
	var bookMoveCP, bookMoveMate int
	if g.playerBook != nil {
		moves, ok := g.playerBook[fenKey]
		if ok {
			bestMove := moves.BestMove()
			if bestMove != nil {
				bookMoveUCI = bestMove.MoveUCI
				bookPonderUCI = bestMove.PonderUCI
				bookMoveCP, bookMoveMate = 55555, 0

				// check book to get eval
				var bookMove2 *yamlbook.Move
				bookMove2, bookPonderUCI2 := g.book.BestMove(fenKey)
				if bookMove2 != nil && bookMove2.Move == bestMove.MoveSAN {
					bookMoveCP, bookMoveMate = bookMove2.CP, bookMove2.Mate
					bookPonderUCI = bookPonderUCI2
				}

				fmt.Printf("%s %s '%s' %s\n", ts(), bestMove.MoveSAN, fenKey, bestMove.GameText)
			}
		}
	}

	if board.FEN() != startPosFEN && bookMoveUCI == "" {
		var bookMove *yamlbook.Move
		bookMove, bookPonderUCI = g.book.BestMove(fenKey)
		if bookMove != nil {
			bookMoveUCI = bookMove.UCI()
			bookMoveCP, bookMoveMate = bookMove.CP, bookMove.Mate
		}
	}
	_, repetition := g.seenPos[fenKey]
	g.seenPos[fenKey] += 1
	if repetition {
		fmt.Printf("%s %s - REPETITONS: %d\n", ts(), fenKey, g.seenPos[fenKey])
		g.input <- "setoption name StartAgro value true"
	}

	if bookMoveUCI != "" && !repetition {
		bestMove = bookMoveUCI
		povMultiplier := iif(g.playerNumber == 0, 1, -1)
		g.humanEval = iif(bookMoveMate == 0, fmt.Sprintf("%0.2f", float64(bookMoveCP*povMultiplier)/100), fmt.Sprintf("M%d", bookMoveMate*povMultiplier))

		fmt.Printf("%s %s - BOOK MOVE: %s (%s), eval %s\n", ts(), board.FEN(), board.UCItoSAN(bestMove), bestMove, g.humanEval)
		g.bookMovesPlayed++

		if ponderHit {
			g.ponderHit()
			g.consumeBestMove()
		} else {
			g.stopPondering()
		}

		if bookPonderUCI != "" {
			g.ponderMove(bookPonderUCI, state, bestMove)
		}
	} else {
		if ponderHit {
			g.ponderHit()
		} else {
			g.stopPondering()

			var pos string
			if state.Moves == "" {
				pos = fmt.Sprintf("position startpos")
			} else {
				pos = fmt.Sprintf("position startpos moves %s", state.Moves)
			}

			goCmd := fmt.Sprintf("go wtime %d winc %d btime %d binc %d",
				state.WhiteTime, state.WhiteInc,
				state.BlackTime, state.BlackInc,
			)

			g.input <- pos
			g.input <- goCmd
		}

		fmt.Printf("%s thinking...\n", ts())

		for item := range g.output {
			if g.IsFinished() {
				return
			}

			// bestmove and ponder
			if strings.HasPrefix(item, "bestmove") {
				p := strings.Split(item, " ")
				bestMove = p[1]
				for i := 2; i < len(p)-1; i++ {
					if p[i] == "ponder" {
						g.ponderMove(p[i+1], state, bestMove)
					} else if p[i] == "eval" {
						g.humanEval = p[i+1]
						if g.humanEval == "0.00" {
							g.consecutiveFullMovesWithZeroEval++
						} else {
							g.consecutiveFullMovesWithZeroEval = 0
						}
					}
				}
				break
			}
		}
	}

	goForDirtyFlag := ourTime > opponentTime && opponentTime < 5*time.Second || ourTime > opponentTime*3/2
	tcHasIncrement := state.WhiteInc > 0 && state.BlackInc > 0
	gameIsEqual := g.consecutiveFullMovesWithZeroEval > 12 && board.FullMove > 40 && board.HalfmoveClock > 4
	offerDraw := gameIsEqual && tcHasIncrement && !goForDirtyFlag

	if tcHasIncrement && ourTime >= 30*time.Second {
		elapsed := time.Since(start)
		delta := 400*time.Millisecond - elapsed
		if delta > 0 {
			time.Sleep(delta)
		}
	}

	if g.IsFinished() {
		return
	}

	if err := g.sendMoveToServer(bestMove, offerDraw); err != nil {
		// '{"error":"Not your turn, or game already over"}'
		// TODO: we should handle the opponent resigning, flagging or aborting while we're thinking
		fmt.Printf("%s *** ERR: api.PlayMove: %v: %s\n", ts(), err, string(ndjson))

		g.Finish()
		return
	}

	g.maybeGiveTime(ourTime, opponentTime)

	bestMoveSAN := board.UCItoSAN(bestMove)
	tslbl := ts()
	fullFEN := board.FEN()
	fmt.Printf("%s game: %s (%d) | our_time: %6v opp_time: %6v | our_move: %s (%s) | eval: %s\n%s fen: %s\n",
		tslbl, g.opponent.Name, g.opponent.Rating, ourTime, opponentTime, bestMoveSAN, bestMove, g.humanEval,
		tslbl, fullFEN)

	g.storeMove(fullFEN, bestMoveSAN)
}

func (g *Game) storeMove(fenPOS, moveSAN string) {
	g.moves = append(g.moves, SavedMove{FEN: fenPOS, MoveSAN: moveSAN})
}

func (g *Game) ponderHit() {
	g.input <- "ponderhit"
	g.pondering = false
}

func (g *Game) stopPondering() {
	g.input <- "stop"
	if g.pondering {
		g.pondering = false
		g.consumeBestMove()
	}
}

func (g *Game) consumeBestMove() {
	// consume 'bestmove' from pondering, so we don't accidentally consume it later
	for line := range g.output {
		if strings.HasPrefix(line, "bestmove") {
			break
		}
	}
}

func (g *Game) ponderMove(ponderMoveUCI string, state api.State, playedMoveUCI string) {
	g.ponder = ponderMoveUCI
	g.totalPonders++

	var pos string
	if state.Moves == "" {
		pos = fmt.Sprintf("position startpos moves %s %s", playedMoveUCI, g.ponder)
	} else {
		pos = fmt.Sprintf("position startpos moves %s %s %s", state.Moves, playedMoveUCI, g.ponder)
	}

	var goCmd string
	elapsed := int(time.Since(state.MessageReceived).Milliseconds())
	whiteTime := state.WhiteTime - iif(g.playerNumber == 0, elapsed, 0)
	whiteTime = max(whiteTime, 50)
	blackTime := state.BlackTime - iif(g.playerNumber == 0, 0, elapsed)
	blackTime = max(blackTime, 50)

	goCmd = fmt.Sprintf("go ponder wtime %d winc %d btime %d binc %d",
		whiteTime, state.WhiteInc,
		blackTime, state.BlackInc,
	)

	g.input <- pos
	g.input <- goCmd

	g.pondering = true
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
	return !l.activeGame.IsFinished()
}

func (g *Game) waitReady() {
	g.input <- "isready"
	for line := range g.output {
		if line == "readyok" {
			break
		}
	}
}

func iif[T any](condition bool, ifTrue, ifFalse T) T {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func max(a, b int) int {
	return iif(a > b, a, b)
}
