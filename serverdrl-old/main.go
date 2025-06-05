package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Structs để định dạng dữ liệu gửi đi và nhận về từ Flask API
type State struct {
	CWNDf float64 `json:"CWNDf"`
	INPf  float64 `json:"INPf"`
	SRTTf float64 `json:"SRTTf"`
	CWNDs float64 `json:"CWNDs"`
	INPs  float64 `json:"INPs"`
	SRTTs float64 `json:"SRTTs"`
}

type UpdateData struct {
	State     State   `json:"state"`
	Action    int     `json:"action"`
	Reward    float64 `json:"reward"`
	NextState State   `json:"next_state"`
	Done      bool    `json:"done"`
}

func main() {
	// GET request để lấy action từ Flask API
	getAction()

	// POST request để cập nhật agent của Flask API
	updateAgent()
}

func getAction() {
	stateData := UpdateData{
		State: State{
			CWNDf: 0.5,
			INPf:  0.3,
			SRTTf: 50,
			CWNDs: 0.5,
			INPs:  0.3,
			SRTTs: 50,
		},
		Done: false,
	}

	// Chuyển struct thành JSON
	jsonData, err := json.Marshal(stateData)
	if err != nil {
		fmt.Println("Error1:", err)
		return
	}

	// Tạo request POST tới endpoint '/update_agent'
	response, err := http.Post("http://localhost:8080/get_action", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error2:", err)
		return
	}
	defer response.Body.Close()

	// Đọc dữ liệu nhận được từ response
	var data map[string]interface{}
	err = json.NewDecoder(response.Body).Decode(&data)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// In ra action nhận được từ Flask API
	fmt.Println("Action:", data["action"])
}

func updateAgent() {
	// Dữ liệu để gửi trong POST request
	updateData := UpdateData{
		State: State{
			CWNDf: 0.5,
			INPf:  0.3,
			SRTTf: 50,
			CWNDs: 0.5,
			INPs:  0.3,
			SRTTs: 50,
		},
		Action: 0,
		Reward: 10,
		NextState: State{
			CWNDf: 0.5,
			INPf:  0.3,
			SRTTf: 50,
			CWNDs: 0.5,
			INPs:  0.3,
			SRTTs: 50,
		},
		Done: false,
	}

	// Chuyển struct thành JSON
	jsonData, err := json.Marshal(updateData)
	if err != nil {
		fmt.Println("Error1:", err)
		return
	}

	// Tạo request POST tới endpoint '/update_agent'
	response, err := http.Post("http://localhost:8080/update_agent", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error2:", err)
		return
	}
	defer response.Body.Close()

	// Đọc dữ liệu nhận được từ response
	var result map[string]interface{}
	err = json.NewDecoder(response.Body).Decode(&result)
	if err != nil {
		fmt.Println("Error3:", err)
		return
	}

	// In ra message nhận được từ Flask API
	fmt.Println("Message:", result["message"])
}
