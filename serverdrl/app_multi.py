from flask import Flask, request, jsonify
from flask_executor import Executor
from model.sacmulti.main import Environment as SACEnv
import threading
import argparse

# Initialize argparse and parse command-line arguments
parser = argparse.ArgumentParser(description='Executes a test with defined scheduler')
parser.add_argument('--client', dest="clt", help="Client Number", required=True, type=int)
args = parser.parse_args()
number_count = args.clt
print(f"Number of clients: count={number_count}")

app = Flask(__name__)
executor = Executor(app)

# Initialize the SAC environment
sac_env = SACEnv()

# Set the default environment
current_env = sac_env

training_request_count = 0

def train_model():
    try:
        current_env.agent.train(batch_size=2048)
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
    try:
        # Increment the training request counter
        training_request_count += 1
        print(f"Flag training called: count={training_request_count}")  # Log the training request count
        if training_request_count >= number_count*2:
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

        required_fields = ['CWNDf', 'INPf', 'SRTTf', 'CWNDs', 'INPs', 'SRTTs', 'CWNDf_all', 'INPf_all', 'SRTTf_all', 'CWNDs_all', 'INPs_all', 'SRTTs_all', 'CNumber']
        for field in required_fields:
            if field not in state_json:
                return jsonify({'error': f'Missing field in state data: {field}'}), 400

        # Convert state data to list
        state = [state_json['CWNDf'], state_json['INPf'], state_json['SRTTf'], 
                 state_json['CWNDs'], state_json['INPs'], state_json['SRTTs'],
                 state_json['CWNDf_all'], state_json['INPf_all'], state_json['SRTTf_all'], 
                 state_json['CWNDs_all'], state_json['INPs_all'], state_json['SRTTs_all'], state_json['CNumber']]
        # print(f"Received state: {state}")

        # Get action probability from the SAC agent
        future = executor.submit(current_env.agent.get_action_probability, state)
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
        
        state = [state_json['CWNDf'], state_json['INPf'], state_json['SRTTf'], 
                 state_json['CWNDs'], state_json['INPs'], state_json['SRTTs'],
                 state_json['CWNDf_all'], state_json['INPf_all'], state_json['SRTTf_all'], 
                 state_json['CWNDs_all'], state_json['INPs_all'], state_json['SRTTs_all'], state_json['CNumber']]
        next_state = [next_state_json['CWNDf'], next_state_json['INPf'], next_state_json['SRTTf'], 
                      next_state_json['CWNDs'], next_state_json['INPs'], next_state_json['SRTTs'],
                      next_state_json['CWNDf_all'], next_state_json['INPf_all'], next_state_json['SRTTf_all'], 
                      next_state_json['CWNDs_all'], next_state_json['INPs_all'], next_state_json['SRTTs_all'], next_state_json['CNumber']]
        
        action = data['action']
        reward = data['reward']
        done = data['done']

        executor.submit(current_env.agent.replay_buffer.add, state, action, reward, next_state, done)
        #current_env.agent.add_reward(reward)
        #current_env.agent.train(batch_size=64)

        return jsonify({'status': 'Reward updated'}), 200
    except Exception as e:
        print(f"Error in update_reward: {e}")
        return jsonify({'error': str(e)}), 500

@app.route('/plot_training_history', methods=['GET'])
def plot_training_history():
    # Start the plotting in a new thread
    plotting_thread = threading.Thread(target=plot_history)
    plotting_thread.start()
    return jsonify({'status': 'Plotting started'}), 200

@app.route('/')
def index():
    return "Welcome to the Path Scheduler Training Server!"

@app.route('/status', methods=['GET'])
def status():
    return jsonify({'status': 'Server is running'})

if __name__ == '__main__':
    app.run(debug=True, host='0.0.0.0', port=8080, threaded=True)
