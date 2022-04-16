package api

import "fmt"

type ChallengeUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Rating      int    `json:"rating"`
	Provisional bool   `json:"provisional"`
	Online      bool   `json:"online"`
	Patron      bool   `json:"patron"`
	Lag         int    `json:"lag"`
}

func (u ChallengeUser) Bot() bool {
	return u.Title == "BOT"
}

func (u ChallengeUser) Titled() bool {
	return u.Title != "BOT" && u.Title != ""
}

type ChallengeTimeControl struct {
	Type      string `json:"type"`
	Limit     int    `json:"limit"`
	Increment int    `json:"increment"`
	Show      string `json:"show"` // e.g. 5+25
}

type ChallengePerf struct {
	Icon string `json:"icon"`
	Name string `json:"name"`
}

type ChallengeEvent struct {
	Type      string    `json:"type"`
	Challenge Challenge `json:"challenge"`
}

type Challenge struct {
	ID            string               `json:"id"`
	URL           string               `json:"url"`
	Status        string               `json:"status"`
	Challenger    ChallengeUser        `json:"challenger"`
	DestUser      *ChallengeUser       `json:"destUser"`
	Variant       Variant              `json:"variant"`
	Rated         bool                 `json:"rated"`
	Speed         string               `json:"speed"`
	TimeControl   ChallengeTimeControl `json:"timeControl"`
	Color         string               `json:"color"` // "white", "black", "random"
	Perf          ChallengePerf        `json:"perf"`
	Direction     string               `json:"direction"` // "in", "out"
	InitialFEN    string               `json:"initialFen"`
	DeclineReason string               `json:"declineReason"`

	InternalCreated int64 `json:"-"`
}

type Challenges []Challenge

func (cs Challenges) Less(i, j int) bool {
	a := cs[i]
	b := cs[j]

	p1 := a.Challenger
	p2 := b.Challenger

	// humans come before bots
	if p1.Bot() != p2.Bot() {
		return !p1.Bot()
	}

	// take care of the humans
	if !p1.Bot() && !p2.Bot() {
		// titled players get priority
		if p1.Titled() != p2.Titled() {
			return p1.Titled()
		}

		return a.InternalCreated < b.InternalCreated
	}

	speed1 := speedToInt(a.Speed)
	speed2 := speedToInt(b.Speed)

	if speed1 != speed2 {
		return speed1 < speed2
	}

	inc1 := b.TimeControl.Increment
	inc2 := b.TimeControl.Increment

	if inc1 != inc2 {
		return inc1 < inc2
	}

	return a.InternalCreated < b.InternalCreated
}

func speedToInt(speed string) int {
	switch speed {
	case "ultraBullet":
		return 0
	case "bullet":
		return 5
	case "blitz":
		return 10
	case "rapid":
		return 15
	case "classical":
		return 20
	case "correspondence":
		return 25
	default:
		fmt.Printf("*** unhandled speed '%s'", speed)
		return 99
	}
}

func (cs Challenges) Swap(i, j int) {
	cs[i], cs[j] = cs[j], cs[i]
}

func (cs Challenges) Len() int {
	return len(cs)
}
