package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var token = os.Getenv("SLACK_AUTH_TOKEN")
var prodHook = os.Getenv("SLACK_PROD_WEBHOOK")
var devHook = os.Getenv("SLACK_DEV_WEBHOOK")

var mutex sync.Mutex

func cleanup() {
	if r := recover(); r != nil {
		fmt.Println("Recovered in cleanup", r)
	}
}

func postJSON(url string, data map[string]interface{}) (*http.Response, error) {
	jsonValue, _ := json.Marshal(data)
	fmt.Println(string(jsonValue))
	return http.Post(url, "application/json", bytes.NewBuffer(jsonValue))
}

func getUserInfo(userID string) (string, error) {
	resp, err := http.PostForm("https://slack.com/api/users.info", url.Values{"token": {token}, "user": {userID}})
	if err != nil {
		panic(err)
	}
	var data struct {
		Ok    bool
		Error string
		User  struct {
			Profile struct {
				Real_name               string
				Display_name            string
				Real_name_normalized    string
				Display_name_normalized string
			}
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		panic(err)
	}
	var e error
	if !data.Ok {
		e = errors.New(data.Error)
	}
	if data.User.Profile.Display_name != "" {
		return data.User.Profile.Display_name, e
	}
	return data.User.Profile.Real_name, e
}

func getChannelInfo(teamID string) (string, error) {
	resp, err := http.PostForm("https://slack.com/api/conversations.info", url.Values{"token": {token}, "channel": {teamID}})
	if err != nil {
		panic(err)
	}
	var data struct {
		Ok      bool
		Error   string
		Channel struct {
			Name string
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		panic(err)
	}
	var e error
	if !data.Ok {
		e = errors.New(data.Error)
	}
	return data.Channel.Name, e
}

func sendMessage(msg interface{}, channelName string, table bool) {
	var payload map[string]interface{}
	if table {
		payload = map[string]interface{}{"blocks": msg}
	} else {
		payload = map[string]interface{}{"text": msg.(string)}
	}
	if len(channelName) == 0 || strings.Contains(channelName, "dev") {
		res, err := postJSON(devHook, payload) // For #shots-dev
		fmt.Println(res)
		if err != nil {
			panic(err)
		}
	} else {
		res, err := postJSON(prodHook, payload) // For #shots
		fmt.Println(res)
		if err != nil {
			panic(err)
		}
	}
}

type reqBody struct {
	Challenge string
	Event     struct {
		Channel     string
		User        string
		Text        string
		Ts          string
		EventTs     string
		ChannelType string
	}
}

func msgHandler(w http.ResponseWriter, r *http.Request) {
	var data reqBody
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		panic(err)
	}
	if data.Challenge != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, err = w.Write([]byte(data.Challenge))
		if err != nil {
			panic(err)
		}
		return
	} else if r.Method == "POST" && data.Event.User != "" {
		go processEvent(data)
	}
}

