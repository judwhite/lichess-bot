package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const allRatings = "1600,1800,2000,2200,2500"
const allSpeeds = "bullet,blitz,rapid,classical"

type Move struct {
	UCI           string `json:"uci"`
	SAN           string `json:"san"`
	AverageRating int    `json:"averageRating"`

	White      int `json:"white"`
	Black      int `json:"black"`
	Draws      int `json:"draws"`
	TotalGames int `json:"total_games"`

	WhitePercent      float64 `json:"white_pct"`
	BlackPercent      float64 `json:"black_pct"`
	DrawsPercent      float64 `json:"draws_pct"`
	PopularityPercent float64 `json:"popularity_pct"`
}

type PositionResults struct {
	White      int    `json:"white"`
	Draws      int    `json:"draws"`
	Black      int    `json:"black"`
	Moves      []Move `json:"moves"`
	TotalGames int    `json:"total_games"`
}

func Lookup(fen, play string) (PositionResults, error) {
	var result PositionResults

	u, err := url.Parse("https://explorer.lichess.ovh/lichess")
	if err != nil {
		return result, err
	}
	q := u.Query()
	q.Add("fen", fen)
	if play != "" {
		q.Add("play", play)
	}
	q.Add("recentGames", "0")
	q.Add("topGames", "0")
	q.Add("speeds", allSpeeds)
	q.Add("ratings", allRatings)
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return result, err
	}

	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&result); err != nil {
		return result, err
	}

	if resp.StatusCode != 200 {
		return result, fmt.Errorf("http status code %d", resp.StatusCode)
	}

	total := result.White + result.Black + result.Draws
	result.TotalGames = total

	for i := 0; i < len(result.Moves); i++ {
		move := result.Moves[i]

		moveTotal := move.White + move.Black + move.Draws

		popularity := float64(moveTotal) / float64(total) * 100
		white := float64(move.White) / float64(moveTotal) * 100
		black := float64(move.Black) / float64(moveTotal) * 100
		draw := float64(move.Draws) / float64(moveTotal) * 100

		result.Moves[i].WhitePercent = white
		result.Moves[i].BlackPercent = black
		result.Moves[i].DrawsPercent = draw
		result.Moves[i].PopularityPercent = popularity
		result.Moves[i].TotalGames = moveTotal
	}

	return result, nil
}

func ReadStream(endpoint string, handler func([]byte)) error {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	r := bufio.NewScanner(resp.Body)
	for r.Scan() {
		ndjson := r.Bytes()

		if len(ndjson) != 0 {
			handler(ndjson)
		}
	}

	if err := r.Err(); err != nil {
		return err
	}

	return nil
}

func StreamBots() error {
	handler := func(ndjson []byte) {
		var user User
		if err := json.Unmarshal(ndjson, &user); err != nil {
			log.Fatal(err)
		}

		blitz, ok := user.Perfs["blitz"]
		if !ok {
			return
		}
		if blitz.Games == 0 || blitz.Provisional {
			return
		}
		created := time.UnixMilli(user.CreatedAt)
		seen := time.UnixMilli(user.SeenAt)
		fmt.Printf("%s blitz: games: %d rating: %d created: %v seen: %v ago\n", user.ID, blitz.Games, blitz.Rating,
			created.Format("Jan 2006"), time.Since(seen).Round(time.Second))
	}

	if err := ReadStream("https://lichess.org/api/bot/online", handler); err != nil {
		return err
	}

	return nil
}

func DeclineChallenge(id, reason string) error {
	fmt.Printf("decline: '%s'\n", reason)

	endpoint := fmt.Sprintf("https://lichess.org/api/challenge/%s/decline", id)

	data := url.Values{}
	data.Set("reason", reason)

	body := data.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", fmt.Sprintf("%d", len(body)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	b, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	return nil
}

func AuthToken() string {
	oauthToken, ok := os.LookupEnv("LICHESS_BOT_TOKEN")
	if !ok {
		log.Fatal("environment variable LICHESS_BOT_TOKEN not set")
	}

	return fmt.Sprintf("Bearer %s", oauthToken)
}

func AcceptChallenge(id string) error {
	fmt.Printf("* ACCEPT challenge %s\n", id)

	endpoint := fmt.Sprintf("https://lichess.org/api/challenge/%s/accept", id)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	b, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	return nil
}

func AddTime(gameID string, seconds int) error {
	endpoint := fmt.Sprintf("https://lichess.org/api/round/%s/add-time/%d", gameID, seconds)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	b, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	return nil
}

func PlayMove(gameID, move string) error {
	endpoint := fmt.Sprintf("https://lichess.org/api/bot/game/%s/move/%s", gameID, move)

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	b, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	return nil
}
