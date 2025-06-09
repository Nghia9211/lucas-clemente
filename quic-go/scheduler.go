package quic

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"bufio"
	"strconv"
	"time"
	"net"

	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/internal/multiclients"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"gonum.org/v1/gonum/mat"
)

type Store struct {
	Row int8
	Col int8
}

type CurrentStateMulti struct {
	cState_f  int8
	cState_s  int8
	cState_fr int8
	cState_sr int8
}

type scheduler struct {
	// XXX Currently round-robin based, inspired from MPTCP scheduler
	quotas map[protocol.PathID]uint
	// Selected scheduler
	SchedulerName string
	// Is training?
	Training          bool
	AllowedCongestion int

	// async updated reward
	record  uint64
	waiting uint64

	// linUCB
	MAaF [banditDimension][banditDimension]float64
	MAaS [banditDimension][banditDimension]float64
	MbaF [banditDimension]float64
	MbaS [banditDimension]float64

	// QSAT
	QminAlpha   float64
	Qqtable     map[Store]float64
	QoldState   map[State]float64
	Qstate      [11]float64
	QcountState [11]uint32
	QoldQ       float64

	// Qlearning
	qtable     [5][5][2]float64
	clv_state  [4]float64
	clv_state2 [4]float64

	currentState_f  int8
	currentState_s  int8
	currentState_fr int8
	currentState_sr int8

	countSelectPath uint16

	//qtable map[Store] float64
	// oldState map[state] float64
	state      [11]float64
	countState [5][5]uint32
	// f_cTable map[state] int8
	// s_cTable map[state] int8
	preRTT map[protocol.PathID]time.Duration
	iRTT   map[protocol.PathID]float64

	Epsilon    float64
	Alpha      float64
	Beta       float64
	Delta      float64
	Gamma      float64
	AdaDivisor float64

	//MultiClients
	list_State map[State]CurrentStateMulti

	// Retrans cache
	retrans map[protocol.PathID]uint64

	// Write experiences
	DumpExp bool
	// DumpPath  string
	// dumpAgent experienceAgent

	//SAC
	list_State_DQN    map[State]StateDQN
	list_Action_DQN   map[State]float64
	current_State_DQN StateDQN
	current_Prob      float64
	current_Reward    float64
	count_Reward      uint16
	model_id          uint64
	time_Get_Action   time.Time
	list_Reward_DQN   map[float64]RewardPayload

	//SACMulti
	list_State_SACMulti    map[State]StateSACMulti
	list_Action_SACMulti   map[State]float64
	list_Reward_SACMulti   map[float64]RewardPayloadSACMulti
	current_State_SACMulti StateSACMulti
	count_Action           uint16

	//SACMultiJoinCC
	list_Action_SACMultiJoinCC map[State]ActionJoinCC
	list_Reward_SACMultiJoinCC map[float64]RewardPayloadSACMultiJoinCC
	current_Prob_JoinCC        ActionJoinCC

	//SAC-CC
	list_State_SACcc    map[State]StateSACcc
	list_Action_SACcc   map[State]ActionSACCC
	list_Reward_SACcc   map[float64]RewardPayloadSACcc
	current_State_SACcc StateSACcc
	current_Prob_SACcc  ActionSACCC

	//log
	csvwriter_state     *csv.Writer
	csvwriter_reward    *csv.Writer
	csvwriter_state_dis *csv.Writer
	csvwriter_statistic *csv.Writer //rtt, loss,
	csvwriter_action    *csv.Writer
	csvwriter_lrtt      *csv.Writer //last rtt
	csvwriter_flag      bool


}

func (sch *scheduler) setup() {
	sch.quotas = make(map[protocol.PathID]uint)
	sch.retrans = make(map[protocol.PathID]uint64)

	sch.iRTT = make(map[protocol.PathID]float64)
	sch.preRTT = make(map[protocol.PathID]time.Duration)
	sch.waiting = 0
	sch.current_Prob = 0
	sch.current_Reward = 0
	sch.count_Reward = 0
	sch.time_Get_Action = time.Now()
	sch.list_Reward_DQN = make(map[float64]RewardPayload)
	sch.AdaDivisor = 0
	if sch.SchedulerName == "random" {
		sch.current_Prob = 0.5
	}

	if sch.SchedulerName == "peek" || sch.SchedulerName == "lowband" {
		//Read lin to buffer
		file, err := os.Open("./config/peek")
		if err != nil {
			panic(err)
		}

		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				fmt.Fscanln(file, &sch.MAaF[i][j])
			}
		}
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				fmt.Fscanln(file, &sch.MAaS[i][j])
			}
		}
		for i := 0; i < banditDimension; i++ {
			fmt.Fscanln(file, &sch.MbaF[i])
		}
		for i := 0; i < banditDimension; i++ {
			fmt.Fscanln(file, &sch.MbaS[i])
		}
		file.Close()
	}

	if sch.SchedulerName == "qsat" || sch.SchedulerName == "fuzzyqsat" || sch.SchedulerName == "multiclients" {
		sch.list_State = make(map[State]CurrentStateMulti)
		sch.QoldState = make(map[State]float64)
		sch.Qqtable = make(map[Store]float64)

		var config [6]float64
		f, err := os.Open("./config/qsat")
		if err != nil {
			panic(err)
		}

		// sch.state[0] = 0.05
		// sch.state[1] = 0.10
		// sch.state[2] = 0.15
		// sch.state[3] = 0.20
		// sch.state[4] = 0.30
		// sch.state[5] = 0.40
		// sch.state[6] = 0.60

		sch.clv_state[0] = 0.3
		sch.clv_state[1] = 0.5
		sch.clv_state[2] = 0.7
		sch.clv_state[3] = 0.9

		sch.clv_state2[0] = 0.3
		sch.clv_state2[1] = 0.5
		sch.clv_state2[2] = 0.7
		sch.clv_state2[3] = 0.9

		for i := 0; i < 6; i++ {
			fmt.Fscanln(f, &config[i])
		}

		sch.Alpha = config[0]
		sch.Beta = config[1]
		sch.Delta = config[2]
		sch.Gamma = config[3]
		sch.Epsilon = config[4]
		sch.AdaDivisor = config[5]
		// fmt.Println(sch.Alpha)
		// fmt.Println(sch.Beta)
		// fmt.Println(sch.Delta)
		// fmt.Println(sch.Gamma)
		fmt.Println(sch.Epsilon)

		sch.countSelectPath = 0

		f.Close()
		sch.record = 1
		// b, err := ioutil.ReadFile("/App/config/qsat")
		// if err != nil {
		// 	panic(err)
		// }
		// json.Unmarshal(b, &sch.qtable)
		// fmt.Println(sch.qtable)
	}

	if sch.SchedulerName == "dqn" {
		sch.list_State_DQN = make(map[State]StateDQN)
		// modelType := map[string]string{"model_type": "dqn"}
		// setModel(modelType)
	} else if sch.SchedulerName == "sac" {
		f, err := os.Open("./config/sac")
		if err != nil {
			panic(err)
		}
		var config [2]float64
		for i := 0; i < 2; i++ {
			fmt.Fscanln(f, &config[i])
		}
		sch.Alpha = config[0]
		sch.Beta = config[1]

		sch.list_State_DQN = make(map[State]StateDQN)
		sch.list_Action_DQN = make(map[State]float64)
		sch.model_id = TMP_ModelID
		// fmt.Println("Check_K: ", sch.Alpha)
		sch.count_Action = 0
		// modelType := map[string]string{"model_type": "sac", "model_id": string(sch.model_id)}
		setModel("sac", sch.model_id)
	} else if sch.SchedulerName == "sacmulti" || sch.SchedulerName == "sacrx" {
		sch.list_State_SACMulti = make(map[State]StateSACMulti)
		sch.list_Action_SACMulti = make(map[State]float64)
		sch.list_Reward_SACMulti = make(map[float64]RewardPayloadSACMulti)

		sch.count_Action = 0
		// sch.model_id = TMP_ModelID
		// fmt.Println("CheckllL: ", TMP_ModelID)
		// modelType := map[string]string{"model_type": "sac", "model_id": string(sch.model_id)}
		// setModel("sac", sch.model_id)
		SV_Txbitrate_interface0 = 7
		SV_Txbitrate_interface1 = 7
	} else if sch.SchedulerName == "sacmultiJoinCC" {
		sch.list_State_SACMulti = make(map[State]StateSACMulti)
		sch.list_Action_SACMultiJoinCC = make(map[State]ActionJoinCC)
		sch.list_Reward_SACMultiJoinCC = make(map[float64]RewardPayloadSACMultiJoinCC)
		sch.count_Action = 0
	} else if sch.SchedulerName == "sac-cc" {
		sch.list_State_SACcc = make(map[State]StateSACcc)
		sch.list_Action_SACcc = make(map[State]ActionSACCC)
		sch.list_Reward_SACcc = make(map[float64]RewardPayloadSACcc)

		sch.count_Action = 0

	}
	//log
	sch.model_id = TMP_ModelID
	filePath := "./logs/state.csv"
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	sch.csvwriter_state = csv.NewWriter(f)
	filePath = "./logs/state_dis.csv"
	f2, _ := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	sch.csvwriter_state_dis = csv.NewWriter(f2)
	filePath = "./logs/reward.csv"
	f3, _ := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	sch.csvwriter_reward = csv.NewWriter(f3)
	filePath = "./logs/action.csv"
	f4, _ := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	sch.csvwriter_action = csv.NewWriter(f4)
	filePath = "./logs/statistic.csv"
	f5, _ := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	sch.csvwriter_statistic = csv.NewWriter(f5)
	filePath = "./logs/lrtt.csv"
	f6, _ := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	sch.csvwriter_lrtt = csv.NewWriter(f6)

	sch.csvwriter_flag = true
	// fmt.Println("SETUP Scheduler")
}

func (sch *scheduler) getRetransmission(s *session) (hasRetransmission bool, retransmitPacket *ackhandler.Packet, pth *path) {
	// check for retransmissions first
	for {
		// TODO add ability to reinject on another path
		// XXX We need to check on ALL paths if any packet should be first retransmitted
		s.pathsLock.RLock()
	retransmitLoop:
		for _, pthTmp := range s.paths {
			retransmitPacket = pthTmp.sentPacketHandler.DequeuePacketForRetransmission()
			if retransmitPacket != nil {
				pth = pthTmp
				break retransmitLoop
			}
		}
		s.pathsLock.RUnlock()
		if retransmitPacket == nil {
			break
		}
		hasRetransmission = true
		if sch.SchedulerName == "multiclients" {
			if _, ok := sch.list_State[State{pth.pathID, pth.lastRcvdPacketNumber}]; ok {
				sch.GetStateAndRewardMultiClientsRetrans(s, pth)
			}
		}

		if retransmitPacket.EncryptionLevel != protocol.EncryptionForwardSecure {
			if s.handshakeComplete {
				// Don't retransmit handshake packets when the handshake is complete
				continue
			}
			utils.Debugf("\tDequeueing handshake retransmission for packet 0x%x", retransmitPacket.PacketNumber)
			return
		}
		utils.Debugf("\tDequeueing retransmission of packet 0x%x from path %d", retransmitPacket.PacketNumber, pth.pathID)
		// resend the frames that were in the packet
		for _, frame := range retransmitPacket.GetFramesForRetransmission() {
			switch f := frame.(type) {
			case *wire.StreamFrame:
				s.streamFramer.AddFrameForRetransmission(f)
			case *wire.WindowUpdateFrame:
				// only retransmit WindowUpdates if the stream is not yet closed and the we haven't sent another WindowUpdate with a higher ByteOffset for the stream
				// XXX Should it be adapted to multiple paths?
				currentOffset, err := s.flowControlManager.GetReceiveWindow(f.StreamID)
				if err == nil && f.ByteOffset >= currentOffset {
					s.packer.QueueControlFrame(f, pth)
				}
			case *wire.PathsFrame:
				// Schedule a new PATHS frame to send
				s.schedulePathsFrame()
			default:
				s.packer.QueueControlFrame(frame, pth)
			}
		}
	}
	return
}

