import re
# Đọc nội dung log từ file
with open("logs/server.logs", "r") as file:
    log_data = file.read()

# Regex để tìm tất cả thời gian với đơn vị (ms hoặc us)
matches = re.findall(r"Time elapsed for request-response: ([\d.]+)(ms|us)", log_data)

# Chuyển tất cả về đơn vị ms
elapsed_times_ms = []
for value, unit in matches:
    value = float(value)
    if unit == "us":
        value /= 1000  # đổi microseconds → milliseconds
    elapsed_times_ms.append(value)

# Tính toán trung bình
if elapsed_times_ms:
    avg_ms = sum(elapsed_times_ms) / len(elapsed_times_ms)
    print(f"Số mẫu: {len(elapsed_times_ms)}")
    print(f"Trung bình Time elapsed: {avg_ms:.6f} ms")
else:
    print("Không tìm thấy dữ liệu thời gian.")
