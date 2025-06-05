from flask import Flask, jsonify, request, render_template
from flask_executor import Executor
from lib.utils import make_predictions
from model.deepql.main import Environment
import os.path

app = Flask(__name__) 
# app.config['EXECUTOR_MAX_WORKERS'] = 5
# app.config['EXECUTOR_TYPE'] = 'thread'
# executor = Executor(app)

@app.route('/') 
def root(): 
    return render_template("index.html")

# Initialize the environment and agent
env = Environment()
# if os.path.isfile("trained_model.pth"):   
env.agent.load_model("trained_model.pth")

# Define API endpoint for getting action based on state
@app.route('/get_action', methods=['POST'])
def get_action():
    state = request.json['state']
    action = env.agent.act(state)
    return jsonify({'action': action})

# Define API endpoint for updating the agent with experience
@app.route('/update_agent', methods=['POST'])
def update_agent():
    if request.method == 'POST':
        update_data = request.json
        state = update_data['state']
        action = update_data['action']
        reward = update_data['reward']
        next_state = update_data['next_state']
        done = update_data['done']
        env.agent.remember(state, action, reward, next_state, done)
    return '', 204  # Không trả về gì cả

# Define API endpoint for resetting the environment
@app.route('/reset_environment', methods=['GET'])
def reset_environment():
    env = Environment()
    return jsonify({'message': 'Environment reset successfully'})

# Route để nhận tín hiệu từ client
@app.route('/train_signal', methods=['POST'])
def train_signal():
    # Kiểm tra xem client đã gửi tín hiệu báo hiệu đã đến lúc train chưa
    if request.method == 'POST':
        data = request.json
        train_flag = data.get('train_flag')
        
        # Nếu nhận được tín hiệu báo hiệu đã đến lúc train
        if train_flag:
            # Thực hiện quá trình train mô hình
            env.agent.replay()
            env.agent.save_model("trained_model.pth")
            #env.agent.epsilon = env.agent.epsilon_initial  # Khởi tạo lại epsilon
            # Trả về phản hồi cho client
            return jsonify({'message': env.agent.epsilon}), 200
        
    # Nếu không nhận được tín hiệu hoặc không đúng định dạng
    return jsonify({'error': 'Invalid train signal'}), 400

if __name__ == "__main__": 
    host = "127.0.0.1"
    port_number = 8080 
    app.run(host, port_number)
    # app.run()
