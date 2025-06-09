package quic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
	"net"

	"github.com/lucas-clemente/quic-go/internal/protocol"

	// "github.com/nguyenthanhtrungbkhn/go-fuzzy-logic"
	"github.com/lucas-clemente/quic-go/internal/multiclients"
)

// Địa chỉ socket server
const socketAddr = "127.0.0.1:8081"

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func NormalizeTimes(stat time.Duration) float64 {
	return float64(stat.Nanoseconds()) / float64(time.Millisecond.Nanoseconds())
}

func NormalizeGoodput(s *session, packetNumber uint64, retransNumber uint64) float64 {
	duration := time.Since(s.sessionCreationTime)

	elapsedtime := NormalizeTimes(duration) / 1000
	goodput := ((float64(packetNumber) - float64(retransNumber)) / 1024 / 1024 / elapsedtime) * float64(protocol.DefaultTCPMSS)

	return goodput
}

func NormalizeRewardGoodput(goodput float64) float64 {
	if goodput < Goodput_Min {
		Goodput_Min = goodput
	}
	if goodput > Goodput_Max {
		Goodput_Max = goodput
	}
	if Goodput_Max > Goodput_Min {
		return (goodput - Goodput_Min) / (Goodput_Max - Goodput_Min)
	} else {
		return 0.0
	}
}

func NormalizeRewardLrtt(lrtt float64) float64 {
	if lrtt < Lrtt_Min {
		Lrtt_Min = lrtt
	}
	if lrtt > Lrtt_Max {
		Lrtt_Max = lrtt
	}
	if Lrtt_Max > Lrtt_Min {
		return (lrtt - Lrtt_Min) / (Lrtt_Max - Lrtt_Min)
	} else {
		return 0.0
	}
}

