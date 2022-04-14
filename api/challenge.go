package api

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
}
