from flask import Flask, request, jsonify
from model.sac.main import Environment as SACEnv
import matplotlib.pyplot as plt
import threading

app = Flask(__name__)

# Initialize the SAC environment
models = {}
models[3] = SACEnv()
models[4] = SACEnv()
models[5] = SACEnv()

training_request_count = 0

@app.route('/set_model', methods=['POST'])
def set_model():
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
        model.agent.train(batch_size=64, num_epochs=1, model_id=model_id)
        print(f"Training completed for model_id: {model_id}")
    except Exception as e:
        print(f"Error training model_id {model_id}: {e}")

@app.route('/flag_training', methods=['POST'])
def flag_training():
    global training_request_count
    try:
        # Increment the training request counter
        training_request_count += 1
        print(f"Flag training called: count={training_request_count}")  # Log the training request count
        if training_request_count >= 3:
            for model_id, model in models.items():
                print(f"Training model: model_id={model_id}")  # Log the model_id
                training_thread = threading.Thread(target=train_model, args=(model, model_id))
                training_thread.start()
            training_request_count = 0  # Reset the counter after training
            return jsonify({'status': 'Training started for all models'}), 200
        else:
            return jsonify({'status': 'Training flag received', 'count': training_request_count}), 200

    except Exception as e:
        print(f"Error in training: {e}")
        return jsonify({'error': str(e)}), 500

def get_action_thread(state, model_id, response):
    try:
        required_fields = ['CWNDf', 'INPf', 'SRTTf', 'CWNDs', 'INPs', 'SRTTs']
        for field in required_fields:
            if field not in state:
                response['error'] = f'Missing field in state data: {field}'
                return

        state = [state['CWNDf'], state['INPf'], state['SRTTf'], 
                 state['CWNDs'], state['INPs'], state['SRTTs']]

        if model_id not in models:
            response['error'] = 'Model ID not found'
            return

        # print(f"Get action: model_id={model_id}, state={state}")  # Log the model_id and state

        prob = models[model_id].agent.get_action_probability(state)
        response['probability'] = prob.tolist()

    except Exception as e:
        response['error'] = str(e)

@app.route('/get_action', methods=['POST'])
def get_action():
    try:
        state_json = request.json.get('state')
        model_id_str = request.json.get('model_id')

        if not state_json:
            return jsonify({'error': 'State data is missing'}), 400

        try:
            model_id = int(model_id_str)  # Convert model_id to integer
        except ValueError as e:
            return jsonify({'error': f'Invalid model_id: {e}'}), 400
        
        response = {}
        action_thread = threading.Thread(target=get_action_thread, args=(state_json, model_id, response))
        action_thread.start()
        action_thread.join()  # Ensure the thread completes before responding

        if 'error' in response:
            return jsonify({'error': response['error']}), 400

        return jsonify({'probability': response['probability']}), 200

    except Exception as e:
        print(f"Error in get_action: {e}")
        return jsonify({'error': str(e)}), 500

def update_reward_thread(data, response):
    try:
        state_json = data.get('state')
        next_state_json = data.get('next_state')
        model_id_str = data.get('model_id')

        if not model_id_str:
            response['error'] = 'model_id is missing'
            return

        try:
            model_id = int(model_id_str)  # Convert model_id to integer
        except ValueError as e:
            response['error'] = f'Invalid model_id: {e}'
            return

        if not state_json or not next_state_json:
            response['error'] = 'State or next_state data is missing'
            return
        
        state = [state_json['CWNDf'], state_json['INPf'], state_json['SRTTf'], 
                 state_json['CWNDs'], state_json['INPs'], state_json['SRTTs']]
        next_state = [next_state_json['CWNDf'], next_state_json['INPf'], next_state_json['SRTTf'], 
                      next_state_json['CWNDs'], next_state_json['INPs'], next_state_json['SRTTs']]
        
        action = data.get('action')
        reward = data.get('reward')
        done = data.get('done')

        if action is None or reward is None or done is None:
            response['error'] = 'Action, reward, or done data is missing'
            return

        if model_id not in models:
            response['error'] = 'Model ID not found'
            return

        # print(f"Update reward: model_id={model_id}, state={state}, next_state={next_state}, action={action}, reward={reward}, done={done}")  # Log the model_id and other details

        models[model_id].agent.replay_buffer.add(state, action, reward, next_state, done)
        response['status'] = 'Reward updated'
    except Exception as e:
        response['error'] = str(e)

@app.route('/update_reward', methods=['POST'])
def update_reward():
    try:
        data = request.json
        response = {}
        update_thread = threading.Thread(target=update_reward_thread, args=(data, response))
        update_thread.start()
        update_thread.join()  # Ensure the thread completes before responding

        if 'error' in response:
            return jsonify({'error': response['error']}), 400

        return jsonify({'status': response['status']}), 200
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
    app.run(debug=True, host='0.0.0.0', port=8080, threaded=True)