func (sch *scheduler) GetStateAndRewardQlearning(s *session, pth *path) {
	//rcvdpacketNumber := pth.lastRcvdPacketNumber
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)

	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)

	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	cwndlevel := make(map[protocol.PathID]float32)
	inp := make(map[protocol.PathID]protocol.ByteCount)

	reWard := make(map[protocol.PathID]float64)

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths {
		//dataString := fmt.Sprintf("%d -", int(pathID))
		//f.WriteString(dataString)
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], _ = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			cwnd[pathID] = path.sentPacketHandler.GetCongestionWindow()
			inp[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(cwnd[pathID]) != 0 {
				cwndlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(cwnd[pathID])
			} else {
				cwndlevel[pathID] = 1
			}

			// Ordering paths
			if firstPath == protocol.PathID(255) {
				firstPath = pathID
			} else {
				if pathID < firstPath {
					secondPath = firstPath
					firstPath = pathID
				} else {
					secondPath = pathID
				}
			}
		}
	}
	if s.scheduler.preRTT[pth.pathID] != 0 {
		s.scheduler.iRTT[pth.pathID] = 0.5*s.scheduler.iRTT[pth.pathID] + 0.5*float64(NormalizeTimes(lRTT[pth.pathID]-s.scheduler.preRTT[pth.pathID]))
	}
	s.scheduler.preRTT[pth.pathID] = lRTT[pth.pathID]

	if float32(packetNumber[firstPath]) > 0 {
		reWard[firstPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[firstPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[firstPath])/50) - s.scheduler.Beta*float64(5*float32(retransNumber[firstPath])/float32(packetNumber[firstPath]))
	} else {
		reWard[firstPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[firstPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[firstPath])/50)
	}
	if float32(packetNumber[secondPath]) > 0 {
		reWard[secondPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[secondPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[secondPath])/50) - s.scheduler.Beta*float64(5*float32(retransNumber[secondPath])/float32(packetNumber[secondPath]))
	} else {
		reWard[secondPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[secondPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[secondPath])/50)
	}

	//sendingRate := (float64(cwnd[pth.pathID])/ float64(lRTT[pth.pathID])) / (float64(cwnd[firstPath])/ float64(lRTT[firstPath]) + float64(cwnd[secondPath])/ float64(lRTT[secondPath]))
	f_sendingRate := (float64(cwnd[firstPath]) / float64(lRTT[firstPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))
	s_sendingRate := (float64(cwnd[secondPath]) / float64(lRTT[secondPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))

	//update Q
	var f_cLevel, s_cLevel, col int8

	if pth.pathID == firstPath {
		col = 0
		if reWard[firstPath] == 0 {
			return
		}
	} else {
		if reWard[secondPath] == 0 {
			return
		}
		col = 1
	}

	if f_sendingRate < sch.clv_state[0] {
		f_cLevel = 0
	} else if f_sendingRate < sch.clv_state[1] {
		f_cLevel = 1
	} else if f_sendingRate < sch.clv_state[2] {
		f_cLevel = 2
	} else if f_sendingRate < sch.clv_state[3] {
		f_cLevel = 3
	} else {
		f_cLevel = 4
	}

	if s_sendingRate < sch.clv_state[0] {
		s_cLevel = 0
	} else if s_sendingRate < sch.clv_state[1] {
		s_cLevel = 1
	} else if s_sendingRate < sch.clv_state[2] {
		s_cLevel = 2
	} else if s_sendingRate < sch.clv_state[3] {
		s_cLevel = 3
	} else {
		s_cLevel = 4
	}

	old_f_cLevel := s.scheduler.currentState_f
	old_s_cLevel := s.scheduler.currentState_s

	var maxNextState float64
	if s.scheduler.qtable[f_cLevel][s_cLevel][0] > s.scheduler.qtable[f_cLevel][s_cLevel][1] {
		maxNextState = s.scheduler.qtable[f_cLevel][s_cLevel][0]
	} else {
		maxNextState = s.scheduler.qtable[f_cLevel][s_cLevel][1]
	}
	// BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))

	//Fuzzy logic
	// if s.scheduler.SchedulerName == "fuzzyqsat"{
	// 	var data fuzzy.FuzzyNumber
	// 	data.Family.Number = string(s.scheduler.record)
	// 	data.Family.Income = float64(float32(cwnd[pth.pathID]))
	// 	data.Family.Debt = float64(float32(BSend))

	// 	blt := fuzzy.BLT{}
	// 	blt.Fuzzification(&data)
	// 	blt.Inference(&data)
	// 	blt.Defuzzification(&data)
	// 	if (s.scheduler.AdaDivisor != 1.0){
	// 		s.scheduler.Delta = 1 - data.CrispValue
	// 	} else{
	// 		s.scheduler.Delta = data.CrispValue
	// 	}
	// 	//fmt.Println(s.scheduler.Delta)
	// }
	s.scheduler.record += 1

	// fmt.Println("Vl: ", s.scheduler.Delta)

	newValue := (1-s.scheduler.Delta)*s.scheduler.qtable[old_f_cLevel][old_s_cLevel][col] + (s.scheduler.Delta)*(reWard[pth.pathID]+s.scheduler.Gamma*maxNextState)

	s.scheduler.qtable[old_f_cLevel][old_s_cLevel][col] = newValue
	s.scheduler.currentState_f = f_cLevel
	s.scheduler.currentState_s = s_cLevel
	//log reward
	if s.perspective == protocol.PerspectiveServer && sch.model_id == 3 && sch.AdaDivisor == 1 {
		s.scheduler.csvwriter_reward.Write([]string{
			fmt.Sprintf("%.2f", reWard[pth.pathID]),
		})
		s.scheduler.csvwriter_reward.Flush()
	}
}

func (sch *scheduler) GetStateAndRewardMultiClients(s *session, pth *path) {
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)

	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)

	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	cwndlevel := make(map[protocol.PathID]float32)
	inp := make(map[protocol.PathID]protocol.ByteCount)

	reWard := make(map[protocol.PathID]float64)

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], _ = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			cwnd[pathID] = path.sentPacketHandler.GetCongestionWindow()
			inp[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(cwnd[pathID]) != 0 {
				cwndlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(cwnd[pathID])
			} else {
				cwndlevel[pathID] = 1
			}

			// Ordering paths
			if firstPath == protocol.PathID(255) {
				firstPath = pathID
			} else {
				if pathID < firstPath {
					secondPath = firstPath
					firstPath = pathID
				} else {
					secondPath = pathID
				}
			}
		}
	}

	reWard[firstPath] = (1-s.scheduler.AdaDivisor)*float64(cwndlevel[firstPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[firstPath]))
	reWard[secondPath] = (1-s.scheduler.AdaDivisor)*float64(cwndlevel[secondPath]) - s.scheduler.Alpha*float64(NormalizeTimes(lRTT[secondPath]))

	//Xac dinh trang thai cua mang
	f_sendingRate := (float64(cwnd[firstPath]) / float64(lRTT[firstPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))
	s_sendingRate := (float64(cwnd[secondPath]) / float64(lRTT[secondPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))

	fr_Rate := 0.0
	sr_Rate := 0.0

	if multiclients.S2.Count() > 1 {
		ItemsList := multiclients.S2.Items()
		for _, element := range ItemsList {
			if foo, ok := element.(multiclients.StateMulti); ok {
				fr_Rate += (float64(foo.FCWND) / float64(foo.FRTT)) / (float64(foo.FCWND)/float64(foo.FRTT) + float64(foo.SCWND)/float64(foo.SRTT))
				sr_Rate += (float64(foo.SCWND) / float64(foo.SRTT)) / (float64(foo.FCWND)/float64(foo.FRTT) + float64(foo.SCWND)/float64(foo.SRTT))
			}
		}
		fr_Rate = (fr_Rate - f_sendingRate) / float64(multiclients.S2.Count()-1)
		sr_Rate = (sr_Rate - s_sendingRate) / float64(multiclients.S2.Count()-1)
	}

	// tmp_para := 0.3
	// f_sendingRate = (1-tmp_para)*f_sendingRate + tmp_para*fr_Rate
	// s_sendingRate = (1-tmp_para)*s_sendingRate + tmp_para*sr_Rate
	var nf_cLevel, ns_cLevel, nfr_cLevel, nsr_cLevel int8

	if f_sendingRate < sch.clv_state[0] {
		nf_cLevel = 0
	} else if f_sendingRate < sch.clv_state[1] {
		nf_cLevel = 1
	} else if f_sendingRate < sch.clv_state[2] {
		nf_cLevel = 2
	} else if f_sendingRate < sch.clv_state[3] {
		nf_cLevel = 3
	} else {
		nf_cLevel = 4
	}

	if s_sendingRate < sch.clv_state[0] {
		ns_cLevel = 0
	} else if s_sendingRate < sch.clv_state[1] {
		ns_cLevel = 1
	} else if s_sendingRate < sch.clv_state[2] {
		ns_cLevel = 2
	} else if s_sendingRate < sch.clv_state[3] {
		ns_cLevel = 3
	} else {
		ns_cLevel = 4
	}

	if fr_Rate < sch.clv_state2[0] {
		nfr_cLevel = 0
	} else if fr_Rate < sch.clv_state2[1] {
		nfr_cLevel = 1
	} else if fr_Rate < sch.clv_state2[2] {
		nfr_cLevel = 2
	} else if fr_Rate < sch.clv_state2[3] {
		nfr_cLevel = 3
	} else {
		nfr_cLevel = 4
	}

	if sr_Rate >= sch.clv_state2[0] {
		nsr_cLevel = 0
	} else if sr_Rate >= sch.clv_state2[1] {
		nsr_cLevel = 1
	} else if sr_Rate >= sch.clv_state2[2] {
		nsr_cLevel = 2
	} else if sr_Rate >= sch.clv_state2[3] {
		nsr_cLevel = 3
	} else {
		nsr_cLevel = 4
	}

	//update Q follow by state of action t
	//Trang thai cu
	var f_cLevel, s_cLevel, fr_cLevel, sr_cLevel, col int8
	f_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_f
	s_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_s
	fr_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_fr
	sr_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_sr
	// f_cLevel = sch.currentState_f
	// s_cLevel = sch.currentState_s
	// fr_cLevel = sch.currentState_fr
	// sr_cLevel = sch.currentState_sr
	if pth.pathID == firstPath {
		col = 0
		if reWard[firstPath] == 0 {
			return
		}
	} else {
		if reWard[secondPath] == 0 {
			return
		}
		col = 1
	}

	// BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))

	//Fuzzy logic
	// if s.scheduler.SchedulerName == "multiclients"{
	// 	var data fuzzy.FuzzyNumber
	// 	data.Family.Number = string(s.scheduler.record)
	// 	data.Family.Income = float64(float32(cwnd[pth.pathID]))
	// 	data.Family.Debt = float64(float32(BSend))

	// 	blt := fuzzy.BLT{}
	// 	blt.Fuzzification(&data)
	// 	blt.Inference(&data)
	// 	blt.Defuzzification(&data)
	// 	if (s.scheduler.AdaDivisor != 1.0){
	// 		s.scheduler.Delta = 1 - data.CrispValue
	// 	} else{
	// 		s.scheduler.Delta = data.CrispValue
	// 	}
	// 	//fmt.Println(s.scheduler.Delta)
	// }
	s.scheduler.record += 1

	var maxNextState float64
	if multiclients.MultiQtable[nf_cLevel][ns_cLevel][nfr_cLevel][nsr_cLevel][0] > multiclients.MultiQtable[nf_cLevel][ns_cLevel][nfr_cLevel][nsr_cLevel][1] {
		maxNextState = multiclients.MultiQtable[nf_cLevel][ns_cLevel][nfr_cLevel][nsr_cLevel][0]
	} else {
		maxNextState = multiclients.MultiQtable[nf_cLevel][ns_cLevel][nfr_cLevel][nsr_cLevel][1]
	}

	newValue := (1-s.scheduler.Delta)*multiclients.MultiQtable[s.scheduler.currentState_f][s.scheduler.currentState_s][s.scheduler.currentState_fr][s.scheduler.currentState_sr][col] + (s.scheduler.Delta)*(reWard[pth.pathID]+s.scheduler.Gamma*maxNextState)

	multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][col] = newValue

	//fmt.Println("RewardAck: ", reWard[pth.pathID], maxNextState, newValue )
	// s.scheduler.currentState_f = f_cLevel
	// s.scheduler.currentState_s = s_cLevel
	// s.scheduler.currentState_fr = fr_cLevel
	// s.scheduler.currentState_sr = sr_cLevel
}

