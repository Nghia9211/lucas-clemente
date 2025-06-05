package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type StateDQN struct {
	CWNDf float64 `json:"CWNDf"`
	INPf  float64 `json:"INPf"`
	SRTTf float64 `json:"SRTTf"`
	CWNDs float64 `json:"CWNDs"`
	INPs  float64 `json:"INPs"`
	SRTTs float64 `json:"SRTTs"`
}

type UpdateDataDQN struct {
	State     StateDQN `json:"state"`
	Action    int      `json:"action"`
	Reward    float64  `json:"reward"`
	NextState StateDQN `json:"next_state"`
	Done      bool     `json:"done"`
}

func main() {
	// Set model to SAC
	modelType := map[string]string{"model_type": "sac"}
	setModel(modelType)

	// Define a state to get action for
	state := StateDQN{
		CWNDf: 0,
		INPf:  0,
		SRTTf: 0,
		CWNDs: 0,
		INPs:  0,
		SRTTs: 0,
	}

	// Get action from SAC model
	action := getAction(state)
	fmt.Printf("Action: %v\n", action)

	// Decide path based on action
	path := selectPath(action)
	fmt.Printf("Selected path: %d\n", path)

	// Update reward
	rewardData := UpdateDataDQN{
		State:     state,
		Action:    path,
		Reward:    1.0, // Placeholder reward
		NextState: StateDQN{CWNDf: 1, INPf: 1, SRTTf: 1, CWNDs: 1, INPs: 1, SRTTs: 1},
		Done:      false,
	}
	updateReward(rewardData)
}

func setModel(data map[string]string) {
	url := "http://localhost:8081/set_model"
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error making POST request:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Model set to SAC successfully")
	} else {
		fmt.Println("Failed to set model:", resp.Status)
	}
}

func getAction(state StateDQN) []float64 {
	url := "http://localhost:8081/get_action"
	jsonData, err := json.Marshal(map[string]StateDQN{"state": state})
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return nil
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error making POST request:", err)
		return nil
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	action, ok := result["action"].([]interface{})
	if !ok {
		fmt.Println("Failed to parse action from response")
		return nil
	}

	var actionFloat []float64
	for _, a := range action {
		actionFloat = append(actionFloat, a.(float64))
	}
	return actionFloat
}

func updateReward(data UpdateDataDQN) {
	url := "http://localhost:8080/update_reward"
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error making POST request:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Reward updated successfully")
	} else {
		fmt.Println("Failed to update reward:", resp.Status)
	}
}

func selectPath(action []float64) int {
	if action[0] > action[1] {
		return 1 // Select path 1
	} else {
		return 2 // Select path 2
	}
}
