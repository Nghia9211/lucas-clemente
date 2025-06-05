#!/bin/bash

# Chuyển đến thư mục và xây dựng client
cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/quic-go
go build
go install ./...
cd /home/"$(whoami)"/runMPQUIC/
pwd
cp /home/"$(whoami)"/go/bin/client_benchmarker ./clientMPQUIC
sudo rm ./logs/*

# Tạo lệnh cho các port và clt tương ứng
CLIENT_CMD="./clientMPQUIC -n 1 -t 0 -m -clt {clt}"
CLIENT_FIL="https://171.244.134.9:%d/files/%s-3"
LOG_FILE="./logs/client_clt_%d_port_%d.log"  # Log lưu theo clt, port

# Biến fil chứa tên file bạn muốn sử dụng
fil="2MB"

# Tạo mảng các port tương ứng
ports=(6121 6122 6123 6124)

# Hàm để chạy khối lệnh chính
run_task() {
  # Lặp lại 100 lần
  for iter in {1..100}; do
    echo "Iteration: $iter"

    # Lặp qua các port và thực thi lệnh tương ứng
    for idx in "${!ports[@]}"; do
      port=${ports[$idx]}
      clt=$((idx + 1))

      # Thay thế các giá trị trong lệnh
      client_cmd=$(echo $CLIENT_CMD | sed "s/{clt}/$clt/g")
      client_fil=$(printf "$CLIENT_FIL" "$port" "$fil")
      log_file=$(printf "$LOG_FILE" "$clt" "$port")

      # Kết hợp thành lệnh đầy đủ, với log lưu riêng
      final_cmd="$client_cmd $client_fil >> $log_file 2>&1"

      # In ra lệnh để kiểm tra
      echo "Lệnh cho clt $clt, port $port, lần lặp $iter: $final_cmd"

      # Thực thi lệnh
      eval $final_cmd
    done
  done

  # Di chuyển toàn bộ dữ liệu trong logs sang thư mục mới với tên định dạng thời gian
  cur_time=$(date +"%Y-%m-%d_%H-%M")
  mkdir -p "./archive_logs/$cur_time"
  mv ./logs/* "./archive_logs/$cur_time/"
  echo "Logs đã được chuyển vào ./archive_logs/$cur_time"
}

# Vòng lặp vô hạn để kiểm tra và chạy khối lệnh vào các khung giờ 0h, 3h, 6h, ..., 21h
while true; do
  current_hour=$(date +"%H" | sed 's/^0//')
  current_minute=$(date +"%M")

  if (( current_hour % 3 == 0 )) && [[ "$current_minute" == "00" ]]; then
      echo "Running task at $(date)"
      run_task
      sleep 60  # Đợi một phút để tránh chạy lại trong cùng phút
  fi

  sleep 10  # Chờ 10 giây trước khi kiểm tra lại
done
