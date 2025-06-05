from flask import Flask, request, jsonify
from flask_executor import Executor
from model.sac.main import Environment as SACEnv
import argparse

app = Flask(__name__)
executor = Executor(app)

# Initialize argparse and parse command-line arguments
parser = argparse.ArgumentParser(description='Executes a test with defined scheduler')
parser.add_argument('--client', dest="clt", help="Client Number", required=True, type=int)
args = parser.parse_args()
number_count = args.clt
print(f"Number of clients: count={number_count}")
training_request_count = 0

models = {}

@app.route('/set_model', methods=['POST'])
def set_model():
    global models
    model_type = request.json.get('model_type', 'sac')
    model_id_str = request.json.get('model_id', '0')

    try:
        model_id = int(model_id_str)  # Convert model_id to integer
        if model_id < 0 or model_id > (1 << 64) - 1:
            raise ValueError("model_id out of range for uint64")
    except ValueError as e:
        return jsonify({'error': f'Invalid model_id: {e}'}), 400

    if model_id in models:
        return jsonify({'status': f'Model {model_id} already exists'}), 200

    if model_type == 'sac':
        models[model_id] = SACEnv()
        print(f"Set model: model_type={model_type}, model_id={model_id}")  # Log the model_id
    else:
        return jsonify({'error': 'Invalid model type'}), 400
    
    return jsonify({'status': f'Model set to {model_type} - {model_id}'}), 200

def train_model(model, model_id):
    try:
        model.agent.train(batch_size=2048, model_id=model_id)
        print(f"Training completed for model_id: {model_id}")
    except Exception as e:
        print(f"Error training model_id {model_id}: {e}")

@app.route('/flag_training', methods=['POST'])
def flag_training():
    global training_request_count
    global number_count
    global models
    try:
        # Increment the training request counter
        training_request_count += 1
        print(f"Flag training called: count={training_request_count}")  # Log the training request count
        if training_request_count >= number_count:
            for model_id, model in models.items():
                print(f"Training model: model_id={model_id}")  # Log the model_id
                executor.submit(train_model, model, model_id)

            training_request_count = 0  # Reset the counter after training
            return jsonify({'status': 'Training started for all models'}), 200
        else:
            return jsonify({'status': 'Training flag received', 'count': training_request_count}), 200

    except Exception as e:
        print(f"Error in training: {e}")
        return jsonify({'error': str(e)}), 500

@app.route('/get_action', methods=['POST'])
def get_action():
    global models
    try:
        state_json = request.json.get('state')
        model_id_str = request.json.get('model_id')

        if not state_json:
            return jsonify({'error': 'State data is missing'}), 400

        try:
            model_id = int(model_id_str)  # Convert model_id to integer
        except ValueError as e:
            return jsonify({'error': f'Invalid model_id: {e}'}), 400
        
        required_fields = ['CWNDf', 'INPf', 'SRTTf', 'CWNDs', 'INPs', 'SRTTs']
        for field in required_fields:
            if field not in state_json:
                return jsonify({'error': f'Missing field in state data: {field}'}), 400

        state = [state_json['CWNDf'], state_json['INPf'], state_json['SRTTf'], 
                 state_json['CWNDs'], state_json['INPs'], state_json['SRTTs']]

        if model_id not in models:
            return jsonify({'error': f'GetAction: Model ID {model_id} not found'}), 400

        future = executor.submit(models[model_id].agent.get_action_probability, state)
        prob = future.result()

        return jsonify({'probability': prob.tolist()}), 200

    except Exception as e:
        print(f"Error in get_action: {e}")
        return jsonify({'error': str(e)}), 500

@app.route('/update_reward', methods=['POST'])
def update_reward():
    global models
    try:
        data = request.json
        state_json = data['state']
        next_state_json = data['next_state']
        
        state = [state_json['CWNDf'], state_json['INPf'], state_json['SRTTf'], 
                 state_json['CWNDs'], state_json['INPs'], state_json['SRTTs']]
        next_state = [next_state_json['CWNDf'], next_state_json['INPf'], next_state_json['SRTTf'], 
                      next_state_json['CWNDs'], next_state_json['INPs'], next_state_json['SRTTs']]
        
        action = data['action']
        reward = data['reward']
        done = data['done']
        model_id = int(data['model_id'])

        executor.submit(models[model_id].agent.replay_buffer.add, state, action, reward, next_state, done)

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
    app.run(debug=True, host='0.0.0.0', port=8081, threaded=True)
