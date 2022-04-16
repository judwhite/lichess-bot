package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"trollfish-lichess/api"
)

const botID = "trollololfish"
const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

type Listener struct {
	activeGameID     string
	gamePlayerNumber int
	gameGaveTime     bool
	gameOpponent     api.Player

	playingMtx sync.Mutex
	playing    map[string]any
	input      chan<- string
	output     <-chan string

	challengeQueue api.Challenges
}

func New(input chan<- string, output <-chan string) *Listener {
	l := Listener{
		playing: make(map[string]any),
		input:   input,
		output:  output,
	}
	input <- "uci"
	return &l
}

func (l *Listener) Events() error {
	handler := func(ndjson []byte) {
		var event api.Event

		if err := json.Unmarshal(ndjson, &event); err != nil {
			log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
		}

		if event.Type == "challenge" {
			var challengeEvent api.ChallengeEvent
			if err := json.Unmarshal(ndjson, &challengeEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}

			c := challengeEvent.Challenge

			switch c.Status {
			case "created":
				if err := l.QueueChallenge(c); err != nil {
					log.Printf("ERR: %v\n", err)
				}
			default:
				log.Printf("TODO: Challenge status '%s' unhandled", c.Status)
			}
		} else if event.Type == "gameStart" {
			fmt.Printf("%s gameStart: %s\n", ts(), ndjson)
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}
			game := gameEvent.Game
			fmt.Printf("%s %#v\n", ts(), game)
			go l.StreamGame(game.GameID)
		} else if event.Type == "gameFinish" {
			fmt.Printf("%s gameFinish: %s\n", ts(), ndjson)
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}
		} else {
			fmt.Printf("%s *** UNHANDLED EVENT: %s\n", ts(), ndjson)
		}
	}

	go l.processChallengeQueue()

	if err := api.ReadStream("https://lichess.org/api/stream/event", handler); err != nil {
		return err
	}

	return nil
}

