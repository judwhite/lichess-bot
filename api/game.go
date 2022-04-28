package api

import "time"

type GameEvent struct {
	Type string        `json:"type"`
	Game GameEventInfo `json:"game"`
}

type Compat struct {
	Bot   bool `json:"bot"`
	Board bool `json:"board"`
}

type GameEventInfo struct {
	ID          string   `json:"id"`
	FullID      string   `json:"fullId"`
	GameID      string   `json:"gameId"`
	FEN         string   `json:"fen"`
	Color       string   `json:"color"`
	LastMove    string   `json:"lastMove"`
	Source      string   `json:"source"`
	Variant     Variant  `json:"variant"`
	Speed       string   `json:"speed"`
	Perf        string   `json:"perf"`
	Rated       bool     `json:"rated"`
	HasMoved    bool     `json:"hasMoved"`
	Opponent    Opponent `json:"opponent"`
	Compat      Compat   `json:"compat"`
	IsMyTurn    bool     `json:"isMyTurn"`
	SecondsLeft int      `json:"secondsLeft"`
}

type Variant struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Short string `json:"short"`
}

type Opponent struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Rating   int    `json:"rating"`
}

type GameFull struct {
	Type       string  `json:"type"`
	ID         string  `json:"id"`
	Rated      bool    `json:"rated"`
	Variant    Variant `json:"variant"`
	Clock      Clock   `json:"clock"`
	Speed      string  `json:"speed"`
	Perf       Perf    `json:"perf"`
	CreatedAt  int64   `json:"createdAt"`
	White      Player  `json:"white"`
	Black      Player  `json:"black"`
	InitialFEN string  `json:"initialFen"`
	State      State   `json:"state"`
}

type Clock struct {
	Initial   int `json:"initial"`
	Increment int `json:"increment"`
	TotalTime int `json:"totalTime,omitempty"` // only set in completed games
}

type Perf struct {
	Name string `json:"name"`
}

type Player struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provisional bool   `json:"provisional"`
	Rating      int    `json:"rating"`
	Title       string `json:"title"`
}

type State struct {
	Type      string `json:"type"`
	Moves     string `json:"moves"`
	WhiteTime int    `json:"wtime"`
	BlackTime int    `json:"btime"`
	WhiteInc  int    `json:"winc"`
	BlackInc  int    `json:"binc"`
	Status    string `json:"status"`
	Winner    string `json:"winner"`
	WhiteDraw bool   `json:"wdraw"`
	BlackDraw bool   `json:"bdraw"`

	MessageReceived time.Time `json:"-"`
}

type CompletedGame struct {
	ID         string  `json:"id"`
	Rated      bool    `json:"rated"`
	Variant    string  `json:"variant"`
	Speed      string  `json:"speed"`
	Perf       string  `json:"perf"`
	CreatedAt  int64   `json:"createdAt"`
	LastMoveAt int64   `json:"lastMoveAt"`
	Status     string  `json:"status"`
	Players    Players `json:"players"`
	Winner     string  `json:"winner"`
	Opening    Opening `json:"opening"`
	Moves      string  `json:"moves"`
	PGN        string  `json:"pgn"`
	Clock      Clock   `json:"clock"`
}

type UserShort struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	ID    string `json:"id"`
}

type UserShortRating struct {
	User       UserShort `json:"user"`
	Rating     int       `json:"rating"`
	RatingDiff int       `json:"ratingDiff"`
}

type Players struct {
	White UserShortRating `json:"white"`
	Black UserShortRating `json:"black"`
}

type Opening struct {
	ECO  string `json:"eco"`
	Name string `json:"name"`
	Ply  int    `json:"ply"`
}