func processEvent(data reqBody) {
	mutex.Lock()
	defer mutex.Unlock()
	fmt.Printf("%#v\n", data)
	text := strings.ToLower(data.Event.Text)
	channelName, err := getChannelInfo(data.Event.Channel)
	if err != nil {
		panic(err)
	}
	text = strings.TrimSpace(text)
	words := strings.Split(text, " ")
	if len(words) == 2 && words[0] == "limit" {
		limit, err := strconv.Atoi(words[1])
		if err != nil {
			panic(err)
		}

		fmt.Println("Setting limit for " + data.Event.User)
		name, err := getUserInfo(data.Event.User)
		if err == nil {
			setLimit(data.Event.User, limit)
			sendMessage(name+" has set a limit of "+strconv.Itoa(limit)+".", channelName, false)
		}
	}
	switch text {
	case "score":
		fmt.Println("Checking highscore for " + data.Event.User)
		shots := getShotHistory(data.Event.User, false, false, 0)
		highScore, _, _ := analyzeHistory(shots)
		name, err := getUserInfo(data.Event.User)
		if err == nil {
			sendMessage(name+"'s high score is "+strconv.Itoa(highScore)+"!", channelName, false)
		}
	case "shot":
		fmt.Println(data.Event.User + " takes a shot")
		ts, err := strconv.ParseFloat(data.Event.Ts, 64)
		shots := getShotHistory(data.Event.User, false, true, float64(ts))
                if shots == nil {
                    return
                }
		_, currentScore, limit := analyzeHistory(shots)
		name, err := getUserInfo(data.Event.User)
		if err != nil {
			panic(err)
		}
		sendMessage(name+" is at "+strconv.Itoa(currentScore)+" drinks right now.", channelName, false)
		var message string
		mention := fmt.Sprintf("<@%s>", data.Event.User)
		diff := limit - currentScore
		fmt.Println("Limit=", limit, "| diff=", diff)
		if limit < 0 || diff > 1 {
			fmt.Println("Not close to limit or no limit set.")
			return
		} else if diff == 1 {
			message = ":warning: Slow down there " + mention + "! You're getting real close to your limit of " + strconv.Itoa(limit) + " drinks! :warning:"
		} else if diff == 0 {
			message = ":octagonal_sign: Now's a good time to stop " + mention + "! You've hit your limit of " + strconv.Itoa(limit) + " drinks! :octagonal_sign:"
		} else {
			message = ":x::no_entry::no_good: BRO " + mention + " tf are you doing?? You've exceeded your limit of " + strconv.Itoa(limit) + " drinks! :x::no_entry::no_good:"
		}
		sendMessage(message, channelName, false)
	case "limit":
		fmt.Println("Checking limit for " + data.Event.User)
		shots := getShotHistory(data.Event.User, false, false, 0)
		_, _, limit := analyzeHistory(shots)
		name, err := getUserInfo(data.Event.User)
		if err != nil {
			panic(err)
		}
		message := name
		if limit < 0 {
			message += ", you currently do not have a limit set. Type `limit <number>` to set your limit, e.g. `limit 7`."
		} else {
			message += " currently has a limit of " + strconv.Itoa(limit) + " drinks."
		}
		sendMessage(message, channelName, false)
	case "leaderboard":
		fmt.Println("Printing leaderboard")
		entries, err := getAll()
		titleBlock := block{"section", []field{}, field{"mrkdwn", "Current standings for #" + channelName}}
		newlineBlock := block{"section", []field{}, field{"mrkdwn", " "}}
		headerBlock := block{"section", []field{field{"mrkdwn", "*Name* :eyes:"}, field{"mrkdwn", "*Score* :beers:"}}, nil}
		dividerBlock := block{"divider", []field{}, nil}
		blocks := []interface{}{titleBlock, newlineBlock, headerBlock, dividerBlock}
		if err != nil {
			panic(err)
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].highScore > entries[j].highScore
		})
		for _, entry := range entries {
			name, _ := getUserInfo(entry.userID)
			if len(name) == 0 {
				continue
			}
			fields := []field{field{"plain_text", name}, field{"plain_text", strconv.Itoa(entry.highScore)}}
			blocks = append(blocks, block{"section", fields, nil})
		}
		sendMessage(blocks, channelName, true)
	case "reset":
		fmt.Println("Resetting score for " + data.Event.User)
		shots := getShotHistory(data.Event.User, true, true, 0)
		_, currentScore, _ := analyzeHistory(shots)
		name, err := getUserInfo(data.Event.User)
		if err == nil {
			sendMessage(name+" is at "+strconv.Itoa(currentScore)+" drinks right now.", channelName, false)
		}

	}
}

type field struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func analyzeHistory(shotHistory []float64) (highscore int, currentScore int, limit int) {
	limit = -1
	if len(shotHistory) == 0 {
		return 0, 0, limit
	}
	prev := shotHistory[0]
	highscore = 0
	sections := [][]float64{{}}
	fmt.Println("History of shots: ", shotHistory)
	for _, current := range shotHistory {
		if current < 0 {
			limit = -int(current)
			prev = float64(time.Now().Unix())
			continue
		} else if current-prev <= 12*60*60 && current != 0 {
			sections[len(sections)-1] = append(sections[len(sections)-1], current)
			highscore = int(math.Max(float64(len(sections[len(sections)-1])), float64(highscore)))
		} else if current == 0 {
			limit = -1
			sections = append(sections, []float64{})
		} else {
			limit = -1
			sections = append(sections, []float64{current})
		}
		prev = current
	}
	fmt.Printf("Current shots: %v\n", sections[len(sections)-1])
	currentScore = len(sections[len(sections)-1])
	return
}

type block struct {
	Type   string      `json:"type"`
	Fields []field     `json:"fields,omitempty"`
	Text   interface{} `json:"text,omitempty"`
}

func main() {
	defer cleanup()
	port := os.Getenv("PORT")

	if port == "" {
		log.Fatal("$PORT must be set")
	}
	fmt.Println("Running on :" + port)
	http.HandleFunc("/msged", msgHandler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
