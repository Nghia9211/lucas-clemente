import torch
import torch.nn as nn
import torch.optim as optim
import numpy as np
import random
import os.path


# Define the Deep Q-Network
class DQN(nn.Module):
    def __init__(self, input_size, output_size):
        super(DQN, self).__init__()
        self.fc1 = nn.Linear(input_size, 64)
        self.fc2 = nn.Linear(64, 64)
        self.fc3 = nn.Linear(64, output_size)

    def forward(self, x):
        x = torch.relu(self.fc1(x))
        x = torch.relu(self.fc2(x))
        x = self.fc3(x)
        return x

# Define the Deep Q-Learning Agent
class DQNAgent:
    def __init__(self, state_size, action_size, learning_rate=0.001, gamma=0.99, epsilon=1.0, epsilon_decay=0.995, epsilon_min=0.01):
        self.state_size = state_size
        self.action_size = action_size
        self.memory = []
        self.batch_size = 300
        self.learning_rate = learning_rate
        self.gamma = gamma
        self.epsilon = epsilon
        self.epsilon_decay = epsilon_decay
        self.epsilon_min = epsilon_min
        self.model = DQN(state_size, action_size)
        self.optimizer = optim.Adam(self.model.parameters(), lr=learning_rate)
        self.criterion = nn.MSELoss()

    def remember(self, state, action, reward, next_state, done):
        self.memory.append((state, action, reward, next_state, done))

    def act(self, state):
        if np.random.rand() <= self.epsilon:
            return random.randrange(self.action_size)
        else:
            state_values = [state['CWNDf'], state['INPf'], state['SRTTf'], state['CWNDs'], state['INPs'], state['SRTTs']]
            q_values = self.model(torch.FloatTensor(state_values))
            return torch.argmax(q_values).item()

    def replay(self):
        if len(self.memory) < self.batch_size:
            return
        minibatch = random.sample(self.memory, self.batch_size)
        for state, action, reward, next_state, done in minibatch:
            target = reward
            if not done:
                # Trích xuất các giá trị từ next_state
                next_state_values = [next_state['CWNDf'], next_state['INPf'], next_state['SRTTf'], next_state['CWNDs'], next_state['INPs'], next_state['SRTTs']]
                # Chuyển đổi thành tensor
                next_state_tensor = torch.FloatTensor(next_state_values)
                # Tính toán giá trị target dựa trên next_state_tensor
                target = reward + self.gamma * torch.max(self.model(next_state_tensor)).item()
            # Chuyển đổi state thành tensor
            state_values = [state['CWNDf'], state['INPf'], state['SRTTf'], state['CWNDs'], state['INPs'], state['SRTTs']]
            state_tensor = torch.FloatTensor(state_values)
            target_f = self.model(state_tensor)
            target_f[action] = target
            self.optimizer.zero_grad()
            loss = self.criterion(target_f, self.model(state_tensor))
            loss.backward()
            self.optimizer.step()
            print('train: ')
        if self.epsilon > self.epsilon_min:
            self.epsilon *= self.epsilon_decay
        
        print('epsilon', self.epsilon)

    def save_model(self, filename):
        torch.save(self.model.state_dict(), filename)
        
    def load_model(self, filename):
        self.model.load_state_dict(torch.load(filename))
        #self.model.eval()  # Chuyển sang chế độ đánh giá

# Define the environment
class Environment:
    def __init__(self):
        self.state_size = 6  # CWND, smooth RTT, INP
        self.action_size = 2  # wifi, lte
        self.agent = DQNAgent(self.state_size, self.action_size)

    def step(self, action):
        # Perform action and return next_state, reward, done (assuming a simplified environment)
        # Update state
        next_state = [0, 0, 0, 0, 0, 0]  # Placeholder for next state
        # Calculate reward (latest RTT)
        reward = 0  # Placeholder for reward
        # Check if episode is done
        done = False  # Placeholder for done
        return next_state, reward, done

# # Main loop
# env = Environment()
# episodes = 1000
# for episode in range(episodes):
#     state = [0, 0, 0]  # Initial state
#     total_reward = 0
#     done = False
#     while not done:
#         action = env.agent.act(state)
#         next_state, reward, done = env.step(action)
#         env.agent.remember(state, action, reward, next_state, done)
#         state = next_state
#         total_reward += reward
#         env.agent.replay()
#     print("Episode:", episode + 1, "Total Reward:", total_reward)

# Interface for REST API using Flask (not implemented)