func (sch *scheduler) GetStateAndRewardMultiClientsRetrans(s *session, pth *path) {

	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			cwnd[pathID] = path.sentPacketHandler.GetCongestionWindow()
			// Ordering paths
			if firstPath == protocol.PathID(255) {
				firstPath = pathID
			} else {
				if pathID < firstPath {
					secondPath = firstPath
					firstPath = pathID
				} else {
					secondPath = pathID
				}
			}
		}
	}

	// BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))

	//Fuzzy logic
	// if s.scheduler.SchedulerName == "multiclients"{
	// 	var data fuzzy.FuzzyNumber
	// 	data.Family.Number = string(s.scheduler.record)
	// 	data.Family.Income = float64(float32(cwnd[pth.pathID]))
	// 	data.Family.Debt = float64(float32(BSend))

	// 	blt := fuzzy.BLT{}
	// 	blt.Fuzzification(&data)
	// 	blt.Inference(&data)
	// 	blt.Defuzzification(&data)
	// 	if (s.scheduler.AdaDivisor != 1.0){
	// 		s.scheduler.Delta = 1 - data.CrispValue
	// 	} else{
	// 		s.scheduler.Delta = data.CrispValue
	// 	}
	// 	//fmt.Println(s.scheduler.Delta)
	// }
	s.scheduler.record += 1

	reWard := make(map[protocol.PathID]float64)
	reWard[firstPath] = -s.scheduler.Beta
	reWard[secondPath] = -s.scheduler.Beta

	var f_cLevel, s_cLevel, fr_cLevel, sr_cLevel, col int8

	//update Q follow by state of action t

	f_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_f
	s_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_s
	fr_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_fr
	sr_cLevel = sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}].cState_sr

	if pth.pathID == firstPath {
		col = 0
	} else {
		col = 1
	}

	var maxNextState float64
	if multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][0] > multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][1] {
		maxNextState = multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][0]
	} else {
		maxNextState = multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][1]
	}

	newValue := (1-s.scheduler.Delta)*multiclients.MultiQtable[s.scheduler.currentState_f][s.scheduler.currentState_s][s.scheduler.currentState_fr][s.scheduler.currentState_sr][col] + (s.scheduler.Delta)*(reWard[pth.pathID]+s.scheduler.Gamma*maxNextState)

	multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][col] = newValue
	//fmt.Println("RewardRestran: ", reWard[pth.pathID], maxNextState, newValue )

}