func (sch *scheduler) selectPathRoundRobin(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
	// 	return s.paths[protocol.InitialPathID]
	// }else{
	// 	return nil
	// }
	if sch.quotas == nil {
		sch.setup()
	}

	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// TODO cope with decreasing number of paths (needed?)
	var selectedPath *path
	var lowerQuota, currentQuota uint
	var ok bool

	// Max possible value for lowerQuota at the beginning
	lowerQuota = ^uint(0)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do no consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentQuota, ok = sch.quotas[pathID]
		if !ok {
			sch.quotas[pathID] = 0
			currentQuota = 0
		}

		if currentQuota < lowerQuota {
			selectedPath = pth
			lowerQuota = currentQuota
		}
	}

	return selectedPath

}

func (sch *scheduler) selectPathLowLatency(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	utils.Debugf("selectPathLowLatency")
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			utils.Debugf("Only initial path and sending not allowed without retransmission")
			utils.Debugf("SCH RTT - NIL")
			return nil
		}
		utils.Debugf("Only initial path and sending is allowed or has retransmission")
		utils.Debugf("SCH RTT - InitialPath")
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				utils.Debugf("has ret, has stream ret and sRTT == 0")
				utils.Debugf("SCH RTT - Selecting %d by low quota", pathID)
				return pth
			}
		}
	}

	var selectedPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	selectedPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			utils.Debugf("Discarding %d - no has ret and sending is not allowed ", pathID)
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			utils.Debugf("Discarding %d - potentially failed", pathID)
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			utils.Debugf("Discarding %d - currentRTT == 0 and lowerRTT != 0 ", pathID)
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if currentRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[selectedPathID]
			if selectedPath != nil && currentQuota > lowerQuota {
				utils.Debugf("Discarding %d - higher quota ", pathID)
				continue pathLoop
			}
		}

		if currentRTT != 0 && lowerRTT != 0 && selectedPath != nil && currentRTT >= lowerRTT {
			utils.Debugf("Discarding %d - higher SRTT ", pathID)
			continue pathLoop
		}

		// Update
		lowerRTT = currentRTT
		selectedPath = pth
		selectedPathID = pathID
	}
	// dataString2 := fmt.Sprintf("SCH RTT - Selecting %d by low RTT: %f\n", selectedPathID, lowerRTT/1000000)
	// f2, err2 := os.OpenFile("/App/output/packet.data", os.O_APPEND|os.O_WRONLY, 0644)
	// if err2 != nil {
	// 	panic(err2)
	// }
	// defer f2.Close()

	// f2.WriteString(dataString2)
	// utils.Debugf("SCH RTT - Selecting %d by low RTT: %f", selectedPathID, lowerRTT)

	// log state
	if s.perspective == protocol.PerspectiveServer && sch.model_id == 3 && sch.AdaDivisor == 1 {
		lRTT := make(map[protocol.PathID]time.Duration)
		cwnd := make(map[protocol.PathID]protocol.ByteCount)
		inp := make(map[protocol.PathID]protocol.ByteCount)
		for pathID, pth := range s.paths {
			lRTT[pathID] = pth.rttStats.LatestRTT()
			cwnd[pathID] = pth.sentPacketHandler.GetCongestionWindow()
			inp[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		}

		f_sendingRate := 0.0
		s_sendingRate := 0.0
		if float64(lRTT[1]) > 0 {
			f_sendingRate = (float64(cwnd[1]) / float64(lRTT[1])) / (float64(cwnd[1])/float64(lRTT[1]) + float64(cwnd[3])/float64(lRTT[3]))
		}
		if float64(lRTT[3]) > 0 {
			s_sendingRate = (float64(cwnd[3]) / float64(lRTT[3])) / (float64(cwnd[1])/float64(lRTT[1]) + float64(cwnd[3])/float64(lRTT[3]))
		}

		var f_cLevel, s_cLevel int8

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
		//log state

		sch.countState[f_cLevel][s_cLevel]++
		sch.csvwriter_state.Write([]string{
			// fmt.Sprintf("%.0f %.0f", float64(firstPath), float64(secondPath)),
			fmt.Sprintf("%.2f", float64(cwnd[1])/1024),
			fmt.Sprintf("%.2f", float64(inp[1])/1024),
			fmt.Sprintf("%.2f", float64(lRTT[1].Nanoseconds())/1000000),
			fmt.Sprintf("%.2f", float64(cwnd[3])/1024),
			fmt.Sprintf("%.2f", float64(inp[3])/1024),
			fmt.Sprintf("%.2f", float64(lRTT[3].Nanoseconds())/1000000),
			fmt.Sprintf("%d", f_cLevel),
			fmt.Sprintf("%d", s_cLevel),
			fmt.Sprintf("%d", selectedPathID),
		})
		sch.csvwriter_state.Flush() // Gọi Flush() để đảm bảo dữ liệu được ghi ra file
	}

	return selectedPath
}

func (sch *scheduler) selectBLEST(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if currentRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID
	}

	if bestPath == nil {
		if secondBestPath != nil {
			return secondBestPath
		}
		return nil
	}

	if hasRetransmission || bestPath.SendingAllowed() {
		return bestPath
	}

	if secondBestPath == nil {
		return nil
	}
	cwndBest := uint64(bestPath.sentPacketHandler.GetCongestionWindow())
	FirstCo := uint64(protocol.DefaultTCPMSS) * uint64(secondLowerRTT) * (cwndBest*2*uint64(lowerRTT) + uint64(secondLowerRTT) - uint64(lowerRTT))
	BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
	SecondCo := 2 * 1 * uint64(lowerRTT) * uint64(lowerRTT) * (uint64(BSend) - (uint64(secondBestPath.sentPacketHandler.GetBytesInFlight()) + uint64(protocol.DefaultTCPMSS)))

	if FirstCo > SecondCo {
		return nil
	} else {
		return secondBestPath
	}
}

func (sch *scheduler) selectECF(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if currentRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID
	}

	if bestPath == nil {
		if secondBestPath != nil {
			return secondBestPath
		}
		return nil
	}

	if hasRetransmission || bestPath.SendingAllowed() {
		return bestPath
	}

	if secondBestPath == nil {
		return nil
	}

	var queueSize uint64
	getQueueSize := func(s *stream) (bool, error) {
		if s != nil {
			queueSize = queueSize + uint64(s.lenOfDataForWriting())
		}
		return true, nil
	}
	s.streamsMap.Iterate(getQueueSize)

	cwndBest := uint64(bestPath.sentPacketHandler.GetCongestionWindow())
	cwndSecond := uint64(secondBestPath.sentPacketHandler.GetCongestionWindow())
	deviationBest := uint64(bestPath.rttStats.MeanDeviation())
	deviationSecond := uint64(secondBestPath.rttStats.MeanDeviation())

	delta := deviationBest
	if deviationBest < deviationSecond {
		delta = deviationSecond
	}
	xBest := queueSize
	if queueSize < cwndBest {
		xBest = cwndBest
	}

	lhs := uint64(lowerRTT) * (xBest + cwndBest)
	rhs := cwndBest * (uint64(secondLowerRTT) + delta)
	if (lhs * 4) < ((rhs * 4) + sch.waiting*rhs) {
		xSecond := queueSize
		if queueSize < cwndSecond {
			xSecond = cwndSecond
		}
		lhsSecond := uint64(secondLowerRTT) * xSecond
		rhsSecond := cwndSecond * (2*uint64(lowerRTT) + delta)
		if lhsSecond > rhsSecond {
			sch.waiting = 1
			return nil
		}
	} else {
		sch.waiting = 0
	}

	return secondBestPath
}

func (sch *scheduler) selectPathPeek(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if currentRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID

	}

	//Paths
	// firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	// for pathID, _ := range s.paths{
	// 	if pathID != protocol.InitialPathID{
	// 		// fmt.Println("PathID: ", pathID)
	// 		if firstPath == protocol.PathID(255){
	// 			firstPath = pathID
	// 		}else{
	// 			if pathID < firstPath{
	// 				secondPath = firstPath
	// 				firstPath = pathID
	// 			}else{
	// 				secondPath = pathID
	// 			}
	// 		}
	// 	}

	// }
	// if secondPath != protocol.PathID(255) && firstPath != protocol.PathID(255) {
	// 	dataString2 := fmt.Sprintf("%d,%d,%d,%d,%d,%d\n",s.paths[protocol.InitialPathID].sentPacketHandler.GetBytesInFlight(),
	// 																s.paths[firstPath].sentPacketHandler.GetBytesInFlight(),
	// 																s.paths[secondPath].sentPacketHandler.GetBytesInFlight(),
	// 																s.paths[protocol.InitialPathID].sentPacketHandler.GetCongestionWindow(),
	// 																s.paths[firstPath].sentPacketHandler.GetCongestionWindow(),
	// 																s.paths[secondPath].sentPacketHandler.GetCongestionWindow())
	// 	f2, err2 := os.OpenFile("/App/output/state_action.csv", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	// 	if err2 != nil {
	// 		panic(err2)
	// 	}
	// 	defer f2.Close()

	// 	f2.WriteString(dataString2)
	// }

	if bestPath == nil {
		if secondBestPath != nil {
			return secondBestPath
		}
		// if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
		// 	return s.paths[protocol.InitialPathID]
		// }else{
		return nil
		// }
	}
	if bestPath.SendingAllowed() {
		sch.waiting = 0
		return bestPath
	}
	if secondBestPath == nil {
		// if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
		// 	return s.paths[protocol.InitialPathID]
		// }else{
		return nil
		// }
	}

	if hasRetransmission && secondBestPath.SendingAllowed() {
		return secondBestPath
	}
	if hasRetransmission {
		return s.paths[protocol.InitialPathID]
	}

	if sch.waiting == 1 {
		return nil
	} else {
		// Migrate from buffer to local variables
		AaF := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaF.Set(i, j, sch.MAaF[i][j])
			}
		}
		AaS := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaS.Set(i, j, sch.MAaS[i][j])
			}
		}
		baF := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baF.Set(i, 0, sch.MbaF[i])
		}
		baS := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baS.Set(i, 0, sch.MbaS[i])
		}

		//Features
		cwndBest := float64(bestPath.sentPacketHandler.GetCongestionWindow())
		cwndSecond := float64(secondBestPath.sentPacketHandler.GetCongestionWindow())
		BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
		inflightf := float64(bestPath.sentPacketHandler.GetBytesInFlight())
		inflights := float64(secondBestPath.sentPacketHandler.GetBytesInFlight())
		llowerRTT := bestPath.rttStats.LatestRTT()
		lsecondLowerRTT := secondBestPath.rttStats.LatestRTT()
		feature := mat.NewDense(banditDimension, 1, nil)
		if 0 < float64(lsecondLowerRTT) && 0 < float64(llowerRTT) {
			feature.Set(0, 0, cwndBest/float64(llowerRTT))
			feature.Set(2, 0, float64(BSend)/float64(llowerRTT))
			feature.Set(4, 0, inflightf/float64(llowerRTT))
			feature.Set(1, 0, inflights/float64(lsecondLowerRTT))
			feature.Set(3, 0, float64(BSend)/float64(lsecondLowerRTT))
			feature.Set(5, 0, cwndSecond/float64(lsecondLowerRTT))
		} else {
			feature.Set(0, 0, 0)
			feature.Set(2, 0, 0)
			feature.Set(4, 0, 0)
			feature.Set(1, 0, 0)
			feature.Set(3, 0, 0)
			feature.Set(5, 0, 0)
		}

		//Obtain theta
		AaIF := mat.NewDense(banditDimension, banditDimension, nil)
		AaIF.Inverse(AaF)
		thetaF := mat.NewDense(banditDimension, 1, nil)
		thetaF.Product(AaIF, baF)

		AaIS := mat.NewDense(banditDimension, banditDimension, nil)
		AaIS.Inverse(AaS)
		thetaS := mat.NewDense(banditDimension, 1, nil)
		thetaS.Product(AaIS, baS)

		//Obtain bandit value
		thetaFPro := mat.NewDense(1, 1, nil)
		thetaFPro.Product(thetaF.T(), feature)

		thetaSPro := mat.NewDense(1, 1, nil)
		thetaSPro.Product(thetaS.T(), feature)

		//Make decision based on bandit value and stochastic value
		if thetaSPro.At(0, 0) < thetaFPro.At(0, 0) {
			if rand.Intn(100) < 70 {
				sch.waiting = 1
				return nil
			} else {
				sch.waiting = 0
				return secondBestPath
			}
		} else {
			if rand.Intn(100) < 90 {
				sch.waiting = 0
				return secondBestPath
			} else {
				sch.waiting = 1
				return nil
			}
		}
	}

}