func (l *Listener) StreamGame(gameID string) {
	l.playingMtx.Lock()
	if _, ok := l.playing[gameID]; ok {
		l.playingMtx.Unlock()
		return
	}
	l.playing[gameID] = struct{}{}
	l.playingMtx.Unlock()

	defer func() {
		l.playingMtx.Lock()
		delete(l.playing, gameID)
		l.playingMtx.Unlock()
	}()

	endpoint := fmt.Sprintf("https://lichess.org/api/bot/game/stream/%s", gameID)

	playerNumber := -1
	rated := true

	handler := func(ndjson []byte) {
		var event api.Event
		if err := json.Unmarshal(ndjson, &event); err != nil {
			log.Fatal(err)
		}

		var state api.State

		if event.Type == "gameFull" {
			// full game
			var game api.GameFull
			if err := json.Unmarshal(ndjson, &game); err != nil {
				log.Fatal(err)
			}

			state = game.State

			if game.State.Status != "started" {
				return
			}

			rated = game.Rated

			var opp api.Player
			if game.White.ID == botID {
				playerNumber = 0
				opp = game.Black
			} else if game.Black.ID == botID {
				playerNumber = 1
				opp = game.White
			} else {
				fmt.Printf("not your game %s vs %s\n", game.White.ID, game.Black.ID)
				return
			}

			play := true
			l.playingMtx.Lock()
			if l.activeGameID == "" {
				var rated string
				if game.Rated {
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

				l.resetGame()
				l.activeGameID = gameID
				l.gamePlayerNumber = playerNumber
				l.gameOpponent = opp

				if game.Rated {
					l.input <- "setoption name PlayBad value false"
				} else {
					l.input <- "setoption name PlayBad value true"
				}

				if game.Rated && opp.Title == "BOT" {
					l.input <- "setoption name StartAgro value true"
				} else {
					l.input <- "setoption name StartAgro value false"
				}

				l.input <- "ucinewgame"
			} else if l.activeGameID != gameID {
				play = false
			}
			l.playingMtx.Unlock()

			if !play {
				return
			}
		} else if event.Type == "gameState" {
			if err := json.Unmarshal(ndjson, &state); err != nil {
				log.Fatal(err)
			}

			var color string
			if playerNumber == 0 {
				color = "white"
			} else if playerNumber == 1 {
				color = "black"
			}

			if state.Winner != "" {
				fmt.Printf("gameState: %s\n", ndjson)
				fmt.Printf("winner: %s rated: %v color: %s\n", state.Winner, rated, color)
				if !rated && state.Winner != color && state.Winner != "" {
					const room = "player"
					const text = "Good game. Want to play rated?"
					if err := api.Chat(gameID, room, text); err != nil {
						fmt.Printf("*** ERR: api.PlayMove: %v\n", err)
					}
				}
			}
		} else {
			fmt.Printf("%s *** unhandled event type: '%s'\n", ts(), event.Type)
			return
		}

		if state.Status != "started" {
			l.playingMtx.Lock()
			l.resetGame()
			l.playingMtx.Unlock()
			return
		}

		var opponentTime, ourTime time.Duration
		if l.gamePlayerNumber == 0 {
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
		if len(moves)%2 != l.gamePlayerNumber {
			fmt.Printf("%s waiting for opponent...\n", ts())
			return
		}

		pos := fmt.Sprintf("position fen %s moves %s", startPosFEN, state.Moves)
		goCmd := fmt.Sprintf("go wtime %d winc %d btime %d binc %d",
			state.WhiteTime, state.WhiteInc,
			state.BlackTime, state.BlackInc,
		)

		//fmt.Printf("%s\n%s\n", pos, goCmd)

		l.input <- pos
		l.input <- goCmd

		fmt.Printf("%s thinking...\n", ts())

		var bestmove string
		for item := range l.output {
			if strings.HasPrefix(item, "bestmove ") {
				p := strings.Split(item, " ")
				bestmove = p[1]
				l.input <- "stop"
				break
			}
		}

		fmt.Printf("%s move=%s gameID=%s\n", ts(), bestmove, gameID)

		if bestmove != "" {
			if err := api.PlayMove(gameID, bestmove); err != nil {
				// '{"error":"Not your turn, or game already over"}'
				// TODO: we should handle the opponent resigning, flagging or aborting while we're thinking
				fmt.Printf("*** ERR: api.PlayMove: %v\n", err)

				// read the incantation for 'end game'
				l.playingMtx.Lock()
				l.resetGame()
				l.playingMtx.Unlock()
				return
			}

			if l.gameGaveTime {
				fmt.Printf("%s our_time: %v opp_time: %v gave_time: %v\n", ts(), ourTime, opponentTime, l.gameGaveTime)
			} else {
				fmt.Printf("%s our_time: %v opp_time: %v\n", ts(), ourTime, opponentTime)
			}

			if opponentTime < 15*time.Second && ourTime > opponentTime && !l.gameGaveTime && l.gameOpponent.Title != "BOT" {
				l.gameGaveTime = true
				fmt.Printf("%s *** attempting to give time!\n", ts())
				for i := 0; i < 6; i++ {
					go func() {
						if err := api.AddTime(gameID, 15); err != nil {
							log.Printf("AddTime: %v\n", err)
						}
					}()
				}
			}
		}
	}

	fmt.Printf("%s start game stream %s\n", ts(), gameID)
	if err := api.ReadStream(endpoint, handler); err != nil {
		log.Printf("ERR: StreamGame: %v\n", err)
	}
}

func (l *Listener) resetGame() {
	l.gamePlayerNumber = -1
	l.activeGameID = ""
	l.gameGaveTime = false
	l.gameOpponent = api.Player{}
}

func (l *Listener) QueueChallenge(c api.Challenge) error {
	c.InternalCreated = time.Now().UnixNano()
	opp := c.Challenger

	// ignore our own requests
	if opp.ID == botID {
		return nil
	}

	// only use standard initial position
	if c.InitialFEN != "" && c.InitialFEN != "startpos" {
		if err := api.DeclineChallenge(c.ID, "standard"); err != nil {
			return err
		}
		return nil
	}

	tc := c.TimeControl

	// standard; no variants e.g. Chess960
	if c.Variant.Key != "standard" {
		if err := api.DeclineChallenge(c.ID, "standard"); err != nil {
			return err
		}
		return nil
	}

	// no unlimited, correspondence, etc
	if tc.Type != "clock" {
		if err := api.DeclineChallenge(c.ID, "timeControl"); err != nil {
			return err
		}
		return nil
	}

	// bots wanting unrated can #SiO2
	if opp.Title == "BOT" {
		if tc.Increment == 0 {
			if err := api.DeclineChallenge(c.ID, "timeControl"); err != nil {
				return err
			}
			return nil
		}
	}

	// if below 1 minute, must include increment for human players
	if tc.Limit < 60 && tc.Increment == 0 {
		if err := api.DeclineChallenge(c.ID, "tooFast"); err != nil {
			return err
		}
		return nil
	}

	// longest game we accept is 5 minutes
	// if time is 1 minute or higher, max increment is 5s
	// below 1 minute we accept higher increments
	if tc.Limit > 300 || (tc.Increment > 5 && tc.Limit >= 60) {
		if err := api.DeclineChallenge(c.ID, "tooSlow"); err != nil {
			return err
		}
		return nil
	}

	// if we're already playing a game queue the challenge
	l.playingMtx.Lock()
	l.challengeQueue = append(l.challengeQueue, c)
	l.playingMtx.Unlock()

	return nil
}

func (l *Listener) processChallengeQueue() {
	for {
		l.playingMtx.Lock()
		isPlaying := l.activeGameID != ""
		hasChallenges := len(l.challengeQueue) != 0
		l.playingMtx.Unlock()

		if isPlaying || !hasChallenges {
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		l.playingMtx.Lock()
		sort.Sort(l.challengeQueue)
		for i := 0; i < len(l.challengeQueue); i++ {
			c := l.challengeQueue[i]
			if err := api.AcceptChallenge(c.ID); err != nil {
				log.Printf("ERR: %v\n", c.ID)
				l.challengeQueue = append(l.challengeQueue[:i], l.challengeQueue[i+1:]...)
				i--
				continue
			}
			break
		}
		l.playingMtx.Unlock()

		time.Sleep(5 * time.Second)
	}
}