// func updateReward(url string, payload RewardPayload) error {
// 	jsonPayload, err := json.Marshal(payload)
// 	if err != nil {
// 		return err
// 	}

// 	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	var response map[string]interface{}
// 	err = json.NewDecoder(resp.Body).Decode(&response)
// 	if err != nil {
// 		return err
// 	}

// 	if status, ok := response["status"].(string); !ok || status != "Reward updated" {
// 		return fmt.Errorf("unexpected response: %v", response)
// 	}

// 	return nil
// }

func updateReward(url string, payload RewardPayload) {
	// Use a goroutine to perform the POST request without waiting for the response
	go func() {
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}
		// fmt.Println("JSON: ", jsonPayload)
		_, err = http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
		}
	}()
}

// func (sch *scheduler) GetStateAndRewardDQN(s *session, pth *path) {
// 	packetNumber := make(map[protocol.PathID]uint64)
// 	retransNumber := make(map[protocol.PathID]uint64)
// 	lostNumber := make(map[protocol.PathID]uint64)

// 	lRTT := make(map[protocol.PathID]time.Duration)
// 	sRTT := make(map[protocol.PathID]time.Duration)
// 	mRTT := make(map[protocol.PathID]time.Duration)

// 	CWND := make(map[protocol.PathID]protocol.ByteCount)
// 	CWNDlevel := make(map[protocol.PathID]float32)
// 	INP := make(map[protocol.PathID]protocol.ByteCount)

// 	reWard := make(map[protocol.PathID]float64)

// 	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

// 	for pathID, path := range s.paths {
// 		if pathID != protocol.InitialPathID {
// 			packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = path.sentPacketHandler.GetStatistics()
// 			lRTT[pathID] = path.rttStats.LatestRTT()
// 			sRTT[pathID] = path.rttStats.SmoothedRTT()
// 			mRTT[pathID] = path.rttStats.MinRTT()
// 			CWND[pathID] = path.sentPacketHandler.GetCongestionWindow()
// 			INP[pathID] = path.sentPacketHandler.GetBytesInFlight()
// 			if float32(CWND[pathID]) != 0 {
// 				CWNDlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(CWND[pathID])
// 			} else {
// 				CWNDlevel[pathID] = 1
// 			}
// 			// Ordering paths
// 			if firstPath == protocol.PathID(255) {
// 				firstPath = pathID
// 			} else {
// 				if pathID < firstPath {
// 					secondPath = firstPath
// 					firstPath = pathID
// 				} else {
// 					secondPath = pathID
// 				}
// 			}
// 		}
// 	}

// 	//alpha := 0.01
// 	//reWard[firstPath] = goodput1 - alpha*float64(NormalizeTimes(lRTT[firstPath])) - float64(lostNumber[firstPath]/packetNumber[firstPath])
// 	//reWard[secondPath] = goodput1 - alpha*float64(NormalizeTimes(lRTT[secondPath]))- float64(lostNumber[secondPath]/packetNumber[secondPath])

// 	rttrate := 0.0
// 	// goodput := NormalizeGoodput(s, packetNumber[pth.pathID], retransNumber[pth.pathID])
// 	lostrate := 10 * float64(lostNumber[pth.pathID]) / float64(packetNumber[pth.pathID])
// 	rttrate = float64(lRTT[pth.pathID]) / 200

