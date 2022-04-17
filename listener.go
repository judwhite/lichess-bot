package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
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
	gameFinished     bool
	gameLikelyDraw   int

	playingMtx sync.Mutex
	input      chan<- string
	output     <-chan string

	chatPlayerRoomNoTalking    bool
	chatSpectatorRoomNoTalking bool

	challengeQueue api.Challenges

	botQueueMtx      sync.Mutex
	botQueue         *api.BotQueue
	challengePending bool
	declined         chan api.Challenge
	accepted         chan api.GameEventInfo
}

func New(input chan<- string, output <-chan string) *Listener {
	l := Listener{
		input:    input,
		output:   output,
		declined: make(chan api.Challenge, 512),
		accepted: make(chan api.GameEventInfo, 512),
	}
	input <- "uci"

	go func() {
		botQueue, err := api.StreamBots()
		if err != nil {
			log.Printf("ERR: %v", err)
		}

		l.botQueueMtx.Lock()
		l.botQueue = botQueue
		l.botQueueMtx.Unlock()

		l.challengeBot()
	}()

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

			l.playingMtx.Lock()
			if l.activeGameID != "" {
				// TODO: abort game
				fmt.Printf("%s ??? You're already playing a game. Abort one!\n", ts())
				l.playingMtx.Unlock()
				return
			}
			l.resetGame()
			l.activeGameID = game.ID
			l.playingMtx.Unlock()

			l.accepted <- game

			go l.StreamGame(game.GameID, game.Opponent)
		} else if event.Type == "gameFinish" {
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}
			l.playingMtx.Lock()
			if l.activeGameID == gameEvent.Game.ID {
				l.gameFinished = true
			}
			l.playingMtx.Unlock()
		} else if event.Type == "challengeCanceled" {
			// TODO: remove from queue
		} else if event.Type == "challengeDeclined" {
			var challengeEvent api.ChallengeEvent
			if err := json.Unmarshal(ndjson, &challengeEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}

			c := challengeEvent.Challenge
			if c.Challenger.ID != botID {
				return
			}

			fmt.Printf("DEBUG: sending decline chan msg\n")
			l.declined <- c
			fmt.Printf("DEBUG: decline chan msg sent\n")
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

func (l *Listener) StreamGame(gameID string, opp api.Opponent) {
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
				l.playingMtx.Lock()
				l.resetGame()
				l.playingMtx.Unlock()
				fmt.Printf("not your game %s vs %s\n", game.White.ID, game.Black.ID)
				return
			}

			l.playingMtx.Lock()
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

			l.playingMtx.Unlock()
		} else if event.Type == "gameState" {
			if err := json.Unmarshal(ndjson, &state); err != nil {
				log.Fatal(err)
			}

			if state.WhiteDraw || state.BlackDraw {
				fmt.Printf("*** wdraw: %v bdraw: %v\n", state.WhiteDraw, state.BlackDraw)
			}

			if state.Winner != "" {
				var color string
				if playerNumber == 0 {
					color = "white"
				} else if playerNumber == 1 {
					color = "black"
				}

				fmt.Printf("winner: %s rated: %v our_color: %s\n", state.Winner, rated, color)
				if !rated && state.Winner != color && state.Winner != "" {
					const room = "player"
					const text = "Good game. Want to play rated?"
					if err := api.Chat(gameID, room, text); err != nil {
						fmt.Printf("*** ERR: api.Chat: %v\n", err)
					}
				}
			}
		} else if event.Type == "chatLine" {
			l.handleChat(gameID, ndjson)
			return
		} else {
			fmt.Printf("%s *** unhandled event type: '%s'\n", ts(), event.Type)
			return
		}

		if l.gameFinished {
			fmt.Printf("GAME FINISHED: %s\n", string(ndjson))
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
			} else if strings.Contains(item, " eval ") {
				if strings.Contains(item, "eval 0.00") {
					l.gameLikelyDraw++
				} else {
					l.gameLikelyDraw = 0
				}
			}
		}

		if bestmove != "" {
			if err := api.PlayMove(gameID, bestmove, l.gameLikelyDraw > 10); err != nil {
				// '{"error":"Not your turn, or game already over"}'
				// TODO: we should handle the opponent resigning, flagging or aborting while we're thinking
				fmt.Printf("*** ERR: api.PlayMove: %v\n", err)

				l.playingMtx.Lock()
				l.resetGame()
				l.playingMtx.Unlock()
				return
			}

			fmt.Printf("%s game: %s move: %s\n", ts(), gameID, bestmove)

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

	fmt.Printf("%s start game '%s' stream playing %s (%d)\n", ts(), gameID, opp.Username, opp.Rating)
	if err := api.ReadStream(endpoint, handler); err != nil {
		log.Printf("ERR: StreamGame: %v\n", err)
	}
}

