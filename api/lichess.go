package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"trollfish-lichess/fen"
)

const allRatings = "1600,1800,2000,2200,2500"
const allSpeeds = "bullet,blitz,rapid,classical,correspondence"
const startPosFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

var ErrNotFound = errors.New("http 404 error")

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

func GetGames(username string, count int) (string, int, error) {
	filename := username + ".pgn"
	fp, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return filename, 0, err
	}

	w := bufio.NewWriter(fp)
	defer func() {
		w.Flush()
		fp.Close()
	}()

	var downloaded int
	handler := func(ndjson []byte) bool {
		var game CompletedGame
		if err := json.Unmarshal(ndjson, &game); err != nil {
			log.Fatal(err)
		}

		_, err := w.WriteString(game.PGN + "\n")
		if err != nil {
			log.Fatal(err)
		}

		downloaded++

		return true
	}

	u, err := url.Parse(fmt.Sprintf("https://lichess.org/api/games/user/%s", url.PathEscape(username)))
	if err != nil {
		return filename, 0, err
	}
	q := u.Query()
	//q.Add("since", unixMilli(time.Now().Add(-60*24*time.Hour)))
	//q.Add("analysed", "true") // TODO: may want to turn this off
	//q.Add("until", unixMilli(until))
	q.Add("sort", "dateAsc")
	q.Add("perfType", allSpeeds)
	//q.Add("evals", "true")
	q.Add("opening", "true")
	q.Add("rated", "true")
	if count > 0 {
		q.Add("max", itoa(count))
	}
	q.Add("pgnInJson", "true")
	//q.Add("clocks", "true")
	u.RawQuery = q.Encode()

	endpoint := u.String()
	if err := ReadStream(endpoint, handler); err != nil {
		return filename, 0, err
	}

	return filename, downloaded, nil
}

type LookupDatabase string

const (
	Masters LookupDatabase = "masters"
	Lichess LookupDatabase = "lichess"
)

func Lookup(db LookupDatabase, fen string, play ...string) (PositionResults, error) {
	var result PositionResults

	u, err := url.Parse(fmt.Sprintf("https://explorer.lichess.ovh/%s", db))
	if err != nil {
		return result, err
	}
	q := u.Query()
	if fen == "" || fen == "start" || fen == "startpos" {
		fen = startPosFEN
	}
	q.Add("fen", fen)
	if len(play) != 0 {
		q.Add("play", strings.Join(play, ","))
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

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return result, err
	}

	if resp.StatusCode != 200 {
		return result, fmt.Errorf("http status code %d. %s", resp.StatusCode, b)
	}

	if err := json.Unmarshal(b, &result); err != nil {
		return result, fmt.Errorf("%v. %s", err, b)
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

type CloudEvalResults struct {
	FEN    string `json:"fen"`
	KNodes int    `json:"knodes"`
	Depth  int    `json:"depth"`
	PVs    []PV   `json:"pvs"`
}

type PV struct {
	Moves string `json:"moves"`
	CP    int    `json:"cp"`
	Mate  int    `json:"mate"`
}

func CloudEval(fenPos string, multiPV int) (CloudEvalResults, error) {
	u, err := url.Parse("https://lichess.org/api/cloud-eval")
	if err != nil {
		return CloudEvalResults{}, err
	}
	q := u.Query()
	if fenPos == "" || fenPos == "start" || fenPos == "startpos" {
		fenPos = startPosFEN
	}
	q.Add("fen", fenPos)
	q.Add("multiPv", strconv.Itoa(multiPV))
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return CloudEvalResults{}, err
	}

	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return CloudEvalResults{}, err
	}

	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 {
			return CloudEvalResults{}, ErrNotFound
		}
		return CloudEvalResults{}, fmt.Errorf("http status code %d. %s", resp.StatusCode, b)
	}

	var result CloudEvalResults
	if err := json.Unmarshal(b, &result); err != nil {
		return CloudEvalResults{}, fmt.Errorf("%v. %s", err, b)
	}

	// unfortunately, this is the most convenient place to do the castling translation currently
	for i := 0; i < len(result.PVs); i++ {
		if len(result.PVs[i].Moves) == 0 {
			continue
		}

		board := fen.FENtoBoard(fenPos)
		moves := strings.Split(result.PVs[i].Moves, " ")
		sans := board.UCItoSANs(moves...)

		moves, err = board.SANtoUCIs(sans...)
		if err != nil {
			return CloudEvalResults{}, err
		}

		result.PVs[i].Moves = strings.Join(moves, " ")
	}

	return result, nil
}