// 	reWard[pth.pathID] = -rttrate - lostrate
// 	// fmt.Println("reWard", float64(goodput), rttrate, lostrate)
// 	// old_state := sch.list_State_DQN[State{pth.pathID, pth.lastRcvdPacketNumber}]
// 	// old_action := sch.list_Action_DQN[State{pth.pathID, pth.lastRcvdPacketNumber}]

// 	// nextState := StateDQN{
// 	// 	CWNDf: float64(CWND[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
// 	// 	INPf:  float64(INP[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
// 	// 	SRTTf: NormalizeTimes(sRTT[firstPath]) / 50.0,
// 	// 	CWNDs: float64(CWND[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
// 	// 	INPs:  float64(INP[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
// 	// 	SRTTs: NormalizeTimes(sRTT[secondPath]) / 50.0,
// 	// }
// }

func (sch *scheduler) GetStateAndRewardQSAT(s *session, pth *path) {
	rcvdpacketNumber := pth.lastRcvdPacketNumber
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)

	sRTT := make(map[protocol.PathID]time.Duration)
	maxRTT := make(map[protocol.PathID]time.Duration)

	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	cwndlevel := make(map[protocol.PathID]float32)

	reWard := make(map[protocol.PathID]float64)

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths {
		//dataString := fmt.Sprintf("%d -", int(pathID))
		//f.WriteString(dataString)
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], _ = path.sentPacketHandler.GetStatistics()
			sRTT[pathID] = path.rttStats.LatestRTT()
			maxRTT[pathID] = path.rttStats.MaxRTT()
			cwnd[pathID] = path.sentPacketHandler.GetCongestionWindow()
			if float32(cwnd[pathID]) != 0 {
				cwndlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(cwnd[pathID])
			} else {
				cwndlevel[pathID] = 1
			}

			// Ordering paths
			if firstPath == protocol.PathID(255) {
				firstPath = pathID
			} else {
				if pathID < firstPath {
					secondPath = firstPath
					firstPath = pathID
				} else {
					secondPath = pathID
				}
			}
		}
	}

	if maxRTT[firstPath] <= maxRTT[secondPath] {
		maxRTT[firstPath] = maxRTT[secondPath]
	} else {
		maxRTT[secondPath] = maxRTT[firstPath]
	}

	if float32(packetNumber[firstPath]) > 0 {
		reWard[firstPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[firstPath]) - s.scheduler.Alpha*float64(NormalizeTimes(sRTT[firstPath])/50) - s.scheduler.Beta*float64(5*float32(retransNumber[firstPath])/float32(packetNumber[firstPath]))
	} else {
		reWard[firstPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[firstPath]) - s.scheduler.Alpha*float64(NormalizeTimes(sRTT[firstPath])/50)
	}
	if float32(packetNumber[secondPath]) > 0 {
		reWard[secondPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[secondPath]) - s.scheduler.Alpha*float64(NormalizeTimes(sRTT[secondPath])/50) - s.scheduler.Beta*float64(5*float32(retransNumber[secondPath])/float32(packetNumber[secondPath]))
	} else {
		reWard[secondPath] = (1-s.scheduler.Alpha-s.scheduler.Beta)*float64(cwndlevel[secondPath]) - s.scheduler.Alpha*float64(NormalizeTimes(sRTT[secondPath])/50)
	}

	//State
	oldBSend := s.scheduler.QoldState[State{id: pth.pathID, pktnumber: rcvdpacketNumber}]
	delete(s.scheduler.QoldState, State{id: pth.pathID, pktnumber: rcvdpacketNumber})

	var BSend protocol.ByteCount
	var BSend1 float32

	BSend, _ = s.flowControlManager.SendWindowSize(protocol.StreamID(5))
	BSend1 = float32(BSend) / (float32(protocol.DefaultMaxCongestionWindow) * 300)
	var ro, col, ro1 int8

	if pth.pathID == firstPath {
		col = 0
		if reWard[firstPath] == 0 {
			return
		}
	} else {
		if reWard[secondPath] == 0 {
			return
		}
		col = 1
	}

	if float64(BSend1) < sch.Qstate[0] {
		ro = 0
	} else if float64(BSend1) < sch.Qstate[1] {
		ro = 1
	} else if float64(BSend1) < sch.Qstate[2] {
		ro = 2
	} else if float64(BSend1) < sch.Qstate[3] {
		ro = 3
	} else if float64(BSend1) < sch.Qstate[4] {
		ro = 4
	} else if float64(BSend1) < sch.Qstate[5] {
		ro = 5
	} else if float64(BSend1) < sch.Qstate[6] {
		ro = 6
	} else {
		ro = 7
	}

	if float64(oldBSend) < sch.Qstate[0] {
		ro1 = 0
	} else if float64(oldBSend) < sch.Qstate[1] {
		ro1 = 1
	} else if float64(oldBSend) < sch.Qstate[2] {
		ro1 = 2
	} else if float64(oldBSend) < sch.Qstate[3] {
		ro1 = 3
	} else if float64(oldBSend) < sch.Qstate[4] {
		ro1 = 4
	} else if float64(oldBSend) < sch.Qstate[5] {
		ro1 = 5
	} else if float64(oldBSend) < sch.Qstate[6] {
		ro1 = 6
	} else {
		ro1 = 7
	}

	var maxNextState float64
	if s.scheduler.Qqtable[Store{Row: ro, Col: 1}] > s.scheduler.Qqtable[Store{Row: ro, Col: 0}] {
		maxNextState = s.scheduler.Qqtable[Store{Row: ro, Col: 1}]
	} else {
		maxNextState = s.scheduler.Qqtable[Store{Row: ro, Col: 0}]
	}

	newValue := (1-s.scheduler.Delta)*s.scheduler.Qqtable[Store{Row: ro1, Col: col}] + s.scheduler.Delta*(reWard[pth.pathID]+s.scheduler.Gamma*maxNextState)

	s.scheduler.Qqtable[Store{Row: ro1, Col: col}] = newValue
}