func (l *Listener) resetGame() {
	l.gamePlayerNumber = -1
	l.activeGameID = ""
	l.gameGaveTime = false
	l.gameOpponent = api.Player{}
	l.chatSpectatorRoomNoTalking = false
	l.chatPlayerRoomNoTalking = false
	l.gameLikelyDraw = 0
	l.gameFinished = false
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

	// don't play without increment
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

type BannedBots struct {
	Banned []BannedBot `json:"banned"`
}

type BannedBot struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (l *Listener) challengeBot() {
	l.botQueueMtx.Lock()
	q := l.botQueue
	bots := make([]*api.BotInfo, len(q.Bots))
	copy(bots, q.Bots)
	l.botQueueMtx.Unlock()

	var banned BannedBots
	b, err := ioutil.ReadFile("banned.json")
	if err == nil {
		if err := json.Unmarshal(b, &banned); err != nil {
			log.Fatal(err)
		}
	}

	save := func() {
		b, err := json.MarshalIndent(banned, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		if err := ioutil.WriteFile("banned.json", b, 0644); err != nil {
			log.Fatal(err)
		}

		for i := 0; i < len(bots); i++ {
			bot := bots[i].User
			ban := false
			for j := 0; j < len(banned.Banned); j++ {
				badBot := banned.Banned[j]
				if strings.EqualFold(bot.ID, badBot.ID) {
					ban = true
					break
				}
			}

			if ban {
				bots = append(bots[:i], bots[i+1:]...)
				i--
				continue
			}
		}
	}
	save()

	for i := 0; i < len(bots); i++ {
		bot := bots[i]
		if bot.User.Perfs["bullet"].Rating < 2200 {
			bots = append(bots[:i], bots[i+1:]...)
			i--
			continue
		}
	}

	sort.Slice(bots, func(i, j int) bool {
		return bots[i].User.Perfs["bullet"].Rating > bots[j].User.Perfs["bullet"].Rating
	})

	for i := 0; i < len(bots); i++ {
		fmt.Printf("%3d. %s (%d)\n", i+1, bots[i].User.Username, bots[i].User.Perfs["bullet"].Rating)
	}

	// wait for any pending games to start
	time.Sleep(5000 * time.Millisecond)

	for {
		rand.Shuffle(len(bots), func(i, j int) {
			bots[i], bots[j] = bots[j], bots[i]
		})

		for i := 0; i < len(bots); i++ {
			bot := bots[i]

			l.playingMtx.Lock()
			isBusy := l.activeGameID != "" || l.challengePending
			hasChallenges := len(l.challengeQueue) != 0

			if isBusy || hasChallenges {
				l.playingMtx.Unlock()
				time.Sleep(1000 * time.Millisecond)
				i--
				continue
			}
			l.playingMtx.Unlock()

			fmt.Printf("%s total_bots: %d. next challenge in ", ts(), len(bots))
			for i := 5; i >= 1; i-- {
				fmt.Printf("%d ", i)
				time.Sleep(1 * time.Second)
			}
			fmt.Printf("\n")

			// Send the challenge
			resp := l.challenge(bot.User.ID, true, 60, 1, "random")
			if resp.Busy {
				i--
				continue
			}

			if resp.DailyLimit {
				return
			}

			if resp.CreateChallengeErr != nil {
				banned.Banned = append(banned.Banned, BannedBot{ID: bot.User.ID, Reason: err.Error()})
				save()
				i--
				continue
			}

			if resp.DeclineReason != "" {
				bot.LastDecline = time.Now()
				banned.Banned = append(banned.Banned, BannedBot{ID: bot.User.ID, Reason: resp.DeclineReason})
				save()
				i--
				continue
			}

			if resp.Timeout {
				bot.LastTimeout = time.Now()
				banned.Banned = append(banned.Banned, BannedBot{ID: bot.User.ID, Reason: "soft-ban; timeout"})
				save()
				i--
				continue
			}
		}
	}
}

type TryChallengeResponse struct {
	Busy               bool
	DailyLimit         bool
	CreateChallengeErr error
	DeclineReason      string
	Timeout            bool
	Accepted           bool
}

func (l *Listener) challenge(userID string, rated bool, limit, increment int, color string) TryChallengeResponse {
	l.playingMtx.Lock()
	isBusy := l.activeGameID != "" || l.challengePending
	hasChallenges := len(l.challengeQueue) != 0

	if isBusy || hasChallenges {
		l.playingMtx.Unlock()
		time.Sleep(1000 * time.Millisecond)
		return TryChallengeResponse{Busy: true}
	}

	l.challengePending = true
	l.playingMtx.Unlock()

	defer func() {
		l.playingMtx.Lock()
		l.challengePending = false
		l.playingMtx.Unlock()
	}()

	fmt.Printf("%s sending challenge to %s...\n", ts(), userID)

	challengeID, err := api.CreateChallenge(userID, rated, limit, increment, color, "standard")
	if err != nil {
		if strings.Contains(err.Error(), "429") {
			fmt.Printf("%s outgoing challenge limit exceeded for the day\n", ts())
			return TryChallengeResponse{DailyLimit: true}
		}

		log.Printf("%s ERR: CreateChallenge: %v\n", ts(), err)
		return TryChallengeResponse{CreateChallengeErr: err}
	}

	fmt.Printf("%s challenge sent to %s (id: %s). waiting 10s for response\n", ts(), userID, challengeID)

	timer := time.NewTimer(10 * time.Second)
	for {
		select {
		case c := <-l.declined:
			fmt.Printf("%s %s declined challenge (id: %s): '%s'\n", ts(), c.DestUser.ID, c.ID, c.DeclineReason)
			if c.ID == challengeID {
				if !timer.Stop() {
					<-timer.C
				}
				return TryChallengeResponse{DeclineReason: c.DeclineReason}
			}
		case c := <-l.accepted:
			fmt.Printf("%s %s accepted challenge (id: %s, pending_id: %s)\n", ts(), c.Opponent.ID, c.ID, challengeID)
			if c.ID == challengeID {
				if !timer.Stop() {
					<-timer.C
				}
				return TryChallengeResponse{Accepted: true}
			}
		case <-timer.C:
			fmt.Printf("%s ... challenge request to %s timed out.\n", ts(), userID)
			if challengeID != "" {
				if err := api.CancelChallenge(challengeID); err != nil {
					log.Printf("%s ERR: %v\n", ts(), err)
				}
			}

			return TryChallengeResponse{Timeout: true}
		}
	}
}

func (l *Listener) processChallengeQueue() {
	var lastWaitingPrint time.Time
	for {
		l.playingMtx.Lock()
		isBusy := l.activeGameID != "" || l.challengePending
		hasChallenges := len(l.challengeQueue) != 0
		l.playingMtx.Unlock()

		if isBusy || !hasChallenges {
			if !isBusy && time.Since(lastWaitingPrint) >= 30*time.Second {
				fmt.Printf("%s accepting challenges\n", ts())
				lastWaitingPrint = time.Now()
			}
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		fmt.Printf("%s checking challenge queue\n", ts())

		l.playingMtx.Lock()
		if l.challengePending || l.activeGameID != "" {
			l.playingMtx.Unlock()
			continue
		}
		sort.Sort(l.challengeQueue)
		for i := 0; i < len(l.challengeQueue); i++ {
			c := l.challengeQueue[i]
			if err := api.AcceptChallenge(c.ID); err != nil {
				log.Printf("ERR: %s %v\n", c.ID, err)
				l.challengeQueue = append(l.challengeQueue[:i], l.challengeQueue[i+1:]...)
				i--
				continue
			}
			l.challengeQueue = append(l.challengeQueue[:i], l.challengeQueue[i+1:]...)
			break
		}
		l.playingMtx.Unlock()

		time.Sleep(5 * time.Second)
	}
}

func (l *Listener) handleChat(gameID string, ndjson []byte) {
	var chat api.ChatLine
	if err := json.Unmarshal(ndjson, &chat); err != nil {
		fmt.Printf("%s ERR: chatLine: %v\n", ts(), err)
	}
	if strings.ToLower(chat.Username) == botID {
		return
	}
	if l.gameOpponent.Name != chat.Username {
		return
	}

	if chat.Room == "player" {
		if !l.chatPlayerRoomNoTalking {
			l.chatPlayerRoomNoTalking = true
			go func() {
				time.Sleep(2 * time.Second)
				if err := api.Chat(gameID, chat.Room, "No talking."); err != nil {
					fmt.Printf("%s ERR: api.Chat: %v\n", ts(), err)
				}
			}()
		}
	} else if chat.Room == "spectator" {
		if !l.chatSpectatorRoomNoTalking {
			l.chatSpectatorRoomNoTalking = true
			go func() {
				time.Sleep(2 * time.Second)
				if err := api.Chat(gameID, chat.Room, "No talking."); err != nil {
					fmt.Printf("%s ERR: api.Chat: %v\n", ts(), err)
				}
			}()
		}
	}
}
