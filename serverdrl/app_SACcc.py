from flask import Flask, request, jsonify
from flask_executor import Executor
from model.saccc.main import Environment as SACEnv


import os
import csv

log_file = "training_log.csv"



app = Flask(__name__)
executor = Executor(app)

# Initialize the SAC environment
sac_env = SACEnv()

# Set the default environment
current_env = sac_env
number_count = 1
training_request_count = 0

def train_model():
    try:
        current_env.agent.train(batch_size=2000)
    except Exception as e:
        print(f"Error in training: {e}")

def plot_history():
    try:
        current_env.agent.plot_training_history()
    except Exception as e:
        print(f"Error in plot_training_history: {e}")

@app.route('/set_model', methods=['POST'])
def set_model():
    global current_env
    model_type = request.json.get('model_type', 'sac')
    
    if model_type == 'sac':
        current_env = sac_env
    else:
        return jsonify({'error': 'Invalid model type'}), 400
    
    return jsonify({'status': f'Model set to {model_type}'}), 200



@app.route('/flag_training', methods=['POST'])
def flag_training():
    global training_request_count
    global number_count
    print("==> TRAINING triggered")
    try:
        # Increment the training request counter
        training_request_count += 1
        print(f"Flag training called: count={training_request_count}")  # Log the training request count
        if training_request_count >= number_count:
            print("Train")
            executor.submit(train_model)
            training_request_count = 0  # Reset the counter after training
            return jsonify({'status': 'Training started for all models'}), 200
        else:
            return jsonify({'status': 'Training flag received', 'count': training_request_count}), 200

    except Exception as e:
        print(f"Error in training: {e}")
        return jsonify({'error': str(e)}), 500

@app.route('/get_action', methods=['POST'])
def get_action():
    try:
        # Retrieve and validate the state from the request
        state_json = request.json.get('state')
        if not state_json:
            return jsonify({'error': 'State data is missing'}), 400

        required_fields = ['CWND','INP','SRTT','VRTT']
        for field in required_fields:
            if field not in state_json:
                return jsonify({'error': f'Missing field in state data: {field}'}), 400

        # Convert state data to list
        state = [state_json['CWND'], state_json['INP'], state_json['SRTT'], state_json['VRTT']]
        # print(f"Received state: {state}")

        # Get action probability from the SAC agent
        future = executor.submit(current_env.agent.get_action, state)
        prob = future.result()
        # print(f"Action probability: {prob}")

        # Return the action probability as a response
        return jsonify({'probability': prob.tolist()}), 200

    except Exception as e:
        # Log and return any errors that occur
        print(f"Error in get_action: {e}")
        return jsonify({'error': str(e)}), 500


@app.route('/update_reward', methods=['POST'])
def update_reward():
    try:
        data = request.json
        state_json = data['state']
        next_state_json = data['next_state']
        action_json = data['action']

        
        state = [state_json['CWND'], state_json['INP'], state_json['SRTT'], state_json['VRTT']]
        # print("State : ", state)
        next_state = [next_state_json['CWND'], next_state_json['INP'], next_state_json['SRTT'], next_state_json['VRTT']]
        # print("NextState : ", next_state)
        action = [action_json['action_1']]
        reward = data['reward']
        done = data['done']

        executor.submit(current_env.agent.replay_buffer.add, state, action, reward, next_state, done)

        # Ghi vào file CSV
        with open(log_file, mode='a', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(state + action + [reward] + next_state + [done])


        return jsonify({'status': 'Reward updated'}), 200
    except Exception as e:
        print(f"Error in update_reward: {e}")
        return jsonify({'error': str(e)}), 500


@app.route('/')
def index():
    return "Welcome to the Path Scheduler Training Server!"

@app.route('/status', methods=['GET'])
def status():
    return jsonify({'status': 'Server is running'})

if __name__ == '__main__':

    # Tạo file nếu chưa tồn tại
    if not os.path.exists(log_file):
        with open(log_file, mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow([
                'CWND', 'INP', 'SRTT', 'VRTT',         # state
                'Action', 'Reward',                   # action, reward
                'Next_CWND', 'Next_INP', 'Next_SRTT', 'Next_VRTT',  # next_state
                'Done'                                # done
            ])

    app.run(debug=True, host='0.0.0.0', port=8081, threaded=True)
