#!/bin/bash

# Khai báo các biến IP và giao diện mạng
IP1="192.168.8.182"
IP2="10.40.28.137"
GW1="192.168.8.1"
GW2="10.40.28.1"
NET1="192.168.8.0/24"
NET2="10.40.28.0/24"
DEV1="eth0"
DEV2="eth1"

# Xóa các quy tắc và tuyến cũ (nếu có)
ip rule del from $IP1 table 1 2>/dev/null || true
ip rule del from $IP2 table 2 2>/dev/null || true

ip route flush table 1 2>/dev/null
ip route flush table 2 2>/dev/null

# Thiết lập các quy tắc định tuyến mới
ip rule add from $IP1 table 1
ip rule add from $IP2 table 2

# Cấu hình bảng định tuyến thứ nhất (bảng 1)
ip route add $NET1 dev $DEV1 scope link table 1
ip route add default via $GW1 dev $DEV1 table 1

# Cấu hình bảng định tuyến thứ hai (bảng 2)
ip route add $NET2 dev $DEV2 scope link table 2
ip route add default via $GW2 dev $DEV2 table 2

# Tuyến mặc định cho lưu lượng Internet thông thường
ip route add default scope global nexthop via $GW1 dev $DEV1

echo "Cấu hình định tuyến đã được thiết lập thành công với các biến IP."