func (sch *scheduler) GetStateAndRewardSAC(s *session, pth *path) {
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)

	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)
	mRTT := make(map[protocol.PathID]time.Duration)

	CWND := make(map[protocol.PathID]protocol.ByteCount)
	CWNDlevel := make(map[protocol.PathID]float32)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	reWard := make(map[protocol.PathID]float64)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			mRTT[pathID] = path.rttStats.MinRTT()
			CWND[pathID] = path.sentPacketHandler.GetCongestionWindow()
			INP[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(CWND[pathID]) != 0 {
				CWNDlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(CWND[pathID])
			} else {
				CWNDlevel[pathID] = 1
			}
		}
	}

	rttrate := 0.0
	goodput := NormalizeGoodput(s, packetNumber[pth.pathID], retransNumber[pth.pathID])
	// lostrate := float64(retransNumber[pth.pathID]) / float64(packetNumber[pth.pathID])
	rttrate = NormalizeTimes(lRTT[pth.pathID])
	// restranrate := float64(retransNumber[pth.pathID]) / float64(packetNumber[pth.pathID])

	reWard[pth.pathID] = sch.Beta*NormalizeRewardGoodput(goodput) - (1-sch.Beta)*NormalizeRewardLrtt(rttrate)
	// fmt.Println("Reward: ", reWard[pth.pathID], NormalizeRewardGoodput(goodput), NormalizeRewardLrtt(rttrate), sch.Alpha, sch.Beta)

	old_action := sch.list_Action_DQN[State{pth.pathID, pth.lastRcvdPacketNumber}]

	var connID string = strconv.FormatFloat(old_action, 'E', -1, 64)
	if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
		// fmt.Println("State: ", stateData, rewardPayload)
		rewardPayload, ok := tmp_Reload.(RewardPayload)
		if !ok {
			fmt.Println("Type assertion failed")
			return
		}
		if pth.pathID == 1 {
			rewardPayload.Reward += reWard[pth.pathID] * old_action
		} else {
			rewardPayload.Reward += reWard[pth.pathID] * (1 - old_action)
		}
		rewardPayload.CountReward += 1
		k := uint16(0)
		if uint16(sch.Alpha) < 6 {
			k = uint16(sch.Alpha)
		} else {
			k = uint16(sch.Alpha) - 5
		}
		if rewardPayload.CountReward >= k {
			rewardPayload.Reward = rewardPayload.Reward / float64(rewardPayload.CountReward)
			rewardPayload.Action = old_action

			tmp_Payload := RewardPayload{
				State:     rewardPayload.State,
				NextState: rewardPayload.NextState,
				Action:    rewardPayload.Action,
				Reward:    rewardPayload.Reward,
				Done:      false,
				ModelID:   sch.model_id,
			}

			// fmt.Println("Reward: ", tmp_Payload)
			updateReward(baseURL+"/update_reward", tmp_Payload)

			// multiclients.List_Reward_DQN.Remove(connID)
			rewardPayload.Reward = 0
			rewardPayload.CountReward = 0
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		} else {
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		}
	}
}
func (sch *scheduler) GetStateAndRewardSACMulti(s *session, pth *path) {
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)

	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)
	mRTT := make(map[protocol.PathID]time.Duration)

	CWND := make(map[protocol.PathID]protocol.ByteCount)
	CWNDlevel := make(map[protocol.PathID]float32)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	reWard := make(map[protocol.PathID]float64)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			mRTT[pathID] = path.rttStats.MinRTT()
			CWND[pathID] = path.sentPacketHandler.GetCongestionWindow()
			INP[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(CWND[pathID]) != 0 {
				CWNDlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(CWND[pathID])
			} else {
				CWNDlevel[pathID] = 1
			}
		}
	}

	rttrate := 0.0
	goodput := NormalizeGoodput(s, packetNumber[pth.pathID], retransNumber[pth.pathID])
	// lostrate := float64(retransNumber[pth.pathID]) / float64(packetNumber[pth.pathID])
	rttrate = NormalizeTimes(lRTT[pth.pathID]) / 10

	reWard[pth.pathID] = goodput - rttrate
	old_action := sch.list_Action_SACMulti[State{pth.pathID, pth.lastRcvdPacketNumber}]
	// fmt.Println("Reward: ", reWard[pth.pathID], 10*goodput, rttrate)

	var connID string = strconv.FormatFloat(old_action, 'E', -1, 64)
	if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
		// fmt.Println("State: ", stateData, rewardPayload)
		rewardPayload, ok := tmp_Reload.(RewardPayloadSACMulti)
		if !ok {
			fmt.Println("Type assertion failed")
			return
		}
		if pth.pathID == 1 {
			rewardPayload.Reward += reWard[pth.pathID] * old_action
		} else {
			rewardPayload.Reward += reWard[pth.pathID] * (1 - old_action)
		}
		rewardPayload.CountReward += 1

		if rewardPayload.CountReward > 9 {
			rewardPayload.Reward = rewardPayload.Reward / float64(rewardPayload.CountReward)
			rewardPayload.Action = old_action

			tmp_Payload := RewardPayloadSACMulti{
				State:     rewardPayload.State,
				NextState: rewardPayload.NextState,
				Action:    rewardPayload.Action,
				Reward:    rewardPayload.Reward,
				Done:      false,
				ModelID:   sch.model_id,
			}

			// fmt.Println("Reward: ", tmp_Payload)
			updateRewardSACMulti(baseURL+"/update_reward", tmp_Payload)

			// multiclients.List_Reward_DQN.Remove(connID)
			rewardPayload.Reward = 0
			rewardPayload.CountReward = 0
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		} else {
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		}
	}
}

