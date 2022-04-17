package api

type ChatLine struct {
	Room     string `json:"room"`
	Username string `json:"username"`
	Text     string `json:"text"`
}
