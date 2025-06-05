package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
)

// State represents the state structure sent to the API
type State struct {
	CWNDf float64 `json:"CWNDf"`
	INPf  float64 `json:"INPf"`
	SRTTf float64 `json:"SRTTf"`
	CWNDs float64 `json:"CWNDs"`
	INPs  float64 `json:"INPs"`
	SRTTs float64 `json:"SRTTs"`
}

// ActionProbabilityResponse represents the response structure for /get_action endpoint
type ActionProbabilityResponse struct {
	Probability []float64 `json:"probability"`
	Error       string    `json:"error"`
}

// StatusResponse represents the response structure for /status endpoint
type StatusResponse struct {
	Status string `json:"status"`
}

// RewardPayload represents the payload structure for /update_reward endpoint
type RewardPayload struct {
	State     State   `json:"state"`
	NextState State   `json:"next_state"`
	Action    int     `json:"action"`
	Reward    float64 `json:"reward"`
	Done      bool    `json:"done"`
}

// ModelPayload represents the payload structure for /set_model endpoint
type ModelPayload struct {
	ModelType string `json:"model_type"`
}

func main() {
	baseURL := "http://127.0.0.1:8080"

	// Test /set_model endpoint
	fmt.Println("Testing /set_model endpoint...")
	modelPayload := ModelPayload{ModelType: "sac"}
	testSetModel(baseURL+"/set_model", modelPayload)

	// Simulate random scenario with 100 episodes
	fmt.Println("Simulating 100 episodes...")
	simulateEpisodes(baseURL, 1000)
}

func testSetModel(url string, payload ModelPayload) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("Error marshalling payload:", err)
		os.Exit(1)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		fmt.Println("Error sending request:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		fmt.Println("Error decoding response:", err)
		os.Exit(1)
	}

	fmt.Println("Response from /set_model:", response)
}

func getAction(url string, state State) (float64, error) {
	jsonPayload, err := json.Marshal(map[string]interface{}{
		"state": state,
	})
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var response ActionProbabilityResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return 0, err
	}

	if response.Error != "" {
		return 0, fmt.Errorf(response.Error)
	}

	return response.Probability[0], nil
}

func updateReward(url string, payload RewardPayload) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return err
	}

	if status, ok := response["status"].(string); !ok || status != "Reward updated" {
		return fmt.Errorf("unexpected response: %v", response)
	}

	return nil
}

func simulateEpisodes(baseURL string, numEpisodes int) {
	for episode := 0; episode < numEpisodes; episode++ {
		fmt.Printf("Episode %d\n", episode+1)
		state := randomState()

		done := false
		step := 0
		for !done {
			step++
			prob, err := getAction(baseURL+"/get_action", state)
			if err != nil {
				fmt.Println("Error getting action:", err)
				return
			}

			action := 1
			if prob < 0.5 {
				action = 0
			}

			nextState := randomState()
			reward := rand.Float64()    // Random reward for this example
			done = rand.Float64() < 0.1 // Randomly end the episode with a small probability

			rewardPayload := RewardPayload{
				State:     state,
				NextState: nextState,
				Action:    action,
				Reward:    reward,
				Done:      done,
			}

			err = updateReward(baseURL+"/update_reward", rewardPayload)
			if err != nil {
				fmt.Println("Error updating reward:", err)
				return
			}

			state = nextState

			if done {
				fmt.Printf("Episode %d finished after %d steps\n", episode+1, step)
			}
		}
	}
}

func randomState() State {
	return State{
		CWNDf: rand.Float64(),
		INPf:  rand.Float64(),
		SRTTf: rand.Float64(),
		CWNDs: rand.Float64(),
		INPs:  rand.Float64(),
		SRTTs: rand.Float64(),
	}
}