func ReadStream(endpoint string, handler func([]byte) bool) error {
	fmt.Printf("%s %s\n", ts(), endpoint)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())
	req.Header.Add("Accept", "application/x-ndjson")
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
			continueRead := handler(ndjson)
			if !continueRead {
				break
			}
		}
	}

	if err := r.Err(); err != nil {
		return err
	}

	return nil
}

type BotQueue struct {
	Bots []*BotInfo
}

type BotInfo struct {
	User        User
	LastDecline time.Time
	LastTimeout time.Time
	LastAccept  time.Time
	Win         int
	Lose        int
	Draw        int
}

func StreamBots() (*BotQueue, error) {
	var q BotQueue

	handler := func(ndjson []byte) bool {
		var user User
		if err := json.Unmarshal(ndjson, &user); err != nil {
			log.Fatal(err)
		}

		q.Bots = append(q.Bots, &BotInfo{User: user})
		return true
	}

	if err := ReadStream("https://lichess.org/api/bot/online", handler); err != nil {
		return nil, err
	}

	return &q, nil
}

func DeclineChallenge(id, reason string) error {
	fmt.Printf("%s REQ: %s\n", ts(), "DeclineChallenge")
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

var lichessBotToken string

func AuthToken() string {
	if lichessBotToken != "" {
		return lichessBotToken
	}

	oauthToken, ok := os.LookupEnv("LICHESS_BOT_TOKEN")
	if !ok {
		log.Fatal("environment variable LICHESS_BOT_TOKEN not set")
	}

	lichessBotToken = fmt.Sprintf("Bearer %s", oauthToken)
	return lichessBotToken
}

func AcceptChallenge(id string) error {
	fmt.Printf("%s REQ: %s\n", ts(), "AcceptChallenge")

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
	fmt.Printf("%s REQ: %s\n", ts(), "AddTime")

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

func PlayMove(gameID, move string, draw bool) error {
	var sb strings.Builder
	sb.WriteString("https://lichess.org/api/bot/game/")
	sb.WriteString(gameID)
	sb.WriteString("/move/")
	sb.WriteString(move)

	if draw {
		sb.WriteString("?offeringDraw=true")
	}

	endpoint := sb.String()

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	b, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	return nil
}

func Chat(gameID, room, text string) error {
	endpoint := fmt.Sprintf("https://lichess.org/api/bot/game/%s/chat", gameID)

	data := url.Values{}
	data.Add("room", room)
	data.Add("text", text)

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

func CreateChallenge(id string, rated bool, clockLimit, clockIncrement int, color, variant, fenPos string) (string, error) {
	fmt.Printf("%s REQ: %s '%s'\n", ts(), "CreateChallenge", id)

	endpoint := fmt.Sprintf("https://lichess.org/api/challenge/%s", url.PathEscape(id))

	data := url.Values{}
	data.Add("rated", fmt.Sprintf("%v", rated))
	data.Add("clock.limit", fmt.Sprintf("%d", clockLimit))
	data.Add("clock.increment", fmt.Sprintf("%d", clockIncrement))
	data.Add("color", color)
	data.Add("variant", variant)
	if fenPos != "" {
		data.Add("fen", fenPos)
	}

	body := data.Encode()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("http.NewRequest: '%s' %v", endpoint, err)
	}

	req.Header.Add("Authorization", AuthToken())
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", fmt.Sprintf("%d", len(body)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http.DefaultClient.Do: '%s' %v", endpoint, err)
	}

	defer resp.Body.Close()

	b, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http status code %d '%s' body: '%s'", resp.StatusCode, endpoint, b)
	}

	var response struct {
		Challenge struct {
			ID string `json:"id"`
		} `json:"challenge"`
	}

	if err := json.Unmarshal(b, &response); err != nil {
		return "", fmt.Errorf("'%s' body: '%s'", endpoint, b)
	}

	return response.Challenge.ID, nil
}

func CancelChallenge(id string) error {
	fmt.Printf("%s REQ: %s\n", ts(), "CancelChallenge")

	endpoint := fmt.Sprintf("https://lichess.org/api/challenge/%s/cancel", id)

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

func ts() string {
	return fmt.Sprintf("[%s]", time.Now().Format("2006-01-02 15:04:05.000"))
}

func unixMilli(t time.Time) string {
	return itoa64(t.UnixMilli())
}

func itoa(a int) string {
	return strconv.Itoa(a)
}

func itoa64(a int64) string {
	return itoa(int(a))
}
