import math
import random
import numpy as np
import torch
import torch.nn as nn
import torch.optim as optim
import torch.nn.functional as F
from torch.distributions import Bernoulli
from collections import deque, namedtuple
import matplotlib
matplotlib.use('Agg')  # backend 'Agg'
import matplotlib.pyplot as plt

# Set device
device = torch.device("cuda" if torch.cuda.is_available() else "cpu")

# Reward normalization parameters
reward_min = float('inf')
reward_max = float('-inf')

class ReplayBuffer:
    def __init__(self, capacity):
        self.capacity = capacity
        self.buffer = deque(maxlen=capacity)
        self.experience = namedtuple('Experience', ['state', 'action', 'reward', 'next_state', 'done'])

    def normalize(self, state):
        state = np.array(state)
        state = (state - np.mean(state)) / (np.std(state) + 1e-5)  # Add 1e-5 to avoid division by 0
        return state

    @staticmethod
    def normalize_reward(reward):
        global reward_min, reward_max
        if reward < reward_min:
            reward_min = reward
        if reward > reward_max:
            reward_max = reward
        if reward_max > reward_min:
            return (reward - reward_min) / (reward_max - reward_min)
        else:
            return 0.0  # Avoid division by zero

    def add(self, state, action, reward, next_state, done):
        state = self.normalize(state)  # Chuẩn hóa state
        next_state = self.normalize(next_state)  # Chuẩn hóa next_state
        reward = self.normalize_reward(reward)
        e = self.experience(state, action, reward, next_state, done)
        self.buffer.append(e)

    def sample(self, batch_size):
        experiences = random.sample(self.buffer, batch_size)
        states = torch.FloatTensor(np.array([e.state for e in experiences])).to(device)
        actions = torch.FloatTensor(np.array([e.action for e in experiences])).to(device).unsqueeze(1)
        rewards = torch.FloatTensor(np.array([e.reward for e in experiences])).to(device).unsqueeze(1)
        next_states = torch.FloatTensor(np.array([e.next_state for e in experiences])).to(device)
        dones = torch.FloatTensor(np.array([e.done for e in experiences])).to(device).unsqueeze(1)
        return states, actions, rewards, next_states, dones

    def __len__(self):
        return len(self.buffer)

class SoftQNetwork(nn.Module):
    def __init__(self, state_dim, action_dim):
        super(SoftQNetwork, self).__init__()
        self.linear1 = nn.Linear(state_dim + action_dim, 256)
        self.linear2 = nn.Linear(256, 256)
        self.linear3 = nn.Linear(256, 1)

    def forward(self, state, action):
        if state.dim() == 1:
            state = state.unsqueeze(0)
        if action.dim() == 1:
            action = action.unsqueeze(0)
        x = torch.cat([state, action], dim=1)
        x = F.relu(self.linear1(x))
        x = F.relu(self.linear2(x))
        q = self.linear3(x)
        return q

class PolicyNetwork(nn.Module):
    def __init__(self, state_dim):
        super(PolicyNetwork, self).__init__()
        self.linear1 = nn.Linear(state_dim, 256)
        self.linear2 = nn.Linear(256, 256)
        self.prob = nn.Linear(256, 1)
        self.initialize_weights()

    def forward(self, state):
        x = F.relu(self.linear1(state))
        x = F.relu(self.linear2(x))
        prob = torch.sigmoid(self.prob(x))
        #print(f"State: {state}, Prob: {prob}")
        return prob
    
    def initialize_weights(self):
        nn.init.xavier_uniform_(self.linear1.weight)
        nn.init.xavier_uniform_(self.linear2.weight)
        nn.init.xavier_uniform_(self.prob.weight)

    def sample_action(self, state):
        prob = self.forward(state)
        m = Bernoulli(prob)
        action = m.sample()
        log_prob = m.log_prob(action)
        return action, log_prob

    def get_action_probability(self, state):
        state = torch.FloatTensor(state).unsqueeze(0).to(device)
        prob = self.forward(state)
        return prob.cpu().detach().numpy()[0]

