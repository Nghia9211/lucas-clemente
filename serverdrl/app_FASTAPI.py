from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from model.sac.main import Environment as SACEnv
from fastapi.concurrency import run_in_threadpool
import argparse
import os
import logging

# Cấu hình logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI()

# Initialize argparse and parse command-line arguments
number_count = int(os.getenv('CLIENT_NUM', 1))  # Giá trị mặc định là 1 nếu biến môi trường không có
print(f"Number of clients: count={number_count}")
training_request_count = 0

models = {}

# Pydantic model for state input validation
class State(BaseModel):
    CWNDf: float
    INPf: float
    SRTTf: float
    CWNDs: float
    INPs: float
    SRTTs: float

class ActionRequest(BaseModel):
    model_id: int
    state: State

    class Config:
        protected_namespaces = ()

class RewardPayload(BaseModel):
    state: State
    next_state: State
    action: float
    reward: float
    done: bool
    model_id: int
    count_reward: int

    class Config:
        protected_namespaces = ()
    
# Request body model for set_model
class SetModelRequest(BaseModel):
    model_type: str = 'sac'
    model_id: int

    class Config:
        protected_namespaces = ()

@app.post("/set_model")
async def set_model(request: SetModelRequest):
    global models
    model_id = request.model_id

    if model_id in models:
        return {"status": f"Model {model_id} already exists"}

    if request.model_type == 'sac':
        models[model_id] = SACEnv()
        print(f"Set model: model_type={request.model_type}, model_id={model_id}")
    else:
        raise HTTPException(status_code=400, detail="Invalid model type")
    
    return {"status": f"Model set to {request.model_type} - {model_id}"}

# Training function wrapped in thread pool
async def train_model(model, model_id):
    try:
        await run_in_threadpool(model.agent.train, batch_size=2048, model_id=model_id)
        print(f"Training completed for model_id: {model_id}")
    except Exception as e:
        print(f"Error training model_id {model_id}: {e}")

@app.post("/flag_training")
async def flag_training():
    global training_request_count
    global number_count
    global models

    # Increment the training request counter
    training_request_count += 1
    print(f"Flag training called: count={training_request_count}")

    if training_request_count >= number_count:
        for model_id, model in models.items():
            print(f"Training model: model_id={model_id}")
            await train_model(model, model_id)

        training_request_count = 0  # Reset the counter after training
        return {"status": "Training started for all models"}
    else:
        return {"status": "Training flag received", "count": training_request_count}

@app.post("/get_action")
async def get_action(request: ActionRequest):
    model_id = request.model_id
    state = request.state
    # logger.info(f"Received state: {state}")
    # logger.info(f"Received model_id: {model_id}")
    global models

    if model_id not in models:
        raise HTTPException(status_code=400, detail=f"GetAction: Model ID {model_id} not found")

    model = models[model_id]
    
    try:
        # Run action probability calculation in threadpool
        prob = await run_in_threadpool(model.agent.get_action_probability, [
            state.CWNDf, state.INPf, state.SRTTf,
            state.CWNDs, state.INPs, state.SRTTs
        ])
        return {"probability": prob.tolist()}
    except Exception as e:
        print(f"Error in get_action: {e}")
        raise HTTPException(status_code=500, detail="Error calculating action")

@app.post("/update_reward")
async def update_reward(payload: RewardPayload): 
    global models

    # Lấy các giá trị từ payload
    model_id = payload.model_id
    state = payload.state
    next_state = payload.next_state
    action = payload.action
    reward = payload.reward
    done = payload.done

    if model_id not in models:
        raise HTTPException(status_code=400, detail=f"Model ID {model_id} not found")

    model = models[model_id]

    try:
        await run_in_threadpool(model.agent.replay_buffer.add,
            [state.CWNDf, state.INPf, state.SRTTf, state.CWNDs, state.INPs, state.SRTTs],
            action, reward,
            [next_state.CWNDf, next_state.INPf, next_state.SRTTf, next_state.CWNDs, next_state.INPs, next_state.SRTTs],
            done
        )
        return {"status": "Reward updated"}
    except Exception as e:
        print(f"Error in update_reward: {e}")
        raise HTTPException(status_code=500, detail="Error updating reward")

@app.get("/")
def index():
    return "Welcome to the Path Scheduler Training Server!"

@app.get("/status")
def status():
    return {"status": "Server is running"}
