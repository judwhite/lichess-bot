package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"trollfish-lichess/api"
)

type Listener struct {
	playingMtx sync.Mutex
	playing    map[string]any
	input      chan<- string
	output     <-chan string
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
			u := c.Challenger
			tc := c.TimeControl

			/*
				lichess.Challenge{ID:"Yqi9Ldao", URL:"https://lichess.org/Yqi9Ldao", Status:"created", Challenger:lichess.ChallengeUser{ID:"bantercode", Name:"bantercode", Title:"", Rating:1112, Provisional:false, Online:true, Patron:false, Lag:0}, DestUser:(*lichess.ChallengeUser)(0xc0001fe690), Rated:false, Speed:"blitz", TimeControl:lichess.ChallengeTimeControl{Type:"clock", Limit:180, Increment:2, Show:"3+2"}, Color:"random", Perf:lichess.ChallengePerf{Icon:"\ue01d", Name:"Blitz"}, Direction:"", InitialFEN:"", DeclineReason:""}
				id: Yqi9Ldao url: https://lichess.org/Yqi9Ldao direction:  rated: false status: created color: random start_fen: ''
				- user_id: bantercode rating: 1112 online: true name: bantercode lag: 0 title:
				- type: clock limit: 180 inc: 2 show: 3+2
			*/

			fmt.Printf("%#v\n", c) // TODO: debug; remove

			if c.Status != "created" {
				// TODO: what other statuses do we process?
				return
			}

			// only use standard initial position
			if c.InitialFEN != "" {
				if err := api.DeclineChallenge(c.ID, "standard"); err != nil {
					log.Printf("ERR: %s\n", err)
				}
				return
			}

			// nahh bro
			if u.Provisional && u.Name != "bantercode" {
				if err := api.DeclineChallenge(c.ID, "later"); err != nil {
					log.Printf("ERR: %s\n", err)
				}
				return
			}

			if c.Variant.Key != "standard" {
				if err := api.DeclineChallenge(c.ID, "standard"); err != nil {
					log.Printf("ERR: %s\n", err)
				}
				return
			}

			if u.Name != "bantercode" {

				// bots can't play ultrabullet (but they can play 0+1)
				// I don't want to play (or test) games more than 5 mins or with increment > 5s
				if tc.Type != "clock" {
					if err := api.DeclineChallenge(c.ID, "timeControl"); err != nil {
						log.Printf("ERR: %s\n", err)
					}
					return
				}
				if tc.Limit < 30 {
					if err := api.DeclineChallenge(c.ID, "tooFast"); err != nil {
						log.Printf("ERR: %s\n", err)
					}
					return
				}
				if tc.Limit > 300 || tc.Increment > 5 {
					if err := api.DeclineChallenge(c.ID, "tooSlow"); err != nil {
						log.Printf("ERR: %s\n", err)
					}
					return
				}
			}

			fmt.Printf("id: %s url: %s direction: %s rated: %v status: %s color: %s start_fen: '%s' variant: %s\n",
				c.ID,
				c.URL,
				c.Direction,
				c.Rated,
				c.Status,
				c.Color,
				c.InitialFEN,
				c.Variant.Key,
			)

			fmt.Printf("- user_id: %s rating: %d online: %v name: %s lag: %d title: %s\n",
				u.ID,
				u.Rating,
				u.Online,
				u.Name,
				u.Lag,
				u.Title,
			)

			fmt.Printf("- type: %s limit: %d inc: %d show: %s\n", tc.Type, tc.Limit, tc.Increment, tc.Show)

			if u.Name != "bantercode" {
				if err := api.DeclineChallenge(c.ID, "later"); err != nil {
					log.Printf("ERR: %s\n", err)
				}
				return
			}

			if err := api.AcceptChallenge(c.ID); err != nil {
				log.Printf("ERR: AcceptChallenge: %v\n", err)
			}
		} else if event.Type == "gameStart" {
			fmt.Printf("gameStart: %s\n", ndjson)
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}
			game := gameEvent.Game
			fmt.Printf("%#v\n", game)
			go l.StreamGame(game.GameID)
		} else if event.Type == "gameFinish" {
			fmt.Printf("gameFinish: %s\n", ndjson)
			var gameEvent api.GameEvent
			if err := json.Unmarshal(ndjson, &gameEvent); err != nil {
				log.Fatalf("%v json: '%s' len=%d", err, ndjson, len(ndjson))
			}
		} else {
			fmt.Printf("event: %s\n", ndjson)
		}
	}

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

	handler := func(ndjson []byte) {
		var gameFull api.GameFull
		if err := json.Unmarshal(ndjson, &gameFull); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("gameFull ndjson:\n%s\n\ngameFull:\n%#v\n", ndjson, gameFull)
	}

	if err := api.ReadStream(endpoint, handler); err != nil {
		log.Printf("ERR: StreamGame: %v\n", err)
	}
}
