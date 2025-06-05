package main

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	quic "github.com/lucas-clemente/quic-go"

	"github.com/lucas-clemente/quic-go/h2quic"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

func sendTrainSignal(wg *sync.WaitGroup) {
	defer wg.Done()
	data := map[string]interface{}{
		"train_flag": true,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return
	}

	_, err = http.Post("http://10.0.0.20:8081/flag_training", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error sending POST request:", err)
	}
}

func sendTrainSignal2() {
	go func() {
		data := map[string]interface{}{
			"train_flag": true,
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		_, err = http.Post("http://10.0.0.20:8081/flag_training", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			return
		}
	}()
}

func main() {
	verbose := flag.Bool("v", false, "verbose")
	sleeptime := flag.Int("t", 0, "sleep time for request in second")
	num := flag.Int("n", 1, "number of request")
	clt := flag.Int("clt", 1, "number of client")
	multipath := flag.Bool("m", false, "multipath")
	output := flag.String("o", "", "logging output")
	cache := flag.Bool("c", false, "cache handshake information")
	bulk := flag.Bool("b", false, "bulkfile for throughput")

	flag.Parse()
	urls := flag.Args()

	filePath := "./logs/result" + fmt.Sprint(*clt) + ".csv"
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	csvwriter := csv.NewWriter(f)

	if *verbose {
		utils.SetLogLevel(utils.LogLevelDebug)
	} else {
		utils.SetLogLevel(utils.LogLevelInfo)
	}

	if *output != "" {
		logfile, err := os.Create(*output)
		if err != nil {
			panic(err)
		}
		defer logfile.Close()
		log.SetOutput(logfile)
	}

	quicConfig := &quic.Config{
		CreatePaths:    *multipath,
		CacheHandshake: *cache,
	}

	// processInterfaces() (get txbitrate from client, then sent to server via ACK packet)

	hclient := &http.Client{
		Transport: &h2quic.RoundTripper{QuicConfig: quicConfig, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	if !*bulk {
		var wg sync.WaitGroup
		for i := 0; i < *num; i++ {
			wg.Add(len(urls))
			for _, addr := range urls {
				utils.Infof("GET %s", addr)
				go func(addr string) {
					defer wg.Done()

					start := time.Now()
					rsp, err := hclient.Get(addr)
					if err != nil {
						log.Println("Error getting response:", err)
						return
					}
					defer rsp.Body.Close()

					body := &bytes.Buffer{}
					_, err = io.Copy(body, rsp.Body)
					if err != nil {
						log.Println("Error copying response body:", err)
						utils.Infof("%f", float64(30000))
						csvwriter.Write([]string{fmt.Sprint(float64(30000))})
						csvwriter.Flush()
					} else {
						elapsed := time.Since(start)
						utils.Infof("%f", float64(elapsed.Nanoseconds())/1000000)
						csvwriter.Write([]string{fmt.Sprint(float64(elapsed.Nanoseconds()) / 1000000)})
						csvwriter.Flush() // Gọi Flush() để đảm bảo dữ liệu được ghi ra file
					}
				}(addr)
			}
			wg.Wait()
			if i > 1 {
				sendTrainSignal2()
			}
			// processInterfaces()
			// time.Sleep(time.Duration(*sleeptime) * time.Second)
			time.Sleep(time.Duration(*sleeptime) * time.Millisecond)
		}
		// Thêm waitgroup cho sendTrainSignal
		wg.Add(1)
		go sendTrainSignal(&wg)
		wg.Wait()
	} else {
		var wg sync.WaitGroup
		for i := 0; i < *num; i++ {
			wg.Add(len(urls))
			for _, addr := range urls {
				go func(addr string) {
					defer wg.Done()

					// Tạo ticker để ghi log mỗi giây
					ticker := time.NewTicker(1 * time.Second)
					defer ticker.Stop()

					// Bắt đầu tải dữ liệu
					start := time.Now()
					rsp, err := hclient.Get(addr)
					if err != nil {
						log.Println("Error getting response:", err)
						return
					}
					defer rsp.Body.Close()

					// Đọc từng chunk dữ liệu và tính toán lượng dữ liệu tải được
					var totalBytes int64 = 0
					buf := make([]byte, 1024*1024) // Đọc từng chunk 1MB
					done := make(chan struct{})

					go func() {
						for {
							select {
							case <-ticker.C:
								// Ghi ra lượng dữ liệu tải được mỗi giây
								elapsed := time.Since(start).Seconds()
								log.Printf("Đã tải được: %d bytes sau %f giây\n", totalBytes, elapsed)
								csvwriter.Write([]string{fmt.Sprintf("%d", totalBytes)})
								csvwriter.Flush()
								sendTrainSignal2()
							case <-done:
								return
							}
						}
					}()

					for {
						n, err := rsp.Body.Read(buf)
						if n > 0 {
							totalBytes += int64(n)
						}
						if err != nil {
							if err == io.EOF {
								break
							}
							log.Println("Error reading response body:", err)
							return
						}
					}
					done <- struct{}{} // Kết thúc việc ghi log
					elapsed := time.Since(start)
					log.Printf("Tổng số bytes đã tải: %d, thời gian: %f giây\n", totalBytes, elapsed.Seconds())
				}(addr)
			}
			wg.Wait()
			time.Sleep(time.Duration(*sleeptime) * time.Millisecond)
		}
	}
}

// Hàm để xử lý các giao diện mạng không dây
func processInterfaces() {
	// Lấy danh sách tất cả các giao diện mạng không dây
	interfaces, err := getWirelessInterfaces()
	if err != nil {
		fmt.Printf("Error getting wireless interfaces: %v\n", err)
		return
	}

	// Lặp qua tất cả các giao diện và lấy các thông số hiện có
	for _, iface := range interfaces {
		fmt.Printf("Interface: %s\n", iface)

		// txBitrate, err := getTxBitrate(iface)
		// if err != nil {
		// 	fmt.Printf("Error getting tx bitrate for interface %s: %v\n", iface, err)
		// } else {
		// 	txBitrateUint16 := toUint16(txBitrate)
		// 	fmt.Printf("TX Bitrate: %d\n", txBitrateUint16)
		// 	if strings.HasSuffix(iface, "wlan0") {
		// 		quic.Txbitrate_interface0 = txBitrateUint16
		// 	} else if strings.HasSuffix(iface, "wlan1") {
		// 		quic.Txbitrate_interface1 = txBitrateUint16
		// 	}
		// }

		signal, err := getSignal(iface)
		if err != nil {
			fmt.Printf("Error getting signal for interface %s: %v\n", iface, err)
		} else {
			fmt.Printf("Signal: %d dBm\n", signal)
			if strings.HasSuffix(iface, "wlan0") {
				quic.Txbitrate_interface0 = uint16(signal * (-1))
			} else if strings.HasSuffix(iface, "wlan1") {
				quic.Txbitrate_interface1 = uint16(signal * (-1))
			}
			// Bạn có thể lưu giá trị signal vào các biến trong package quic nếu cần thiết
		}
	}
}

// Hàm lấy danh sách tất cả các giao diện mạng không dây
func getWirelessInterfaces() ([]string, error) {
	cmd := exec.Command("iw", "dev")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	// Phân tích đầu ra để lấy tên các giao diện mạng không dây
	output := out.String()
	lines := strings.Split(output, "\n")
	var interfaces []string
	for _, line := range lines {
		if strings.Contains(line, "Interface") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				interfaces = append(interfaces, parts[1])
			}
		}
	}
	return interfaces, nil
}

// Hàm lấy giá trị txBitrate cho một giao diện mạng cụ thể
func getTxBitrate(iface string) (int, error) {
	cmd := exec.Command("iw", "dev", iface, "link")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return 0, err
	}

	// Phân tích đầu ra để lấy giá trị txBitrate
	output := out.String()
	txBitrate := parseTxBitrate(output)
	return txBitrate, nil
}

