package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"trollfish-lichess/analyze"
	"trollfish-lichess/api"
	"trollfish-lichess/yamlbook"
)

const botID = "trollololfish"
const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

const maxRating = 4000
const minRating = 2500

//const maxRating = 2300
//const minRating = 1800

type Listener struct {
	ctx context.Context

	book *yamlbook.Book

	activeGameMtx sync.Mutex
	activeGame    *Game

	challengeQueueMtx sync.Mutex
	challengeQueue    api.Challenges

	botQueueMtx sync.Mutex
	botQueue    *api.BotQueue

	challengePending bool
	declined         chan api.Challenge
	accepted         chan api.GameEventInfo
	onlyUser         string
	fenPos           string
	tc               TimeControl

	input  chan<- string
	output <-chan string
}

type TimeControl struct {
	Limit     int
	Increment int
}

func (tc *TimeControl) Parse(text string) error {
	const tcMsg = "-tc needs to be in the format mins+secs, ex: 3+2, 1+0, 15+10, etc"

	parts := strings.Split(text, "+")
	if len(parts) != 2 {
		return errors.New(tcMsg)
	}

	tcMins, err := strconv.Atoi(parts[0])
	if err != nil {
		return errors.New(tcMsg)
	}

	tcSecs, err := strconv.Atoi(parts[1])
	if err != nil {
		return errors.New(tcMsg)
	}

	if tcMins < 0 || tcSecs < 0 {
		return errors.New(tcMsg)
	}

	if tcMins >= 30 {
		tc.Limit = tcMins // ex, 30+0 = 30 seconds no increment
	} else {
		tc.Limit = tcMins * 60
	}
	tc.Increment = tcSecs

	return nil
}

func New(ctx context.Context, input chan<- string, output <-chan string, onlyUser, challenge string, tc TimeControl, fenPos string) *Listener {
	l := Listener{
		ctx:      ctx,
		input:    input,
		output:   output,
		declined: make(chan api.Challenge, 512),
		accepted: make(chan api.GameEventInfo, 512),
		onlyUser: strings.ToLower(onlyUser),
		fenPos:   fenPos,
		tc:       tc,
	}
	input <- "uci"
	input <- "setoption name Ponder value true"
	input <- fmt.Sprintf("setoption name SyzygyPath value %s", analyze.SyzygyPath)

	if err := l.importBook("book.yamlbook"); err != nil {
		log.Fatal(err)
	}
	if l.book != nil {
		fmt.Printf("book loaded, %d positions\n", l.book.PosCount())
	}

	if onlyUser == "" {
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
	}

	if challenge != "" {
		go func() {
			l.challenge(challenge, false, tc.Limit, tc.Increment, "random", fenPos)
		}()
	}

	return &l
}

func (l *Listener) importBook(filename string) error {
	fmt.Printf("%s loading book %s...\n", ts(), filename)
	ext := filepath.Ext(filename)

	var err error

	switch ext {
	case ".yamlbook":
		l.book, err = yamlbook.Load(filename)
	default:
		return fmt.Errorf("unknown book extension '%s'", ext)
	}
	if err != nil {
		return err
	}

	return nil
}

func (l *Listener) Events() error {
	handler := func(ndjson []byte) bool {
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
			g := gameEvent.Game
			game := NewGame(g.GameID, l.input, l.output, l.book)

			l.activeGameMtx.Lock()
			if l.activeGame != nil {
				// TODO: abort game
				if !l.activeGame.IsFinished() {
					fmt.Printf("%s ??? You're already playing a game. Abort one!\n", ts())
					l.activeGameMtx.Unlock()
					return true
				}
			}
			l.activeGame = game
			l.activeGameMtx.Unlock()

			go game.StreamGameEvents()

			l.accepted <- g
		} else if event.Type == "gameFinish" {
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}

			l.activeGameMtx.Lock()
			if l.activeGame != nil && l.activeGame.gameID == gameEvent.Game.ID {
				l.activeGame.Finish()
			}
			l.activeGameMtx.Unlock()
			return !l.Quit()
		} else if event.Type == "challengeCanceled" {
			// TODO: remove from queue
		} else if event.Type == "challengeDeclined" {
			var challengeEvent api.ChallengeEvent
			if err := json.Unmarshal(ndjson, &challengeEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}

			c := challengeEvent.Challenge
			if c.Challenger.ID == botID {
				l.declined <- c
			}
		} else {
			fmt.Printf("%s *** UNHANDLED EVENT: %s\n", ts(), ndjson)
		}

		return true
	}

	go l.processChallengeQueue()

	if err := api.ReadStream("https://lichess.org/api/stream/event", handler); err != nil {
		return err
	}

	return nil
}