func (sch *scheduler) selectPathQSAT(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {

	//return s.paths[protocol.InitialPathID]

	if rand.Float64() > sch.Epsilon {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	if len(s.paths) <= 1 {
		//fmt.Println("len1")
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		//fmt.Println("len2")
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	//Paths
	var availablePaths []protocol.PathID
	for pathID := range s.paths {
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
		}
	}

	var ro, action, action2 int8
	var BSend protocol.ByteCount
	var BSend1 float32

	BSend, _ = s.flowControlManager.SendWindowSize(protocol.StreamID(5))
	BSend1 = float32(BSend) / (float32(protocol.DefaultMaxCongestionWindow) * 300)
	if float64(BSend1) < sch.state[0] {
		ro = 0
	} else if float64(BSend1) < sch.state[1] {
		ro = 1
	} else if float64(BSend1) < sch.state[2] {
		ro = 2
	} else if float64(BSend1) < sch.state[3] {
		ro = 3
	} else if float64(BSend1) < sch.state[4] {
		ro = 4
	} else if float64(BSend1) < sch.state[5] {
		ro = 5
	} else if float64(BSend1) < sch.state[6] {
		ro = 6
	} else {
		ro = 7
	}

	sch.QcountState[ro]++

	if sch.Qqtable[Store{Row: ro, Col: 0}] == 0 && sch.Qqtable[Store{Row: ro, Col: 0}] == sch.Qqtable[Store{Row: ro, Col: 1}] {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.Qqtable[Store{Row: ro, Col: 0}] > sch.Qqtable[Store{Row: ro, Col: 1}] {
		action = 0
		action2 = 1
	} else {
		action = 1
		action2 = 0
	}

	var currentRTT time.Duration
	var lowerRTT time.Duration

	currentRTT = s.paths[availablePaths[action]].rttStats.SmoothedRTT()
	lowerRTT = s.paths[availablePaths[action2]].rttStats.SmoothedRTT()

	if (lowerRTT < currentRTT) && s.paths[availablePaths[action2]].SendingAllowed() {
		return s.paths[availablePaths[action2]]
	}

	if s.paths[availablePaths[action]].SendingAllowed() {
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) selectPathQlearning(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {

	//return s.paths[protocol.InitialPathID]

	// if sch.SchedulerName == "qsat" {
	// 	if rand.Float64() < sch.Epsilon {
	// 		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	// 	}
	// }else if sch.SchedulerName == "fuzzyqsat"{
	// 	ep_tmp := sch.Epsilon / (sch.AdaDivisor - 1/float64(sch.record))
	// 	if rand.Float64() < ep_tmp {
	// 		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	// 	}
	// }

	if rand.Float64() <= sch.Epsilon {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	if len(s.paths) <= 1 {
		//fmt.Println("len1")
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		//fmt.Println("len2")
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	lRTT := make(map[protocol.PathID]time.Duration)
	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	inp := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		lRTT[pathID] = pth.rttStats.LatestRTT()
		cwnd[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		inp[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		if pathID != protocol.InitialPathID {
			// fmt.Println("PathID: ", pathID)
			availablePaths = append(availablePaths, pathID)
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
	f_sendingRate := 0.0
	s_sendingRate := 0.0
	if float64(lRTT[firstPath]) > 0 {
		f_sendingRate = (float64(cwnd[firstPath]) / float64(lRTT[firstPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))
	}
	if float64(lRTT[secondPath]) > 0 {
		s_sendingRate = (float64(cwnd[secondPath]) / float64(lRTT[secondPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))
	}
	//fmt.Println(firstPath, f_sendingRate, secondPath, s_sendingRate)

	var f_cLevel, s_cLevel int8
	var action, action2 protocol.PathID

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

	if sch.qtable[f_cLevel][s_cLevel][0] == 0 && sch.qtable[f_cLevel][s_cLevel][1] == sch.qtable[f_cLevel][s_cLevel][0] {
		//fmt.Println("minrtt1")
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.qtable[f_cLevel][s_cLevel][0] > sch.qtable[f_cLevel][s_cLevel][1] {
		action = 0
		action2 = 1
	} else {
		action = 1
		action2 = 0
	}
	//log state
	if s.perspective == protocol.PerspectiveServer && sch.model_id == 3 && sch.AdaDivisor == 1 {
		sch.countState[f_cLevel][s_cLevel]++
		sch.csvwriter_state.Write([]string{
			fmt.Sprintf("%.2f", float64(cwnd[firstPath])/1024),
			fmt.Sprintf("%.2f", float64(inp[firstPath])/1024),
			fmt.Sprintf("%.2f", float64(lRTT[firstPath].Nanoseconds())/1000000),
			fmt.Sprintf("%.2f", float64(cwnd[secondPath])/1024),
			fmt.Sprintf("%.2f", float64(inp[secondPath])/1024),
			fmt.Sprintf("%.2f", float64(lRTT[secondPath].Nanoseconds())/1000000),
			fmt.Sprintf("%d", f_cLevel),
			fmt.Sprintf("%d", s_cLevel),
			fmt.Sprintf("%d", s.paths[availablePaths[action]].pathID),
		})
		sch.csvwriter_state.Flush() // Gọi Flush() để đảm bảo dữ liệu được ghi ra file
	}
	if s.paths[availablePaths[action]].SendingAllowed() {
		//fmt.Println("qsat")
		return s.paths[availablePaths[action]]
	}

	var currentRTT time.Duration
	var lowerRTT time.Duration

	currentRTT = s.paths[availablePaths[action]].rttStats.SmoothedRTT()
	lowerRTT = s.paths[availablePaths[action2]].rttStats.SmoothedRTT()

	if (lowerRTT < currentRTT) && s.paths[availablePaths[action2]].SendingAllowed() {
		//fmt.Println("minrtt2")
		return s.paths[availablePaths[action2]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		//fmt.Println("nosat")
		return nil
	}

}

func (sch *scheduler) selectPathDQN(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// if rand.Float64() <= sch.Epsilon {
	// 	return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	// }

	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
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

	stateData := UpdateDataDQN{
		State: StateDQN{
			CWNDf: float64(CWND[firstPath]),
			INPf:  float64(INP[firstPath]),
			SRTTf: float64(sRTT[firstPath]),
			CWNDs: float64(CWND[secondPath]),
			INPs:  float64(INP[secondPath]),
			SRTTs: float64(sRTT[secondPath]),
		},
		Done: false,
	}

	// var action int8
	// fmt.Println("state: ", stateData)
	// action := sch.getAction(stateData)
	// fmt.Println("Get Action: ", action)
	sch.current_State_DQN = stateData.State
	//return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)

	// if action == 0 {
	// 	return s.paths[availablePaths[0]]
	// } else if action == 1 {
	// 	return s.paths[availablePaths[1]]
	// }

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) selectPathQlearningMultiClients(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if rand.Float64() <= sch.Epsilon {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	lRTT := make(map[protocol.PathID]time.Duration)
	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	inp := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		lRTT[pathID] = pth.rttStats.LatestRTT()
		cwnd[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		inp[pathID] = pth.sentPacketHandler.GetBytesInFlight()

		if pathID != protocol.InitialPathID {
			// fmt.Println("PathID: ", pathID)
			availablePaths = append(availablePaths, pathID)
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

	//update General Q-learning
	// if _, ok := multiclients.S1[uint64(s.connectionID)];ok {
	// 	//multiclients.S1[uint64(s.connectionID)] = &multiclients.StateMulti{}
	// 	multiclients.S1[uint64(s.connectionID)].FRTT = lRTT[firstPath]
	// 	multiclients.S1[uint64(s.connectionID)].SRTT = lRTT[secondPath]
	// 	multiclients.S1[uint64(s.connectionID)].FCWND = cwnd[firstPath]
	// 	multiclients.S1[uint64(s.connectionID)].SCWND = cwnd[secondPath]
	// 	conID := strconv.Itoa(int(s.connectionID))
	// 	multiclients.S2.Set(conID,multiclients.S1[uint64(s.connectionID)])
	// }

	var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
	if _, ok := multiclients.S2.Get(connID); ok {
		var tmp_value multiclients.StateMulti
		tmp_value.FRTT = lRTT[firstPath]
		tmp_value.SRTT = lRTT[secondPath]
		tmp_value.FCWND = cwnd[firstPath]
		tmp_value.SCWND = cwnd[secondPath]
		tmp_value.FInP = inp[firstPath]
		tmp_value.SInP = inp[secondPath]
		multiclients.S2.Set(connID, tmp_value)
	}

	//Xac dinh trang thai cua mang
	f_sendingRate := (float64(cwnd[firstPath]) / float64(lRTT[firstPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))
	s_sendingRate := (float64(cwnd[secondPath]) / float64(lRTT[secondPath])) / (float64(cwnd[firstPath])/float64(lRTT[firstPath]) + float64(cwnd[secondPath])/float64(lRTT[secondPath]))

	fr_Rate := 0.0
	sr_Rate := 0.0
	var fr_Cap, sr_Cap protocol.ByteCount
	fr_Cap = 0
	sr_Cap = 0
	// if len(multiclients.S1) > 1 {
	// 	for _, element := range multiclients.S1 {
	// 		fr_Rate += (float64(element.FCWND) / float64(element.FRTT)) / (float64(element.FCWND) / float64(element.FRTT) + float64(element.SCWND) / float64(element.SRTT))
	// 		sr_Rate += (float64(element.SCWND) / float64(element.SRTT)) / (float64(element.FCWND) / float64(element.FRTT) + float64(element.SCWND) / float64(element.SRTT))
	// 	}

	// 	fr_Rate = (fr_Rate - f_sendingRate) / float64(len(multiclients.S1) - 1)
	// 	sr_Rate = (sr_Rate - s_sendingRate) / float64(len(multiclients.S1) - 1)
	// }
	// fmt.Println("In tap S2:")
	if multiclients.S2.Count() > 1 {
		// var tmp_v multiclients.StateMulti
		ItemsList := multiclients.S2.Items()
		for _, element := range ItemsList {
			// fmt.Println(element)
			if foo, ok := element.(multiclients.StateMulti); ok {
				// fmt.Println("FRTT: ", foo.FRTT, "SRTT: ", foo.SRTT, "FCWND: ", foo.FCWND, "SCWND: ", foo.SCWND)
				fr_Rate += (float64(foo.FCWND) / float64(foo.FRTT)) / (float64(foo.FCWND)/float64(foo.FRTT) + float64(foo.SCWND)/float64(foo.SRTT))
				sr_Rate += (float64(foo.SCWND) / float64(foo.SRTT)) / (float64(foo.FCWND)/float64(foo.FRTT) + float64(foo.SCWND)/float64(foo.SRTT))
				fr_Cap += foo.FCWND
				sr_Cap += foo.SCWND
			}
		}
		fr_Rate = (fr_Rate - f_sendingRate) / float64(multiclients.S2.Count()-1)
		sr_Rate = (sr_Rate - s_sendingRate) / float64(multiclients.S2.Count()-1)
		// fmt.Println("State sum: ", fr_Rate, sr_Rate)
	}

	var f_cLevel, s_cLevel, fr_cLevel, sr_cLevel int8
	var action, action2 protocol.PathID

	// tmp_para := 0.3
	// f_sendingRate = (1-tmp_para)*f_sendingRate + tmp_para*fr_Rate
	// s_sendingRate = (1-tmp_para)*s_sendingRate + tmp_para*sr_Rate
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

	if fr_Rate < sch.clv_state2[0] {
		fr_cLevel = 0
	} else if fr_Rate < sch.clv_state2[1] {
		fr_cLevel = 1
	} else if fr_Rate < sch.clv_state2[2] {
		fr_cLevel = 2
	} else if fr_Rate < sch.clv_state2[3] {
		fr_cLevel = 3
	} else {
		fr_cLevel = 4
	}

	if sr_Rate >= sch.clv_state2[0] {
		sr_cLevel = 0
	} else if sr_Rate >= sch.clv_state2[1] {
		sr_cLevel = 1
	} else if sr_Rate >= sch.clv_state2[2] {
		sr_cLevel = 2
	} else if sr_Rate >= sch.clv_state2[3] {
		sr_cLevel = 3
	} else {
		sr_cLevel = 4
	}

	sch.currentState_f = f_cLevel
	sch.currentState_s = s_cLevel
	sch.currentState_fr = fr_cLevel
	sch.currentState_sr = sr_cLevel
	//fmt.Println(f_cLevel, s_cLevel, fr_cLevel, sr_cLevel)
	multiclients.Flag_update = true
	// sch.countState[f_cLevel][s_cLevel]++

	if multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][0] == 0 && multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][1] == 0 {
		//fmt.Println("minrtt1")
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][0] > multiclients.MultiQtable[f_cLevel][s_cLevel][fr_cLevel][sr_cLevel][1] {
		action = 0
		action2 = 1
	} else {
		action = 1
		action2 = 0
	}

	// if _, ok := sch.list_State[protocol.PacketNumber];!ok {
	// 	sch.list_State[protocol.PacketNumber] = &CurrentStateMulti{}
	// }

	if s.paths[availablePaths[action]].SendingAllowed() {
		//fmt.Println("qsat")
		if (action == 0 && fr_Cap < 1024*1024) || (action == 1 && sr_Cap < 1024*1024) {
			sch.countSelectPath++
			// if secondPath != protocol.PathID(255) && firstPath != protocol.PathID(255) {
			// 	tmp_str := "./logs/state_action_"+strconv.Itoa(int(s.connectionID))+".csv"
			// 	if _, err := os.Stat(tmp_str); err != nil {
			// 		file, _ := os.Create(tmp_str)
			// 		defer file.Close()
			// 	}
			// 	dataString2 := fmt.Sprintf("%d,%d,%d,%d,%d,%d\n", 	sch.countSelectPath, s.paths[firstPath].sentPacketHandler.GetBytesInFlight(),
			// 														s.paths[secondPath].sentPacketHandler.GetBytesInFlight(),
			// 														s.paths[firstPath].sentPacketHandler.GetCongestionWindow(),
			// 														s.paths[secondPath].sentPacketHandler.GetCongestionWindow(),
			// 														uint16(availablePaths[action]))
			// 	f2, err2 := os.OpenFile(tmp_str, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			// 	if err2 != nil {
			// 		panic(err2)
			// 	}
			// 	defer f2.Close()

			// 	f2.WriteString(dataString2)
			// }
			return s.paths[availablePaths[action]]
		}
	}

	var currentRTT time.Duration
	var lowerRTT time.Duration

	currentRTT = s.paths[availablePaths[action]].rttStats.SmoothedRTT()
	lowerRTT = s.paths[availablePaths[action2]].rttStats.SmoothedRTT()

	if (lowerRTT < currentRTT) && s.paths[availablePaths[action2]].SendingAllowed() {
		//fmt.Println("minrtt2")
		if (action2 == 0 && fr_Cap < 1024*1024) || (action2 == 1 && sr_Cap < 1024*1024) {
			sch.countSelectPath++
			// if secondPath != protocol.PathID(255) && firstPath != protocol.PathID(255) {
			// 	tmp_str := "./logs/state_action_"+strconv.Itoa(int(s.connectionID))+".csv"
			// 	if _, err := os.Stat(tmp_str); err != nil {
			// 		file, _ := os.Create(tmp_str)
			// 		defer file.Close()
			// 	}
			// 	dataString2 := fmt.Sprintf("%d,%d,%d,%d,%d,%d\n", 	sch.countSelectPath, s.paths[firstPath].sentPacketHandler.GetBytesInFlight(),
			// 														s.paths[secondPath].sentPacketHandler.GetBytesInFlight(),
			// 														s.paths[firstPath].sentPacketHandler.GetCongestionWindow(),
			// 														s.paths[secondPath].sentPacketHandler.GetCongestionWindow(),
			// 														uint16(availablePaths[action2]))
			// 	f2, err2 := os.OpenFile(tmp_str, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			// 	if err2 != nil {
			// 		panic(err2)
			// 	}
			// 	defer f2.Close()

			// 	f2.WriteString(dataString2)
			// }
			return s.paths[availablePaths[action2]]
		}
	}

	return nil

	// if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
	// 	sch.countSelectPath++
	// 	// if secondPath != protocol.PathID(255) && firstPath != protocol.PathID(255) {
	// 	// 	tmp_str := "./logs/state_action_"+strconv.Itoa(int(s.connectionID))+".csv"
	// 	// 	if _, err := os.Stat(tmp_str); err != nil {
	// 	// 		file, _ := os.Create(tmp_str)
	// 	// 		defer file.Close()
	// 	// 	}
	// 	// 	dataString2 := fmt.Sprintf("%d,%d,%d,%d,%d,%d\n", 	sch.countSelectPath,
	// 	// 														s.paths[firstPath].sentPacketHandler.GetBytesInFlight(),
	// 	// 														s.paths[secondPath].sentPacketHandler.GetBytesInFlight(),
	// 	// 														s.paths[firstPath].sentPacketHandler.GetCongestionWindow(),
	// 	// 														s.paths[secondPath].sentPacketHandler.GetCongestionWindow(),
	// 	// 														uint16(protocol.InitialPathID))
	// 	// 	f2, err2 := os.OpenFile(tmp_str, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	// 	// 	if err2 != nil {
	// 	// 		panic(err2)
	// 	// 	}
	// 	// 	defer f2.Close()

	// 	// 	f2.WriteString(dataString2)
	// 	// }
	// 	return s.paths[protocol.InitialPathID]
	// } else {
	// 	//fmt.Println("nosat")
	// 	return nil
	// }

}

// Lock of s.paths must be held
func (sch *scheduler) selectPath(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Currently round-robin
	if sch.SchedulerName == "rtt" {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "random" {
		return sch.selectPathRandom(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "peek" {
		return sch.selectPathPeek(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "ecf" {
		return sch.selectECF(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "blest" {
		return sch.selectBLEST(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "dqn" {
		return sch.selectPathDQN(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "sac" {
		return sch.selectPathSAC(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "sacmulti" {
		return sch.selectPathSACMulti(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "sacmultiJoinCC" {
		return sch.selectPathSACMultiJoinCC(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "sacrx" {
		return sch.selectPathSACRx(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "qsat" {
		return sch.selectPathQSAT(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "fuzzyqsat" {
		return sch.selectPathQlearning(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "multiclients" {
		return sch.selectPathQlearningMultiClients(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.SchedulerName == "sac-cc" {
		return sch.selectPathSACcc(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else {
		// Default, rtt
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}
	// return sch.selectPathRoundRobin(s, hasRetransmission, hasStreamRetransmission, fromPth)
}

// Lock of s.paths must be free (in case of log print)
func (sch *scheduler) performPacketSending(s *session, windowUpdateFrames []*wire.WindowUpdateFrame, pth *path) (*ackhandler.Packet, bool, error) {
	// add a retransmittable frame
	if pth.sentPacketHandler.ShouldSendRetransmittablePacket() {
		s.packer.QueueControlFrame(&wire.PingFrame{}, pth)
	}
	packet, err := s.packer.PackPacket(pth)
	if err != nil || packet == nil {
		return nil, false, err
	}
	if err = s.sendPackedPacket(packet, pth); err != nil {
		return nil, false, err
	}

	// send every window update twice
	for _, f := range windowUpdateFrames {
		s.packer.QueueControlFrame(f, pth)
	}

	// Packet sent, so update its quota
	sch.quotas[pth.pathID]++

	sRTT := make(map[protocol.PathID]time.Duration)
	restranNumber := make(map[protocol.PathID]uint64)
	pktNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)
	cwnd := make(map[protocol.PathID]protocol.ByteCount)

	// Provide some logging if it is the last packet
	for _, frame := range packet.frames {
		switch frame := frame.(type) {
		case *wire.StreamFrame:
			if frame.FinBit {
				// Last packet to send on the stream, print stats
				s.pathsLock.RLock()
				utils.Infof("Info for stream %x of %x", frame.StreamID, s.connectionID)
				for pathID, pth := range s.paths {
					sntPkts, sntRetrans, sntLost := pth.sentPacketHandler.GetStatistics()
					rcvPkts := pth.receivedPacketHandler.GetStatistics()
					sRTT[pathID] = pth.rttStats.SmoothedRTT()
					cwnd[pathID] = pth.sentPacketHandler.GetCongestionWindow()
					utils.Infof("Path %x: sent %d retrans %d lost %d; rcv %d rtt %v; loss rate: %f", pathID, sntPkts, sntRetrans, sntLost, rcvPkts, sRTT[pathID], float64(sntLost)/float64(sntPkts))
					// TODO: Remove it
					utils.Infof("Congestion Window: %d", cwnd[pathID])
					restranNumber[pathID] = sntRetrans
					pktNumber[pathID] = sntPkts
					lostNumber[pathID] = sntLost
				}

				// if sch.SchedulerName == "multiclients" || sch.SchedulerName == "sacmulti" {
				// 	var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
				// 	multiclients.S2.Remove(connID)
				// 	// utils.Infof("countSession: %d", multiclients.NumSession)
				// }
				var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
				multiclients.S2.Remove(connID)
				//logs
				if s.perspective == protocol.PerspectiveServer && sch.model_id == 3 && sch.csvwriter_flag {
					sch.csvwriter_statistic.Write([]string{
						// fmt.Sprintf("%.0f %.0f", float64(firstPath), float64(secondPath)),
						fmt.Sprintf("%x",s.connectionID),
						// fmt.Sprintf("%.3f", float64(sRTT[1].Nanoseconds())/1000000),
						fmt.Sprintf("%d", pktNumber[1]),
						fmt.Sprintf("%d", restranNumber[1]),
						fmt.Sprintf("%d", lostNumber[1]),
						fmt.Sprintf("%.3f", 100*float64(restranNumber[1])/float64(pktNumber[1])),
						fmt.Sprintf("%.3f", 100*float64(lostNumber[1])/float64(pktNumber[1])),
						// fmt.Sprintf("%.3f", float64(sRTT[3].Nanoseconds())/1000000),
						// fmt.Sprintf("%.3f", 100*float64(restranNumber[3])/float64(pktNumber[3])),
						// fmt.Sprintf("%.3f", 100*float64(lostNumber[3])/float64(pktNumber[3])),
					})
					sch.csvwriter_statistic.Flush()
					sch.csvwriter_flag = false
				}

				s.pathsLock.RUnlock()

				//Write lin parameters for Peekaboo
				if sch.SchedulerName == "peek" {
					os.Remove("./config/peek")
					os.Create("./config/peek")
					file2, _ := os.OpenFile("./config/peek", os.O_WRONLY, 0600)
					for i := 0; i < banditDimension; i++ {
						for j := 0; j < banditDimension; j++ {
							fmt.Fprintf(file2, "%.8f\n", sch.MAaF[i][j])
						}
					}
					for i := 0; i < banditDimension; i++ {
						for j := 0; j < banditDimension; j++ {
							fmt.Fprintf(file2, "%.8f\n", sch.MAaS[i][j])
						}
					}
					for j := 0; j < banditDimension; j++ {
						fmt.Fprintf(file2, "%.8f\n", sch.MbaF[j])
					}
					for j := 0; j < banditDimension; j++ {
						fmt.Fprintf(file2, "%.8f\n", sch.MbaS[j])
					}
					file2.Close()
				}
				if sch.SchedulerName == "fuzzyqsat" && sch.model_id == 3 && sch.AdaDivisor == 1 {
					//log
					for _, row := range sch.countState {
						var strRow []string
						for _, val := range row {
							strRow = append(strRow, strconv.FormatUint(uint64(val), 10))
						}
						sch.csvwriter_state_dis.Write(strRow)
						sch.csvwriter_state_dis.Flush()
					}
				}
			}
		default:
		}
	}

	pkt := &ackhandler.Packet{
		PacketNumber:    packet.number,
		Frames:          packet.frames,
		Length:          protocol.ByteCount(len(packet.raw)),
		EncryptionLevel: packet.encryptionLevel,
	}

	return pkt, true, nil
}

// Lock of s.paths must be free
func (sch *scheduler) ackRemainingPaths(s *session, totalWindowUpdateFrames []*wire.WindowUpdateFrame) error {
	// Either we run out of data, or CWIN of usable paths are full
	// Send ACKs on paths not yet used, if needed. Either we have no data to send and
	// it will be a pure ACK, or we will have data in it, but the CWIN should then
	// not be an issue.
	s.pathsLock.RLock()
	defer s.pathsLock.RUnlock()
	// get WindowUpdate frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdateFrames := totalWindowUpdateFrames
	if len(windowUpdateFrames) == 0 {
		windowUpdateFrames = s.getWindowUpdateFrames(s.peerBlocked)
	}
	for _, pthTmp := range s.paths {
		ackTmp := pthTmp.GetAckFrame()
		for _, wuf := range windowUpdateFrames {
			s.packer.QueueControlFrame(wuf, pthTmp)
		}
		if ackTmp != nil || len(windowUpdateFrames) > 0 {
			if pthTmp.pathID == protocol.InitialPathID && ackTmp == nil {
				continue
			}
			swf := pthTmp.GetStopWaitingFrame(false)
			if swf != nil {
				s.packer.QueueControlFrame(swf, pthTmp)
			}
			s.packer.QueueControlFrame(ackTmp, pthTmp)
			// XXX (QDC) should we instead call PackPacket to provides WUFs?
			var packet *packedPacket
			var err error
			if ackTmp != nil {
				// Avoid internal error bug
				packet, err = s.packer.PackAckPacket(pthTmp)
			} else {
				packet, err = s.packer.PackPacket(pthTmp)
			}
			if err != nil {
				return err
			}
			err = s.sendPackedPacket(packet, pthTmp)
			if err != nil {
				return err
			}
		}
	}
	s.peerBlocked = false
	return nil
}

func (sch *scheduler) sendPacket(s *session) error {
	var pth *path

	// Update leastUnacked value of paths
	s.pathsLock.RLock()
	for _, pthTmp := range s.paths {
		pthTmp.SetLeastUnacked(pthTmp.sentPacketHandler.GetLeastUnacked())
	}
	s.pathsLock.RUnlock()

	// get WindowUpdate frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdateFrames := s.getWindowUpdateFrames(false)
	for _, wuf := range windowUpdateFrames {
		s.packer.QueueControlFrame(wuf, pth)
	}

	// Repeatedly try sending until we don't have any more data, or run out of the congestion window
	for {
		// We first check for retransmissions
		hasRetransmission, retransmitHandshakePacket, fromPth := sch.getRetransmission(s)
		// XXX There might still be some stream frames to be retransmitted
		hasStreamRetransmission := s.streamFramer.HasFramesForRetransmission()

		// Select the path here
		s.pathsLock.RLock()
		pth = sch.selectPath(s, hasRetransmission, hasStreamRetransmission, fromPth)
		s.pathsLock.RUnlock()

		//logs action
		if s.perspective == protocol.PerspectiveServer && sch.model_id == 3 && pth != nil && sch.AdaDivisor == 1 {
			s.scheduler.csvwriter_action.Write([]string{
				fmt.Sprintf("%d", int(pth.pathID)),
			})
			s.scheduler.csvwriter_action.Flush()
		}

		// sRTT := make(map[protocol.PathID]time.Duration)
		// cwndlevel := make(map[protocol.PathID]float32)
		// cwndlevel[1] = 0
		// cwndlevel[3] = 0
		// for pathID, path := range s.paths {
		// 	if pathID != protocol.InitialPathID {
		// 		sRTT[pathID] = path.rttStats.SmoothedRTT()
		// 		if float32(path.sentPacketHandler.GetCongestionWindow()) != 0 {
		// 			cwndlevel[pathID] = float32(path.sentPacketHandler.GetBytesInFlight()) / float32(path.sentPacketHandler.GetCongestionWindow())
		// 		}else {
		// 			cwndlevel[pathID] = 0
		// 		}
		// 	}
		// }

		// 	//fileNameTmp := "/App/output/" + string(s.connectionID) + ".csv"
		// 	// fileNameTmp := fmt.Sprintf("/App/tmp/%d.csv", s.connectionID)

		// 	// _ , err22 := os.Stat(fileNameTmp)
		// 	// if os.IsNotExist(err22) {
		// 	// 	_, err := os.Create(fileNameTmp)
		// 	// 	if err != nil{
		// 	// 		panic(err)
		// 	// 	}
		// 	// }

		// 	// ff, _ := os.OpenFile(fileNameTmp, os.O_APPEND|os.O_WRONLY, 0644)
		// 	//fmt.Printf("%s - %d\n", fileNameTmp, durationTmp)

		// 	// durationTmp := time.Since(multiclients.ServerCreationTime)
		// 	// dataString := fmt.Sprintf("%d,%d,%f,%f,%f\n",s.connectionID,pathIDTmp,float64(durationTmp.Nanoseconds())/1000000.0, cwndlevel[1],cwndlevel[3])
		// 	// ff.WriteString(dataString)
		// 	// defer ff.Close()

		// }
		// XXX No more path available, should we have a new QUIC error message?
		if pth == nil {
			windowUpdateFrames := s.getWindowUpdateFrames(false)
			return sch.ackRemainingPaths(s, windowUpdateFrames)
		}

		// If we have an handshake packet retransmission, do it directly
		if hasRetransmission && retransmitHandshakePacket != nil {
			s.packer.QueueControlFrame(pth.sentPacketHandler.GetStopWaitingFrame(true), pth)
			packet, err := s.packer.PackHandshakeRetransmission(retransmitHandshakePacket, pth)
			if err != nil {
				return err
			}

			if err = s.sendPackedPacket(packet, pth); err != nil {
				return err
			}
			continue
		}

		// XXX Some automatic ACK generation should be done someway
		var ack *wire.AckFrame

		ack = pth.GetAckFrame()
		if ack != nil {
			s.packer.QueueControlFrame(ack, pth)
		}
		if ack != nil || hasStreamRetransmission {
			swf := pth.sentPacketHandler.GetStopWaitingFrame(hasStreamRetransmission)
			if swf != nil {
				s.packer.QueueControlFrame(swf, pth)
			}
		}

		// Also add CLOSE_PATH frames, if any
		for cpf := s.streamFramer.PopClosePathFrame(); cpf != nil; cpf = s.streamFramer.PopClosePathFrame() {
			s.packer.QueueControlFrame(cpf, pth)
		}

		// Also add ADD ADDRESS frames, if any
		for aaf := s.streamFramer.PopAddAddressFrame(); aaf != nil; aaf = s.streamFramer.PopAddAddressFrame() {
			s.packer.QueueControlFrame(aaf, pth)
		}

		// Also add PATHS frames, if any
		for pf := s.streamFramer.PopPathsFrame(); pf != nil; pf = s.streamFramer.PopPathsFrame() {
			s.packer.QueueControlFrame(pf, pth)
		}
		// Cwnd := float32(pth.sentPacketHandler.GetCongestionWindow()) / float32(protocol.DefaultMaxCongestionWindow) / 300
		// Swnd_tmp, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
		// Swnd := float32(Swnd_tmp) / float32(protocol.DefaultMaxCongestionWindow) / 300

		pkt, sent, err := sch.performPacketSending(s, windowUpdateFrames, pth)
		if err != nil {
			// if err == ackhandler.ErrTooManyTrackedSentPackets {
			// 	// utils.Errorf("Closing episode")
			// 	// if sch.SchedulerName == "dqnAgent" && sch.Training {
			// 	// 	sch.TrainingAgent.CloseEpisode(uint64(s.connectionID), -100, false)
			// 	// }
			// }
			return err
		}

		windowUpdateFrames = nil
		if !sent {
			// Prevent sending empty packets
			return sch.ackRemainingPaths(s, windowUpdateFrames)
		}

		if sch.SchedulerName == "multiclients" && pth.pathID > 0 && pkt.PacketNumber > 0 && multiclients.Flag_update {
			//sch.list_State[State{pth.pathID, pkt.PacketNumber}] = CurrentStateMulti{sch.currentState_f, sch.currentState_s, sch.currentState_fr, sch.currentState_sr}
			multiclients.Flag_update = false
		}

		if sch.SchedulerName == "sac" && pth.pathID > 0 && pkt.PacketNumber > 0 {
			sch.list_State_DQN[State{pth.pathID, pkt.PacketNumber}] = sch.current_State_DQN
			sch.list_Action_DQN[State{pth.pathID, pkt.PacketNumber}] = sch.current_Prob
			//fmt.Println(sch.current_Prob)
		}

		if sch.SchedulerName == "sacmulti" && pth.pathID > 0 && pkt.PacketNumber > 0 {
			sch.list_State_SACMulti[State{pth.pathID, pkt.PacketNumber}] = sch.current_State_SACMulti
			sch.list_Action_SACMulti[State{pth.pathID, pkt.PacketNumber}] = sch.current_Prob
			//fmt.Println(sch.current_Prob)
		}

		if sch.SchedulerName == "sacmultiJoinCC" && pth.pathID > 0 && pkt.PacketNumber > 0 {
			sch.list_State_SACMulti[State{pth.pathID, pkt.PacketNumber}] = sch.current_State_SACMulti
			sch.list_Action_SACMultiJoinCC[State{pth.pathID, pkt.PacketNumber}] = sch.current_Prob_JoinCC
			//fmt.Println(sch.current_Prob)
		}

		if sch.SchedulerName == "sac-cc" && pth.pathID > 0 && pkt.PacketNumber > 0 {
			sch.list_State_SACcc[State{pth.pathID, pkt.PacketNumber}] = sch.current_State_SACcc
			sch.list_Action_SACcc[State{pth.pathID, pkt.PacketNumber}] = sch.current_Prob_SACcc
		}

		if sch.SchedulerName == "qsat" && pth.pathID > 0 && pkt.PacketNumber > 0 {
			BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
			BSend1 := float32(BSend) / float32(protocol.DefaultMaxCongestionWindow) / 300
			sch.QoldState[State{id: pth.pathID, pktnumber: pkt.PacketNumber}] = float64(BSend1)
		}

		// Duplicate traffic when it was sent on an unknown performing path
		// FIXME adapt for new paths coming during the connection
		if pth.rttStats.SmoothedRTT() == 0 {
			currentQuota := sch.quotas[pth.pathID]
			// Was the packet duplicated on all potential paths?
		duplicateLoop:
			for pathID, tmpPth := range s.paths {
				if pathID == protocol.InitialPathID || pathID == pth.pathID {
					continue
				}
				if sch.quotas[pathID] < currentQuota && tmpPth.sentPacketHandler.SendingAllowed() {
					// Duplicate it
					pth.sentPacketHandler.DuplicatePacket(pkt)
					break duplicateLoop
				}
			}
		}

		// And try pinging on potentially failed paths
		if fromPth != nil && fromPth.potentiallyFailed.Get() {
			err = s.sendPing(fromPth)
			if err != nil {
				return err
			}
		}
	}
}

func (sch *scheduler) selectPathRandom(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	var action protocol.PathID
	action = 1
	if r.Float64() > 0.5 {
		action = 3
	}
	if s.paths[action].SendingAllowed() {
		return s.paths[action]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) selectPathSAC(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// if rand.Float64() <= sch.Epsilon {
	// 	return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	// }
	//fmt.Println("CHECKKK")

	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
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

	stateData := StateDQN{
		CWNDf: float64(CWND[firstPath]) / float64(protocol.DefaultTCPMSS),
		INPf:  float64(INP[firstPath]) / float64(protocol.DefaultTCPMSS),
		SRTTf: NormalizeTimes(sRTT[firstPath]) / 10,
		CWNDs: float64(CWND[secondPath]) / float64(protocol.DefaultTCPMSS),
		INPs:  float64(INP[secondPath]) / float64(protocol.DefaultTCPMSS),
		SRTTs: NormalizeTimes(sRTT[secondPath]) / 10,
	}

	if sch.current_Prob == 0 {
		sch.current_Prob = 1
		// rewardPayload := sch.list_Reward_DQN[sch.current_Prob]
		// rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		// fmt.Println("State: ", stateData)

		sch.getActionAsync(baseURL+"/get_action", stateData, sch.model_id)
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.current_Prob == 1 {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	// elapsed := time.Since(sch.time_Get_Action)

	if sch.count_Action >= uint16(sch.Alpha) {
		// fmt.Println("PayLoad: ", rewardPayload)
		// rewardPayload := sch.list_Reward_DQN[sch.current_Prob]
		// rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
			rewardPayload, ok := tmp_Reload.(RewardPayload)
			if !ok {
				fmt.Println("Type assertion failed")
				return nil
			}
			rewardPayload.NextState = stateData
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
			// fmt.Println("State: ", stateData, rewardPayload)
		}

		sch.current_State_DQN = stateData
		sch.getActionAsync(baseURL+"/get_action", stateData, sch.model_id)

		sch.count_Action = 0
		sch.count_Reward = 0
		sch.current_Reward = 0
		sch.time_Get_Action = time.Now()
	} else {
		sch.count_Action += 1
	}

	action := 0
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	if sch.current_Prob > r.Float64() {
		action = 1
	}
	if s.paths[availablePaths[action]].SendingAllowed() {
		// sch.current_State_DQN = stateData
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

// func getAction(url string, state StateDQN) (float64, error) {
// 	jsonPayload, err := json.Marshal(map[string]interface{}{
// 		"state": state,
// 	})
// 	if err != nil {
// 		return 0, err
// 	}

// 	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
// 	if err != nil {
// 		return 0, err
// 	}
// 	defer resp.Body.Close()

// 	var response ActionProbabilityResponse
// 	err = json.NewDecoder(resp.Body).Decode(&response)
// 	if err != nil {
// 		return 0, err
// 	}

// 	if response.Error != "" {
// 		return 0, fmt.Errorf(response.Error)
// 	}

// 	return response.Probability[0], nil
// }

func (sch *scheduler) getActionAsync(url string, state StateDQN, model_id uint64) {
	go func() {
		// time_now := time.Now()
		jsonPayload, err := json.Marshal(map[string]interface{}{
			"state":    state,
			"model_id": model_id,
		})

		// fmt.Println("GetAction: ", model_id)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
		defer resp.Body.Close()

		var response ActionProbabilityResponse
		err = json.NewDecoder(resp.Body).Decode(&response)
		if err != nil {
			fmt.Println("Error decoding response:", err)
			return
		}

		if response.Error != "" {
			fmt.Println("Server returned error:", response.Error)
			return
		}

		// fmt.Println("Received action probability:", response.Probability[0])
		sch.current_Prob = response.Probability[0]

		rewardPayload := RewardPayload{
			State:       state,
			NextState:   state,
			Action:      sch.current_Prob,
			Reward:      0.0,
			Done:        false,
			ModelID:     sch.model_id,
			CountReward: 0,
		}
		var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		// elaped := time.Since(time_now).Milliseconds()
		// fmt.Println("Time_getAction: ", elaped)
	}()
}

func (sch *scheduler) selectPathSACRx(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	lostNumber := make(map[protocol.PathID]uint64)
	goodPut := make(map[protocol.PathID]float64)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		packetNumber[pathID], retransNumber[pathID], lostNumber[pathID] = pth.sentPacketHandler.GetStatistics()
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		goodPut[pathID] = NormalizeGoodput(s, packetNumber[pth.pathID], retransNumber[pth.pathID])
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
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

	var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
	if _, ok := multiclients.S2.Get(connID); ok {
		var tmp_value multiclients.StateMulti
		tmp_value.FRTT = lRTT[firstPath]
		tmp_value.SRTT = lRTT[secondPath]
		tmp_value.FCWND = CWND[firstPath]
		tmp_value.SCWND = CWND[secondPath]
		tmp_value.FInP = INP[firstPath]
		tmp_value.SInP = INP[secondPath]
		multiclients.S2.Set(connID, tmp_value)
	}

	var CWNDf_total, INPf_total, CWNDs_total, INPs_total protocol.ByteCount
	var SRTTf_total, SRTTs_total time.Duration
	CWNDf_total = 0
	INPf_total = 0
	CWNDs_total = 0
	INPs_total = 0
	SRTTf_total = 0
	SRTTs_total = 0

	CWNDf_mean := 0.0
	INPf_mean := 0.0
	CWNDs_mean := 0.0
	INPs_mean := 0.0
	// SRTTf_mean := 0.0
	// SRTTs_mean := 0.0

	if multiclients.S2.Count() > 1 {
		ItemsList := multiclients.S2.Items()
		for _, element := range ItemsList {
			if foo, ok := element.(multiclients.StateMulti); ok {
				CWNDf_total += foo.FCWND
				INPf_total += foo.FInP
				CWNDs_total += foo.SCWND
				INPs_total += foo.FInP
				SRTTf_total += foo.FRTT
				SRTTs_total += foo.SRTT
			}
		}
		CWNDf_mean = float64(CWNDf_total)
		INPf_mean = float64(INPf_total)
		CWNDs_mean = float64(CWNDs_total)
		INPs_mean = float64(INPs_total)
	}

	stateData := StateSACMulti{
		CWNDf: float64(CWND[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPf:  float64(INP[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTf: NormalizeTimes(sRTT[firstPath]) / 100.0 / 1000000.0,
		CWNDs: float64(CWND[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPs:  float64(INP[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTs: NormalizeTimes(sRTT[secondPath]) / 100.0 / 1000000.0,

		CWNDf_all: CWNDf_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPf_all:  INPf_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTf_all: SV_Txbitrate_interface0,
		CWNDs_all: CWNDs_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPs_all:  INPs_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTs_all: SV_Txbitrate_interface1,
		CNumber:   multiclients.S2.Count(),
	}

	if sch.current_Prob == 0 {
		sch.current_Prob = 0.5
		sch.getActionAsyncSACRx(baseURL+"/get_action", stateData, sch.model_id)
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.current_Prob == 1 {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	// elapsed := time.Since(sch.time_Get_Action)
	elapsed := uint16(stateData.CNumber) * 3
	if elapsed < 9 {
		elapsed = 9
	}
	if sch.count_Action > elapsed {
		// rewardPayload := sch.list_Reward_SACMulti[sch.current_Prob]
		// rewardPayload.NextState = stateData
		// reward := (goodPut[1] + goodPut[3]) / (SV_Txbitrate_interface0 + SV_Txbitrate_interface1)
		max_throughput := 5.0
		min_signal := -90.0
		max_signal := -30.0
		throughput_0 := 0.0
		throughput_1 := 0.0

		if SV_Txbitrate_interface0 <= min_signal {
			throughput_0 = 0
		} else if SV_Txbitrate_interface0 >= min_signal {
			throughput_0 = max_throughput
		} else {
			throughput_0 = max_throughput * (SV_Txbitrate_interface0 - min_signal) / (max_signal - min_signal)
		}
		if SV_Txbitrate_interface1 <= min_signal {
			throughput_1 = 0
		} else if SV_Txbitrate_interface1 >= min_signal {
			throughput_1 = max_throughput
		} else {
			throughput_1 = max_throughput * (SV_Txbitrate_interface1 - min_signal) / (max_signal - min_signal)
		}
		reward := (goodPut[1] + goodPut[3]) / (throughput_0 + throughput_1)
		// fmt.Println("Reward: ", reward, throughput_0, throughput_1, SV_Txbitrate_interface0, SV_Txbitrate_interface1)
		tmp_Payload := RewardPayloadSACMulti{
			State:     sch.current_State_SACMulti,
			NextState: stateData,
			Action:    sch.current_Prob,
			Reward:    reward,
			Done:      false,
			ModelID:   sch.model_id,
		}

		sch.current_State_SACMulti = stateData
		sch.getActionAsyncSACRx(baseURL+"/get_action", stateData, sch.model_id)

		sch.count_Action = 0
		sch.count_Reward = 0
		sch.current_Reward = 0

		// fmt.Println("PayLoad: ", sch.current_Prob, reward, SV_Txbitrate_interface0, SV_Txbitrate_interface1)
		updateRewardSACMulti(baseURL+"/update_reward", tmp_Payload)
	} else {
		sch.count_Action += 1
	}

	action := 0
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	if sch.current_Prob > r.Float64() {
		action = 1
	}
	if s.paths[availablePaths[action]].SendingAllowed() {
		// sch.current_State_DQN = stateData
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) getActionAsyncSACRx(url string, state StateSACMulti, model_id uint64) {
	go func() {
		jsonPayload, err := json.Marshal(map[string]interface{}{
			"state":    state,
			"model_id": model_id,
		})

		// fmt.Println("GetAction: ", model_id)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
		defer resp.Body.Close()

		var response ActionProbabilityResponse
		err = json.NewDecoder(resp.Body).Decode(&response)
		if err != nil {
			fmt.Println("Error decoding response:", err)
			return
		}

		if response.Error != "" {
			fmt.Println("Server returned error:", response.Error)
			return
		}

		// fmt.Println("Received action probability:", response.Probability[0])
		sch.current_Prob = response.Probability[0]
		sch.count_Action = 0
		// rewardPayload := RewardPayloadSACMulti{
		// 	State:       state,
		// 	NextState:   state,
		// 	Action:      sch.current_Prob,
		// 	Reward:      0.0,
		// 	Done:        false,
		// 	ModelID:     sch.model_id,
		// 	CountReward: 0,
		// }
		// var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		// multiclients.List_Reward_DQN.Set(connID, rewardPayload)
	}()
}

func (sch *scheduler) selectPathSACMultiJoinCC(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
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

	var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
	if _, ok := multiclients.S2.Get(connID); ok {
		var tmp_value multiclients.StateMulti
		tmp_value.FRTT = lRTT[firstPath]
		tmp_value.SRTT = lRTT[secondPath]
		tmp_value.FCWND = CWND[firstPath]
		tmp_value.SCWND = CWND[secondPath]
		tmp_value.FInP = INP[firstPath]
		tmp_value.SInP = INP[secondPath]
		multiclients.S2.Set(connID, tmp_value)
	}

	var CWNDf_total, INPf_total, CWNDs_total, INPs_total protocol.ByteCount
	var SRTTf_total, SRTTs_total time.Duration
	CWNDf_total = 0
	INPf_total = 0
	CWNDs_total = 0
	INPs_total = 0
	SRTTf_total = 0
	SRTTs_total = 0

	CWNDf_mean := 0.0
	INPf_mean := 0.0
	CWNDs_mean := 0.0
	INPs_mean := 0.0
	SRTTf_mean := 0.0
	SRTTs_mean := 0.0

	if multiclients.S2.Count() > 1 {
		ItemsList := multiclients.S2.Items()
		for _, element := range ItemsList {
			if foo, ok := element.(multiclients.StateMulti); ok {
				CWNDf_total += foo.FCWND
				INPf_total += foo.FInP
				CWNDs_total += foo.SCWND
				INPs_total += foo.FInP
				SRTTf_total += foo.FRTT
				SRTTs_total += foo.SRTT
			}
		}
		// CWNDf_mean = (float64(CWNDf_total) - float64(CWND[firstPath])) / float64(multiclients.S2.Count()-1)
		// INPf_mean = (float64(INPf_total) - float64(INP[firstPath])) / float64(multiclients.S2.Count()-1)
		// CWNDs_mean = (float64(CWNDs_total) - float64(CWND[secondPath])) / float64(multiclients.S2.Count()-1)
		// INPs_mean = (float64(INPs_total) - float64(INP[secondPath])) / float64(multiclients.S2.Count()-1)
		// SRTTf_mean = (NormalizeTimes(SRTTf_total) - NormalizeTimes(sRTT[firstPath])) / float64(multiclients.S2.Count()-1)
		// SRTTs_mean = (NormalizeTimes(SRTTs_total) - NormalizeTimes(sRTT[secondPath])) / float64(multiclients.S2.Count()-1)

		CWNDf_mean = float64(CWNDf_total)
		INPf_mean = float64(INPf_total)
		CWNDs_mean = float64(CWNDs_total)
		INPs_mean = float64(INPs_total)
		SRTTf_mean = NormalizeTimes(SRTTf_total)
		SRTTs_mean = NormalizeTimes(SRTTs_total)
	}

	stateData := StateSACMulti{
		CWNDf: float64(CWND[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPf:  float64(INP[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTf: NormalizeTimes(sRTT[firstPath]) / 100.0 / 1000000.0,
		CWNDs: float64(CWND[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPs:  float64(INP[secondPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTs: NormalizeTimes(sRTT[secondPath]) / 100.0 / 1000000.0,

		CWNDf_all: CWNDf_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPf_all:  INPf_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTf_all: SRTTf_mean / 100.0 / 1000000.0,
		CWNDs_all: CWNDs_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		INPs_all:  INPs_mean / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTTs_all: SRTTs_mean / 100.0 / 1000000.0,

		CNumber: multiclients.S2.Count(),
	}

	if sch.current_Prob_JoinCC.Action1 == 0 {
		sch.current_Prob_JoinCC.Action1 = 1
		sch.current_Prob_JoinCC.Action2 = 2
		sch.current_Prob_JoinCC.Action3 = 2
		// rewardPayload := sch.list_Reward_DQN[sch.current_Prob]
		// rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		// fmt.Println("State: ", stateData)

		sch.getActionAsyncMultiJoinCC(s, baseURL+"/get_action", stateData, sch.model_id)
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.current_Prob_JoinCC.Action1 == 1 {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	if sch.count_Action > 10 {
		rewardPayload := sch.list_Reward_SACMulti[sch.current_Prob]
		rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
			rewardPayload, ok := tmp_Reload.(RewardPayloadSACMulti)
			if !ok {
				fmt.Println("Type assertion failed")
				return nil
			}
			rewardPayload.NextState = stateData
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
			// fmt.Println("State: ", stateData, rewardPayload)
		}

		sch.current_State_SACMulti = stateData
		sch.getActionAsyncMultiJoinCC(s, baseURL+"/get_action", stateData, sch.model_id)

		sch.count_Action = 0
		sch.count_Reward = 0
		sch.current_Reward = 0
		sch.time_Get_Action = time.Now()
	} else {
		sch.count_Action += 1
	}

	action := 0
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	if sch.current_Prob_JoinCC.Action1 > r.Float64() {
		action = 1
	}
	if s.paths[availablePaths[action]].SendingAllowed() {
		// sch.current_State_DQN = stateData
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) getActionAsyncMultiJoinCC(s *session, url string, state StateSACMulti, model_id uint64) {
	go func() {
		jsonPayload, err := json.Marshal(map[string]interface{}{
			"state":    state,
			"model_id": model_id,
		})

		// fmt.Println("GetAction: ", model_id)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
		defer resp.Body.Close()

		var response ActionProbabilityResponse
		err = json.NewDecoder(resp.Body).Decode(&response)
		if err != nil {
			fmt.Println("Error decoding response:", err)
			return
		}

		if response.Error != "" {
			fmt.Println("Server returned error:", response.Error)
			return
		}

		fmt.Println("Received action probability:", response.Probability)
		sch.current_Prob_JoinCC.Action1 = response.Probability[0]
		sch.current_Prob_JoinCC.Action2 = response.Probability[1]
		sch.current_Prob_JoinCC.Action3 = response.Probability[2]
		rewardPayload := RewardPayloadSACMultiJoinCC{
			State:       state,
			NextState:   state,
			Action:      sch.current_Prob_JoinCC,
			Reward:      0.0,
			Done:        false,
			ModelID:     sch.model_id,
			CountReward: 0,
		}
		var connID string = strconv.FormatFloat(sch.current_Prob_JoinCC.Action1, 'f', 5, 64) + strconv.FormatFloat(sch.current_Prob_JoinCC.Action2, 'f', 5, 64) + strconv.FormatFloat(sch.current_Prob_JoinCC.Action3, 'f', 5, 64)
		multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		for pathID, pth := range s.paths {
			if pathID == 1 {
				pth.sentPacketHandler.SignalChangeCWWNDSAC(response.Probability[1])
			} else if pathID == 3 {
				pth.sentPacketHandler.SignalChangeCWWNDSAC(response.Probability[2])
			}
		}
	}()
}

func (sch *scheduler) selectPathSACMulti(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2 {
		for pathID, path := range s.paths {
			if pathID != protocol.InitialPathID {
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
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

	var connID string = strconv.FormatUint(uint64(s.connectionID), 10)
	if _, ok := multiclients.S2.Get(connID); ok {
		var tmp_value multiclients.StateMulti
		tmp_value.FRTT = lRTT[firstPath]
		tmp_value.SRTT = lRTT[secondPath]
		tmp_value.FCWND = CWND[firstPath]
		tmp_value.SCWND = CWND[secondPath]
		tmp_value.FInP = INP[firstPath]
		tmp_value.SInP = INP[secondPath]
		multiclients.S2.Set(connID, tmp_value)
	}

	var CWNDf_total, INPf_total, CWNDs_total, INPs_total protocol.ByteCount
	var SRTTf_total, SRTTs_total time.Duration
	CWNDf_total = 0
	INPf_total = 0
	CWNDs_total = 0
	INPs_total = 0
	SRTTf_total = 0
	SRTTs_total = 0

	CWNDf_mean := 0.0
	INPf_mean := 0.0
	CWNDs_mean := 0.0
	INPs_mean := 0.0
	SRTTf_mean := 0.0
	SRTTs_mean := 0.0

	if multiclients.S2.Count() > 1 {
		ItemsList := multiclients.S2.Items()
		for _, element := range ItemsList {
			if foo, ok := element.(multiclients.StateMulti); ok {
				CWNDf_total += foo.FCWND
				INPf_total += foo.FInP
				CWNDs_total += foo.SCWND
				INPs_total += foo.FInP
				SRTTf_total += foo.FRTT
				SRTTs_total += foo.SRTT
			}
		}
		// CWNDf_mean = (float64(CWNDf_total) - float64(CWND[firstPath])) / float64(multiclients.S2.Count()-1)
		// INPf_mean = (float64(INPf_total) - float64(INP[firstPath])) / float64(multiclients.S2.Count()-1)
		// CWNDs_mean = (float64(CWNDs_total) - float64(CWND[secondPath])) / float64(multiclients.S2.Count()-1)
		// INPs_mean = (float64(INPs_total) - float64(INP[secondPath])) / float64(multiclients.S2.Count()-1)
		// SRTTf_mean = (NormalizeTimes(SRTTf_total) - NormalizeTimes(sRTT[firstPath])) / float64(multiclients.S2.Count()-1)
		// SRTTs_mean = (NormalizeTimes(SRTTs_total) - NormalizeTimes(sRTT[secondPath])) / float64(multiclients.S2.Count()-1)

		CWNDf_mean = float64(CWNDf_total)
		INPf_mean = float64(INPf_total)
		CWNDs_mean = float64(CWNDs_total)
		INPs_mean = float64(INPs_total)
		SRTTf_mean = NormalizeTimes(SRTTf_total) / float64(multiclients.S2.Count())
		SRTTs_mean = NormalizeTimes(SRTTs_total) / float64(multiclients.S2.Count())
	}

	stateData := StateSACMulti{
		CWNDf: float64(CWND[firstPath]) / float64(protocol.DefaultTCPMSS),
		INPf:  float64(INP[firstPath]) / float64(protocol.DefaultTCPMSS),
		SRTTf: NormalizeTimes(sRTT[firstPath]) / 10,
		CWNDs: float64(CWND[secondPath]) / float64(protocol.DefaultTCPMSS),
		INPs:  float64(INP[secondPath]) / float64(protocol.DefaultTCPMSS),
		SRTTs: NormalizeTimes(sRTT[secondPath]) / 10,

		CWNDf_all: CWNDf_mean / float64(protocol.DefaultTCPMSS),
		INPf_all:  INPf_mean / float64(protocol.DefaultTCPMSS),
		SRTTf_all: SRTTf_mean / 10,
		CWNDs_all: CWNDs_mean / float64(protocol.DefaultTCPMSS),
		INPs_all:  INPs_mean / float64(protocol.DefaultTCPMSS),
		SRTTs_all: SRTTs_mean / 10,

		CNumber: multiclients.S2.Count(),
	}
	// fmt.Println("Path: ", firstPath, secondPath)
	// fmt.Println("State: ", stateData)

	if sch.current_Prob == 0 {
		sch.current_Prob = 1
		// rewardPayload := sch.list_Reward_DQN[sch.current_Prob]
		// rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		// fmt.Println("State: ", stateData)

		sch.getActionAsyncMulti(baseURL+"/get_action", stateData, sch.model_id)
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	} else if sch.current_Prob == 1 {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}

	// time_interval := (sRTT[firstPath] + sRTT[secondPath]) / 10
	// elapsed := time.Since(sch.time_Get_Action)

	// elapsed := uint16(stateData.CNumber) * 3
	// if elapsed < 9 {
	// 	elapsed = 9
	// }else if elapsed >
	// fmt.Println("Time interval: ", time_interval, elapsed)
	if sch.count_Action > 9 {
		// fmt.Println("PayLoad: ", rewardPayload)
		rewardPayload := sch.list_Reward_SACMulti[sch.current_Prob]
		rewardPayload.NextState = stateData
		// sch.list_Reward_DQN[sch.current_Prob] = rewardPayload
		var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
			rewardPayload, ok := tmp_Reload.(RewardPayloadSACMulti)
			if !ok {
				fmt.Println("Type assertion failed")
				return nil
			}
			rewardPayload.NextState = stateData
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
			// fmt.Println("State: ", stateData, rewardPayload)
		}

		sch.current_State_SACMulti = stateData
		sch.getActionAsyncMulti(baseURL+"/get_action", stateData, sch.model_id)

		sch.count_Action = 0
		sch.count_Reward = 0
		sch.current_Reward = 0
		sch.time_Get_Action = time.Now()
	} else {
		sch.count_Action += 1
	}

	action := 0
	src := rand.NewSource(time.Now().UnixNano())
	r := rand.New(src)

	if sch.current_Prob > r.Float64() {
		action = 1
	}
	if s.paths[availablePaths[action]].SendingAllowed() {
		// sch.current_State_DQN = stateData
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) getActionAsyncMulti(url string, state StateSACMulti, model_id uint64) {
	go func() {
		jsonPayload, err := json.Marshal(map[string]interface{}{
			"state":    state,
			"model_id": model_id,
		})

		// fmt.Println("GetAction: ", model_id)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
		defer resp.Body.Close()

		var response ActionProbabilityResponse
		err = json.NewDecoder(resp.Body).Decode(&response)
		if err != nil {
			fmt.Println("Error decoding response:", err)
			return
		}

		if response.Error != "" {
			fmt.Println("Server returned error:", response.Error)
			return
		}

		// fmt.Println("Received action probability:", response.Probability[0])
		sch.current_Prob = response.Probability[0]
		rewardPayload := RewardPayloadSACMulti{
			State:       state,
			NextState:   state,
			Action:      sch.current_Prob,
			Reward:      0.0,
			Done:        false,
			ModelID:     sch.model_id,
			CountReward: 0,
		}
		var connID string = strconv.FormatFloat(sch.current_Prob, 'E', -1, 64)
		multiclients.List_Reward_DQN.Set(connID, rewardPayload)
		sch.count_Action = 0
	}()
}

func (sch *scheduler) selectPathSACcc(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	firstPath := protocol.PathID(255)
	sRTT := make(map[protocol.PathID]time.Duration)
	lRTT := make(map[protocol.PathID]time.Duration)
	CWND := make(map[protocol.PathID]protocol.ByteCount)
	INP := make(map[protocol.PathID]protocol.ByteCount)

	// packetNumber := make(map[protocol.PathID]uint64)
	// retransNumber := make(map[protocol.PathID]uint64)

	//Paths
	var availablePaths []protocol.PathID
	for pathID, pth := range s.paths {
		CWND[pathID] = pth.sentPacketHandler.GetCongestionWindow()
		INP[pathID] = pth.sentPacketHandler.GetBytesInFlight()
		sRTT[pathID] = pth.rttStats.SmoothedRTT()
		lRTT[pathID] = pth.rttStats.LatestRTT()
		// packetNumber[pathID], retransNumber[pathID], _ = pth.sentPacketHandler.GetStatistics()

		if pathID != protocol.InitialPathID {
			availablePaths = append(availablePaths, pathID)
			if firstPath == protocol.PathID(255) {
				firstPath = pathID
			}
		}

	}
	var rttvar float64
	if sRTT[firstPath] != 0 {
		rttvar = math.Abs(float64(lRTT[firstPath]-sRTT[firstPath])) / float64(sRTT[firstPath])
	} else {
		rttvar = 0
	}

	stateData := StateSACcc{
		CWND: float64(CWND[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		INP:  float64(INP[firstPath]) / float64(protocol.DefaultMaxCongestionWindow*1024),
		SRTT: NormalizeTimes(sRTT[firstPath]) / 1000,
		VRTT: rttvar,
	}

	if sch.count_Action == 20 {
		// fmt.Println("Change Action")

		rewardPayload := sch.list_Reward_SACcc[sch.current_Prob_SACcc.Action1]
		rewardPayload.NextState = stateData
		// sch.List_Reward_DQN[sch.current_Prob] = rewardPayload
		// var connID string = strconv.FormatFloat(sch.current_Prob_SACcc.Action1, 'E', -1, 64)
		connID := fmt.Sprintf("%.6f", sch.current_Prob_SACcc.Action1)
		if tmp_Reload, ok := multiclients.List_Reward_DQN.Get(connID); ok {
			rewardPayload, ok := tmp_Reload.(RewardPayloadSACcc)
			if !ok {
				fmt.Println("Type assertion failed")
				return nil
			}
			rewardPayload.NextState = stateData
			multiclients.List_Reward_DQN.Set(connID, rewardPayload)
			//fmt.Println("State: ", stateData, rewardPayload)
		}
		sch.current_State_SACcc = stateData
		done := make(chan bool)
		// sch.getActionAsyncSACcc(s, baseURL+"/get_action", stateData, sch.model_id)
		// go sch.getActionAsyncSACcc(s, baseURL+"/get_action", stateData, sch.model_id, done)
		go sch.getActionAsyncSACcc(s, "127.0.0.1:8081", stateData, sch.model_id, done)
		sch.count_Action = 0
		sch.count_Reward = 0
		sch.current_Reward = 0
		sch.time_Get_Action = time.Now()
	} else {
		
		sch.count_Action += 1
		// fmt.Println("Count Action" , sch.count_Action)
	}

	action := 0
	if s.paths[availablePaths[action]].SendingAllowed() {
		// sch.current_State_DQN = stateData
		return s.paths[availablePaths[action]]
	}

	if hasRetransmission && s.paths[protocol.InitialPathID].SendingAllowed() {
		return s.paths[protocol.InitialPathID]
	} else {
		return nil
	}

}

func (sch *scheduler) getActionAsyncSACcc(s *session, addr string, state StateSACcc, model_id uint64, done chan bool) {
	defer func() { done <- true }()

	go func() {
		conn, err := net.Dial("udp", addr)
		if err != nil {
			fmt.Println("Error connecting to socket server:", err)
			return
		}
		defer conn.Close()

		// Chuẩn bị message
		msg := map[string]interface{}{
			"command":  "get_action",
			"state":    state,
			"model_id": model_id, // Nếu Python server không dùng thì bỏ dòng này
		}

		jsonData, err := json.Marshal(msg)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		start := time.Now() // Bắt đầu đo thời gian trước khi gửi

		// Gửi JSON + newline để Python server có thể đọc từng message riêng biệt
		_, err = conn.Write(append(jsonData, '\n'))
		if err != nil {
			fmt.Println("Error writing to socket:", err)
			return
		}

		// Đọc response dòng JSON
		reader := bufio.NewReader(conn)
		responseLine, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading response:", err)
			return
		}

		elapsed := time.Since(start) // Tính thời gian trôi qua
		fmt.Printf("Time elapsed for request-response: %s\n", elapsed)

		var response ActionProbabilityResponse
		err = json.Unmarshal([]byte(responseLine), &response)
		if err != nil {
			fmt.Println("Error decoding response JSON:", err)
			return
		}

		if response.Error != "" {
			fmt.Println("Server returned error:", response.Error)
			return
		}

		if len(response.Probability) == 0 {
			fmt.Println("Empty probability array received")
			return
		}

		sch.current_Prob_SACcc.Action1 = response.Probability[0]

		rewardPayload := RewardPayloadSACcc{
			State:       state,
			NextState:   state,
			Action:      sch.current_Prob_SACcc,
			Reward:      0.0,
			Done:        false,
			ModelID:     sch.model_id,
			CountReward: 0,
		}
		connID := fmt.Sprintf("%.6f", sch.current_Prob_SACcc.Action1)
		multiclients.List_Reward_DQN.Set(connID, rewardPayload)

		// Áp dụng action lên đường dẫn
		for pathID, pth := range s.paths {
			if pathID == 1 {
				// utils.Infof("Change CWND of ConnectionID: %x", s.connectionID)
				pth.sentPacketHandler.SignalChangeCWWNDSAC(response.Probability[0])
			}
		}
	}()
}

// func (sch *scheduler) getActionAsyncSACcc(s *session, url string, state StateSACcc, model_id uint64, done chan bool) {
// 	defer func() { done <- true }()
// 	go func() {

// 		jsonPayload, err := json.Marshal(map[string]interface{}{
// 			"state":    state,
// 			"model_id": model_id,
// 		})

// 		// fmt.Println("GetAction: ", model_id)
// 		if err != nil {
// 			fmt.Println("Error encoding JSON:", err)
// 			return
// 		}

// 		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
// 		if err != nil {
// 			fmt.Println("Error sending POST request:", err)
// 			return
// 		}
// 		defer resp.Body.Close()

// 		var response ActionProbabilityResponse
// 		err = json.NewDecoder(resp.Body).Decode(&response)
// 		if err != nil {
// 			fmt.Println("Error decoding response:", err)
// 			return
// 		}

// 		if response.Error != "" {
// 			fmt.Println("Server returned error:", response.Error)
// 			return
// 		}

// 		// fmt.Println("Received action probability:", response.Probability)
// 		sch.current_Prob_SACcc.Action1 = response.Probability[0]

// 		rewardPayload := RewardPayloadSACcc{
// 			State:       state,
// 			NextState:   state,
// 			Action:      sch.current_Prob_SACcc,
// 			Reward:      0.0,
// 			Done:        false,
// 			ModelID:     sch.model_id,
// 			CountReward: 0,
// 		}
// 		// var connID string = strconv.FormatFloat(sch.current_Prob_SACcc.Action1, 'E', -1, 64)
// 		connID := fmt.Sprintf("%.6f", sch.current_Prob_SACcc.Action1)
// 		multiclients.List_Reward_DQN.Set(connID, rewardPayload)

// 		for pathID, pth := range s.paths {
// 			if pathID == 1 {
// 				// fmt.Println("Change CWND of Connection ID:%x", s.connectionID)
// 				utils.Infof("Change CWND of ConnectionID: %x", s.connectionID)
// 				pth.sentPacketHandler.SignalChangeCWWNDSAC(response.Probability[0])
// 			}
// 		}
// 	}()
// }