// Hàm phụ để phân tích tx bitrate
func parseTxBitrate(output string) int {
	txPattern := regexp.MustCompile(`tx bitrate:\s+([\d\.]+) MBit/s`)
	matches := txPattern.FindStringSubmatch(output)
	if len(matches) > 1 {
		txBitrate, _ := strconv.ParseFloat(matches[1], 64)
		return int(txBitrate)
	}
	return 0
}

// Chuyển đổi giá trị tx bitrate sang uint16 và nhân với 10
func toUint16(bitrate int) uint16 {
	return uint16(bitrate * 10)
}

// Hàm lấy giá trị signal cho một giao diện mạng cụ thể
func getSignal(iface string) (int, error) {
	cmd := exec.Command("iw", "dev", iface, "link")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return 0, err
	}

	// Phân tích đầu ra để lấy giá trị signal
	output := out.String()
	signal := parseSignal(output)
	return signal, nil
}

// Hàm phụ để phân tích giá trị signal
func parseSignal(output string) int {
	signalPattern := regexp.MustCompile(`signal:\s(-?\d+) dBm`)
	matches := signalPattern.FindStringSubmatch(output)
	if len(matches) > 1 {
		signal, _ := strconv.Atoi(matches[1])
		return signal
	}
	return 0 // hoặc một giá trị phù hợp khác khi không tìm thấy signal
}