func (l *Listener) QueueChallenge(c api.Challenge) error {
	c.InternalCreated = time.Now().UnixNano()
	opp := c.Challenger

	// ignore our own requests
	if opp.ID == botID {
		return nil
	}

	if l.onlyUser != "" {
		if !strings.EqualFold(c.Challenger.Name, l.onlyUser) && !strings.EqualFold(c.Challenger.Name, "bantercode") {
			if err := api.DeclineChallenge(c.ID, "later"); err != nil {
				return err
			}
			return nil
		}
	}

	// only use standard initial position
	if c.InitialFEN != "" && c.InitialFEN != "startpos" && !strings.EqualFold(c.Challenger.Name, l.onlyUser) {
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
	l.challengeQueueMtx.Lock()
	l.challengeQueue = append(l.challengeQueue, c)
	l.challengeQueueMtx.Unlock()

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

	// remove ourselves if we're in the list
	for i := 0; i < len(bots); i++ {
		if strings.EqualFold(bots[i].User.ID, botID) {
			bots = append(bots[:i], bots[i+1:]...)
			break
		}
	}

	l.botQueueMtx.Unlock()

	first := true

	var banned BannedBots
	b, err := ioutil.ReadFile("banned.json")
	if err == nil {
		if err := json.Unmarshal(b, &banned); err != nil {
			log.Fatal(err)
		}
	}

	// remove soft-bans
	for i := 0; i < len(banned.Banned); i++ {
		ban := banned.Banned[i]
		if strings.Contains(ban.Reason, "soft-ban") || ban.Reason == "I'm not accepting challenges at the moment." || ban.Reason == "This is not the right time for me, please ask again later." {
			banned.Banned = append(banned.Banned[:i], banned.Banned[i+1:]...)
			i--
			continue
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
		bullet := bot.User.Perfs["bullet"]
		bulletRating := bullet.Rating
		if bulletRating > maxRating || bulletRating < minRating || bullet.Provisional {
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
			if l.Quit() {
				return
			}

			bot := bots[i]

			l.activeGameMtx.Lock()
			l.challengeQueueMtx.Lock()

			isBusy := (l.activeGame != nil && !l.activeGame.finished) || l.challengePending
			hasChallenges := len(l.challengeQueue) != 0

			if isBusy || hasChallenges {
				l.activeGameMtx.Unlock()
				l.challengeQueueMtx.Unlock()

				time.Sleep(1000 * time.Millisecond)
				i--
				continue
			}
			l.activeGameMtx.Unlock()
			l.challengeQueueMtx.Unlock()

			fmt.Printf("%s total_bots: %d. next challenge in ", ts(), len(bots))
			for i := 8; i >= 1; i-- {
				if l.Quit() {
					return
				}

				fmt.Printf("%d ", i)
				time.Sleep(iif(first, 500*time.Millisecond, 1*time.Second))
			}
			fmt.Printf("\n")
			first = false

			// Send the challenge
			tcLimit, tcIncrement := l.tc.Limit, l.tc.Increment

			// TODO: put this in the config file
			// bots we like to play that have known time control preferences
			switch strings.ToLower(bot.User.ID) {
			case "torombot":
				// bullet only with increment (even 0+1), blitz, rapid, bots only rated
				tcLimit, tcIncrement = 0, 1
			}

			resp := l.challenge(bot.User.ID, true, tcLimit, tcIncrement, "random", "")
			if l.Quit() {
				return
			}

			if resp.Busy {
				i--
				continue
			}

			if resp.DailyLimit {
				return
			}

			if resp.CreateChallengeErr != nil {
				banned.Banned = append(banned.Banned, BannedBot{ID: bot.User.ID, Reason: resp.CreateChallengeErr.Error()})
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

func (l *Listener) challenge(userID string, rated bool, limit, increment int, color, fenPos string) TryChallengeResponse {
	l.activeGameMtx.Lock()
	l.challengeQueueMtx.Lock()
	isBusy := (l.activeGame != nil && !l.activeGame.finished) || l.challengePending
	hasChallenges := len(l.challengeQueue) != 0

	if isBusy || hasChallenges {
		l.activeGameMtx.Unlock()
		l.challengeQueueMtx.Unlock()
		time.Sleep(1000 * time.Millisecond)
		return TryChallengeResponse{Busy: true}
	}

	l.challengePending = true

	l.activeGameMtx.Unlock()
	l.challengeQueueMtx.Unlock()

	defer func() {
		l.challengeQueueMtx.Lock()
		l.challengePending = false
		l.challengeQueueMtx.Unlock()
	}()

	fmt.Printf("%s sending challenge to %s...\n", ts(), userID)
	//return TryChallengeResponse{DailyLimit: true}

	challengeID, err := api.CreateChallenge(userID, rated, limit, increment, color, "standard", fenPos)
	if err != nil {
		if strings.Contains(err.Error(), "429") {
			fmt.Printf("%s outgoing challenge limit exceeded for the day\n", ts())
			return TryChallengeResponse{DailyLimit: true}
		}

		log.Printf("%s ERR: CreateChallenge: %v\n", ts(), err)
		return TryChallengeResponse{CreateChallengeErr: err}
	}

	fmt.Printf("%s challenge sent to %s (id: %s). waiting 15s for response\n", ts(), userID, challengeID)

	timer := time.NewTimer(15 * time.Second)
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
		case <-l.ctx.Done():
			return TryChallengeResponse{}
		}
	}
}

func (l *Listener) processChallengeQueue() {
	var lastWaitingPrint time.Time
	for {
		if l.Quit() {
			return
		}

		l.activeGameMtx.Lock()
		l.challengeQueueMtx.Lock()
		isBusy := (l.activeGame != nil && !l.activeGame.finished) || l.challengePending
		hasChallenges := len(l.challengeQueue) != 0
		l.activeGameMtx.Unlock()
		l.challengeQueueMtx.Unlock()

		if isBusy || !hasChallenges {
			if !isBusy && time.Since(lastWaitingPrint) >= 30*time.Second {
				fmt.Printf("%s accepting challenges\n", ts())
				lastWaitingPrint = time.Now()
			}
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		fmt.Printf("%s checking challenge queue\n", ts())

		l.activeGameMtx.Lock()
		l.challengeQueueMtx.Lock()
		if l.challengePending || (l.activeGame != nil && !l.activeGame.finished) {
			l.activeGameMtx.Unlock()
			l.challengeQueueMtx.Unlock()
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
		l.activeGameMtx.Unlock()
		l.challengeQueueMtx.Unlock()

		time.Sleep(5 * time.Second)
	}
}

func (l *Listener) Quit() bool {
	select {
	case <-l.ctx.Done():
		return true
	default:
		return false
	}
}