func updateRewardSACMulti(url string, payload RewardPayloadSACMulti) {
	go func() {

		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		_, err2 := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err2 != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
	}()
}

func (sch *scheduler) GetStateAndRewardSACMultiJoinCC(s *session, pth *path) {
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)

	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)
	mRTT := make(map[protocol.PathID]time.Duration)

	CWND := make(map[protocol.PathID]protocol.ByteCount)
	CWNDlevel := make(map[protocol.PathID]float32)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	reWard := make(map[protocol.PathID]float64)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			mRTT[pathID] = path.rttStats.MinRTT()
			CWND[pathID] = path.sentPacketHandler.GetCongestionWindow()
			INP[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(CWND[pathID]) != 0 {
				CWNDlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(CWND[pathID])
			} else {
				CWNDlevel[pathID] = 1
			}
		}
	}

	rttrate := 0.0
	goodput := NormalizeGoodput(s, packetNumber[pth.pathID], retransNumber[pth.pathID]) / 100
	// lostrate := float64(retransNumber[pth.pathID]) / float64(packetNumber[pth.pathID])
	rttrate = float64(lRTT[pth.pathID]) / 100.0 / 1000000.0

	reWard[pth.pathID] = goodput - rttrate
	old_action := sch.list_Action_SACMultiJoinCC[State{pth.pathID, pth.lastRcvdPacketNumber}]

	var connID string = strconv.FormatFloat(old_action.Action1, 'f', 5, 64) + strconv.FormatFloat(old_action.Action2, 'f', 5, 64) + strconv.FormatFloat(old_action.Action3, 'f', 5, 64)
	if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
		// fmt.Println("State: ", stateData, rewardPayload)
		rewardPayload, ok := tmp_Reload.(RewardPayloadSACMultiJoinCC)
		if !ok {
			fmt.Println("Type assertion failed")
			return
		}
		if pth.pathID == 1 {
			rewardPayload.Reward += reWard[pth.pathID] * old_action.Action1
		} else {
			rewardPayload.Reward += reWard[pth.pathID] * (1 - old_action.Action1)
		}
		rewardPayload.CountReward += 1

		if rewardPayload.CountReward > 8 {
			rewardPayload.Reward = rewardPayload.Reward / float64(rewardPayload.CountReward)
			rewardPayload.Action = old_action

			tmp_Payload := RewardPayloadSACMultiJoinCC{
				State:     rewardPayload.State,
				NextState: rewardPayload.NextState,
				Action:    rewardPayload.Action,
				Reward:    rewardPayload.Reward,
				Done:      false,
				ModelID:   sch.model_id,
			}

			// fmt.Println("Reward: ", tmp_Payload)
			updateRewardSACMultiJoinCC(baseURL+"/update_reward", tmp_Payload)

			// multiclients.List_Reward_DQN.Remove(connID)
			rewardPayload.Reward = 0
			rewardPayload.CountReward = 0
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		} else {
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		}
	}
}

func updateRewardSACMultiJoinCC(url string, payload RewardPayloadSACMultiJoinCC) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Use a goroutine to perform the POST request without waiting for the response
	go func() {
		_, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
		}
	}()

	return nil
}