class SACAgent:
    def __init__(self, state_dim, action_dim, learningrate, discount):
        self.actor = PolicyNetwork(state_dim).to(device)
        self.critic1 = SoftQNetwork(state_dim, action_dim).to(device)
        self.critic2 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic1 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic2 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic1.load_state_dict(self.critic1.state_dict())
        self.target_critic2.load_state_dict(self.critic2.state_dict())

        self.actor_optimizer = optim.Adam(self.actor.parameters(), lr=learningrate)
        self.critic1_optimizer = optim.Adam(self.critic1.parameters(), lr=learningrate)
        self.critic2_optimizer = optim.Adam(self.critic2.parameters(), lr=learningrate)

        self.discount = discount
        self.tau = 0.005

        # Adaptive alpha
        self.target_entropy = -action_dim
        self.log_alpha = torch.tensor(0.0, requires_grad=True)
        self.alpha_optimizer = optim.Adam([self.log_alpha], lr=learningrate)
        self.alpha = self.log_alpha.exp().item()

        self.replay_buffer = ReplayBuffer(capacity=2000000)
        self.critic1_loss_history = []
        self.critic2_loss_history = []
        self.actor_loss_history = []
        self.alpha_loss_history = []
        self.rewards_history = []
        self.action_history = []  

    def preprocess_state(self, state):
        state = np.array(state)
        state = (state - np.mean(state)) / (np.std(state) + 1e-5)  # Add 1e-5 to avoid division by 0
        return state
    
    def select_action(self, state):
        prob = self.actor.get_action_probability(state)
        action = 1 if prob > 0.5 else 0
        self.action_history.append(action)  
        return action

    def get_action_probability(self, state):
        state = self.preprocess_state(state)  # Normalize state
        return self.actor.get_action_probability(state)

    def train(self, batch_size=128):
        print("TRAINNNNNNNNNNNNNNNNNNNN")
        if len(self.replay_buffer) < batch_size:
            return

        states, actions, rewards, next_states, dones = self.replay_buffer.sample(batch_size)

        with torch.no_grad():
            next_actions, next_log_probs = self.actor.sample_action(next_states)
            target_q1 = self.target_critic1(next_states, next_actions)
            target_q2 = self.target_critic2(next_states, next_actions)
            target_q = torch.min(target_q1, target_q2) - self.alpha * next_log_probs
            target_q = rewards + (1 - dones) * self.discount * target_q

        current_q1 = self.critic1(states, actions)
        current_q2 = self.critic2(states, actions)

        assert current_q1.shape == target_q.shape, f"current_q1.shape: {current_q1.shape}, target_q.shape: {target_q.shape}"
        assert current_q2.shape == target_q.shape, f"current_q2.shape: {current_q2.shape}, target_q.shape: {target_q.shape}"

        critic1_loss = F.mse_loss(current_q1, target_q)
        critic2_loss = F.mse_loss(current_q2, target_q)

        self.critic1_optimizer.zero_grad()
        critic1_loss.backward()
        self.critic1_optimizer.step()

        self.critic2_optimizer.zero_grad()
        critic2_loss.backward()
        self.critic2_optimizer.step()

        self.critic1_loss_history.append(critic1_loss.item())
        self.critic2_loss_history.append(critic2_loss.item())

        new_actions, log_probs = self.actor.sample_action(states)
        q1_new = self.critic1(states, new_actions)
        q2_new = self.critic2(states, new_actions)
        actor_loss = (self.alpha * log_probs - torch.min(q1_new, q2_new)).mean()
        
        self.actor_loss_history.append(actor_loss.item())
        # self.rewards_history.append(rewards.item())

        self.actor_optimizer.zero_grad()
        actor_loss.backward()
        self.actor_optimizer.step()

        for param, target_param in zip(self.critic1.parameters(), self.target_critic1.parameters()):
            target_param.data.copy_(self.tau * param.data + (1 - self.tau) * target_param.data)

        for param, target_param in zip(self.critic2.parameters(), self.target_critic2.parameters()):
            target_param.data.copy_(self.tau * param.data + (1 - self.tau) * target_param.data)

        # Update alpha
        alpha_loss = -(self.log_alpha * (log_probs + self.target_entropy).detach()).mean()
        
        self.alpha_optimizer.zero_grad()
        alpha_loss.backward()
        self.alpha_optimizer.step()
        
        self.alpha = self.log_alpha.exp().item()
        self.alpha_loss_history.append(alpha_loss.item())
        
        self.rewards_history.append(rewards.sum().item())
        print("TRAINED")
        self.plot_training_history()

    def add_reward(self, reward):
        self.rewards_history.append(reward)
        
    def print_action_history(self):
        print("Action history:")
        for action in self.action_history:
            print(action)

    def plot_training_history(self):
        plt.figure(figsize=(16, 6))
        plt.subplot(1, 4, 1)
        plt.plot(self.critic1_loss_history, label='Critic 1 Loss')
        plt.plot(self.critic2_loss_history, label='Critic 2 Loss')
        plt.xlabel('Episodes')
        plt.ylabel('Loss')
        plt.legend()

        plt.subplot(1, 4, 2)
        plt.plot(self.actor_loss_history, label='Actor Loss')
        plt.xlabel('Episodes')
        plt.ylabel('Loss')
        plt.legend()

        plt.subplot(1, 4, 3)
        plt.plot(self.alpha_loss_history, label='Alpha Loss')
        plt.xlabel('Episodes')
        plt.ylabel('Loss')
        plt.legend()

        plt.subplot(1, 4, 4)
        plt.plot(self.rewards_history, label='Rewards')
        plt.xlabel('Episodes')
        plt.ylabel('Total Reward')
        plt.legend()

        plt.tight_layout()
        plt.savefig('logs/training_history.png') 
        plt.close()

class Environment:
    def __init__(self):
        state_dim = 13
        action_dim = 1
        learningrate = 1e-4
        discount = 0.995
        self.agent = SACAgent(state_dim, action_dim, learningrate, discount)
