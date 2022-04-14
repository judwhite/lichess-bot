package api

type VariantPerf struct {
	Games       int  `json:"games"`
	Rating      int  `json:"rating"`
	Rd          int  `json:"rd"`
	Prog        int  `json:"prog"`
	Provisional bool `json:"prov"`
}

type Profile struct {
	Country    string `json:"country"`
	Location   string `json:"location"`
	Bio        string `json:"bio"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	FIDERating int    `json:"fideRating"`
	USCFRating int    `json:"uscfRating"`
	ECFRating  int    `json:"ecfRating"`
	Links      string `json:"links"`
}

type PlayTime struct {
	Total int `json:"total"`
	TV    int `json:"tv"`
}

type User struct {
	ID           string                 `json:"id"`
	Username     string                 `json:"username"`
	Online       bool                   `json:"online"`
	Perfs        map[string]VariantPerf `json:"perfs"`
	CreatedAt    int64                  `json:"createdAt"`
	Disabled     bool                   `json:"disabled"`
	TOSViolation bool                   `json:"tosViolation"`
	Profile      Profile                `json:"profile"`
	SeenAt       int64                  `json:"seenAt"`
	Patron       bool                   `json:"patron"`
	Verified     bool                   `json:"verified"`
	PlayTime     PlayTime               `json:"playTime"`
	Title        string                 `json:"title"`
}