func (sch *scheduler) GetStateAndRewardSACcc(s *session, pth *path, isLoss bool) {
	// fmt.Println("TEST 1 ")
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)
	CWNDlevel := make(map[protocol.PathID]float32)
	lRTT := make(map[protocol.PathID]time.Duration)
	sRTT := make(map[protocol.PathID]time.Duration)
	mRTT := make(map[protocol.PathID]time.Duration)

	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	firstPath := protocol.PathID(255)

	reWard := make(map[protocol.PathID]float64)

	for pathID, path := range s.paths {
		if pathID != protocol.InitialPathID {
			firstPath = pathID
			packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = path.sentPacketHandler.GetStatistics()
			lRTT[pathID] = path.rttStats.LatestRTT()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			mRTT[pathID] = path.rttStats.MinRTT()
			CWND[pathID] = path.sentPacketHandler.GetCongestionWindow()
			INP[pathID] = path.sentPacketHandler.GetBytesInFlight()
			if float32(CWND[pathID]) != 0 {
				CWNDlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(CWND[pathID])
			} else {
				CWNDlevel[pathID] = 1
			}
		}
	}

	goodput := NormalizeGoodput(s, packetNumber[firstPath], retransNumber[firstPath])
	// fmt.Println("Goodput: ", goodput)
	// lostrate := float64(retransNumber[firstPath]) / float64(packetNumber[firstPath])
	
	//rttrate := float64(lRTT) / 100.0 / 1000000.0
	var rttvar float64
	if sRTT[firstPath] != 0 {
		rttvar = math.Abs(float64(lRTT[firstPath]-sRTT[firstPath])) / float64(sRTT[firstPath])
	}

	// reWard[firstPath] = -2*lostrate
	// reWard[firstPath] = goodput - 15*lostrate - rttvar 

	reWard[firstPath] = goodput - 5*rttvar
	// fmt.Println("lRTT",lRTT[firstPath].Seconds())
	// reWard[firstPath] = 0

	// reWard[firstPath] = goodput - 15 * lostrate - 5 * (float64(INP[firstPath]) / float64(CWND[firstPath]))



	old_action := sch.list_Action_SACcc[State{pth.pathID, pth.lastRcvdPacketNumber}]
	// var connID string = strconv.FormatFloat(old_action.Action1, 'E', -1, 64)
	connID := fmt.Sprintf("%.6f", old_action.Action1)
	// fmt.Println("DeBUg")


	if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
		
		rewardPayload, ok := tmp_Reload.(RewardPayloadSACcc)
		if !ok {
			fmt.Println("Type assertion failed")
			return
		}
		// if isLoss {
			
		// 	reWard[firstPath] -= 500
		// 	// rewardPayload.Reward += 0
		// 	rewardPayload.Reward += reWard[firstPath]

		// 	// rewardPayload.Reward -= 20
		// 	// fmt.Println("Detect Loss, Decrease Reward : ", rewardPayload.Reward)
		// }

		// if !isLoss {
		// 	rewardPayload.Reward += reWard[firstPath]
		// 	rewardPayload.Reward += 0
		// 	// fmt.Println("Reward :", rewardPayload.Reward)
		// }

		// fmt.Println("Reward Count :", rewardPayload.CountReward)
		rewardPayload.Reward += reWard[firstPath]
		rewardPayload.CountReward += 1

		if rewardPayload.CountReward == 20 {
			rewardPayload.Reward = rewardPayload.Reward / float64(rewardPayload.CountReward)
			rewardPayload.Action = old_action

			tmp_Payload := RewardPayloadSACcc{
				State:     rewardPayload.State,
				NextState: rewardPayload.NextState,
				Action:    rewardPayload.Action,
				Reward:    rewardPayload.Reward,
				Done:      false,
				ModelID:   sch.model_id,
			}
			// fmt.Println("Update reward to Server")
			// updateRewardSACcc(baseURL+"/update_reward", tmp_Payload)
			updateRewardSACcc(tmp_Payload)
			// updateRewardSACcc(sch.conn, tmp_Payload)
			rewardPayload.Reward = 0
			rewardPayload.CountReward = 0
		}

		multiclients.List_Reward_DQN.Set(connID, rewardPayload)
	}
}

func updateRewardSACcc(payload RewardPayloadSACcc) error {
	// fmt.Println("In Update")
	conn, err := net.Dial("udp", "127.0.0.1:8081")
	if err != nil {
		return fmt.Errorf("failed to connect to socket server: %w", err)
	}
	defer conn.Close()

	// Gói tin JSON với lệnh update_reward
	msg := map[string]interface{}{
		"command":    "update_reward",
		"state":      payload.State,
		"action":     payload.Action,
		"reward":     payload.Reward,
		"next_state": payload.NextState,
		"done":       payload.Done,
	}

	jsonPayload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Gửi dữ liệu có newline ở cuối để server nhận biết điểm kết thúc
	_, err = conn.Write(append(jsonPayload, '\n'))
	if err != nil {
		return fmt.Errorf("failed to send data: %w", err)
	}

	// Đọc phản hồi nếu có
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err == nil {
		fmt.Println("Server response:", response)
	} else {
		fmt.Println("No response or error reading response:", err)
	}

	return nil
}
