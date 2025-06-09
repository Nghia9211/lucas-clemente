import socket
import json
from model.saccc.main import Environment as SACEnv
import numpy as np

HOST = '0.0.0.0'
PORT = 8081

env = SACEnv()

def dict_to_array(d):
    if isinstance(d, dict):
        return np.array(list(d.values()), dtype=np.float32)
    return np.array(d, dtype=np.float32)

def start_udp_server():
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.bind((HOST, PORT))
        print(f"UDP Socket Server listening on {HOST}:{PORT}")

        while True:
            try:
                data, client_addr = sock.recvfrom(8192)  # nhận dữ liệu + địa chỉ người gửi
                message_line = data.decode().strip()

                if not message_line:
                    continue

                try:
                    message = json.loads(message_line)
                except json.JSONDecodeError:
                    print(f"Invalid JSON received from {client_addr}: {message_line}")
                    continue

                print(f"Received message from {client_addr}: {message}")

                command = message.get("command")
                if command == "get_action":
                    state = message.get("state")
                    action = env.agent.get_action(state)
                    response = {"probability": action.tolist()}

                elif command == "update_reward":
                    raw_state = message["state"]
                    raw_action = message["action"]
                    reward = message["reward"]
                    raw_next_state = message["next_state"]
                    done = message["done"]

                    state = dict_to_array(raw_state)
                    action = dict_to_array(raw_action)
                    next_state = dict_to_array(raw_next_state)

                    env.agent.replay_buffer.add(state, action, reward, next_state, done)
                    response = {"status": "reward updated"}

                elif command == "flag_training":
                    env.agent.train(batch_size=2000)
                    response = {"status": "training started"}

                else:
                    response = {"error": "Invalid command"}

                response_bytes = (json.dumps(response) + "\n").encode()
                sock.sendto(response_bytes, client_addr)  # Gửi lại response tới đúng client

            except Exception as e:
                print(f"Error processing UDP packet: {e}")

if __name__ == "__main__":
    start_udp_server()
