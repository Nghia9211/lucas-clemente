import math
import os
import csv
import random
import numpy as np
import torch
import torch.nn as nn
import torch.optim as optim
import torch.nn.functional as F
from torch.distributions import Bernoulli
import torch.optim.lr_scheduler as lr_scheduler
from torch.distributions import Normal

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
        state = self.normalize(state)  #  Normalize state
        next_state = self.normalize(next_state)  
        # reward = self.normalize_reward(reward)
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
        # print("Before: ",action)
        if state.dim() == 1:
            state = state.unsqueeze(0)
        if action.dim() == 1:
            action = action.unsqueeze(0)
        elif action.dim() == 3:
            action = action.squeeze(-1)  # Add this to reduce extra dimension if action is (batch, 1, 1)
        # print("After: ",action)
        x = torch.cat([state, action], dim=-1)
        x = F.relu(self.linear1(x))
        x = F.relu(self.linear2(x))
        q = self.linear3(x)
        return q

# class PolicyNetwork(nn.Module):
#     def __init__(self, state_dim):
#         super(PolicyNetwork, self).__init__()
#         self.linear1 = nn.Linear(state_dim, 256)
#         self.linear2 = nn.Linear(256, 256)
#         self.prob = nn.Linear(256, 1)
#         self.initialize_weights()

#     def forward(self, state):
#         x = F.relu(self.linear1(state))
#         x = F.relu(self.linear2(x))
#         # Scale output to range [0.5, 2]
#         action = 0.5 + 1.5 * torch.sigmoid(self.prob(x))
#         return action
    
#     def initialize_weights(self):
#         nn.init.xavier_uniform_(self.linear1.weight)
#         nn.init.xavier_uniform_(self.linear2.weight)
#         nn.init.xavier_uniform_(self.prob.weight)


#     def sample_action(self, state):
#         mu = self.forward(state)
#         std = torch.full_like(mu, 0.7)  # std cố định

#         dist = Normal(mu, std)
#         action = dist.rsample()  # dùng rsample để có thể backprop
#         log_prob = dist.log_prob(action).sum(dim=-1, keepdim=True)

#         # Clamp về khoảng [0.5, 2.0]
#         action = action.clamp(0.5, 2.0)

#         return action, log_prob


#     def get_action_probability(self, state):
#         state = torch.FloatTensor(state).unsqueeze(0).to(device)
#         prob = self.forward(state)
#         return prob.cpu().detach().numpy()[0]


class PolicyNetwork(nn.Module):
    def __init__(self, state_dim):
        super(PolicyNetwork, self).__init__()
        self.linear1 = nn.Linear(state_dim, 256)
        self.linear2 = nn.Linear(256, 256)
        self.mu_head = nn.Linear(256, 1)
        self.log_std_head = nn.Linear(256, 1)

    def forward(self, state):
        x = F.relu(self.linear1(state))
        x = F.relu(self.linear2(x))
        mu = self.mu_head(x)
        log_std = self.log_std_head(x).clamp(-20, 2)  # tránh std quá lớn hoặc quá nhỏ
        std = log_std.exp()
        return mu, std

    def sample_action(self, state):
        mu, std = self.forward(state)
        dist = Normal(mu, std)
        raw_action = dist.rsample()
        log_prob = dist.log_prob(raw_action)

        # Sử dụng tanh rồi scale về khoảng [0.5, 2.0]
        tanh_action = torch.tanh(raw_action)
        action = 0.75 * tanh_action + 1.25  # Scale [-1, 1] → [0.5, 2.0]

        # Tính toán lại log_prob do có hàm tanh
        log_prob -= torch.log(0.75 * (1 - tanh_action.pow(2)) + 1e-6)
        log_prob = log_prob.sum(dim=-1, keepdim=True)

        return action, log_prob
    
    def  get_action_probability(self, state, eval_mode=False):
        # state = self.preprocess_state(state)
        state = torch.FloatTensor(state).unsqueeze(0).to(device)
        with torch.no_grad():
            if eval_mode:
                mu, _ = self.forward(state)
                action = torch.tanh(mu)
                action = 0.75 * action + 1.25
            else:
                action, _ = self.sample_action(state)
        return action.cpu().numpy().flatten()





class SACAgent:
    def __init__(self, state_dim, action_dim, learningrate, discount, tau):
        self.actor = PolicyNetwork(state_dim).to(device)
        self.critic1 = SoftQNetwork(state_dim, action_dim).to(device)
        self.critic2 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic1 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic2 = SoftQNetwork(state_dim, action_dim).to(device)
        self.target_critic1.load_state_dict(self.critic1.state_dict())
        self.target_critic2.load_state_dict(self.critic2.state_dict())

        self.actor_optimizer = optim.Adam(self.actor.parameters(), lr=learningrate)
        self.critic1_optimizer = optim.Adam(self.critic1.parameters(), lr=learningrate, weight_decay=1e-4)
        self.critic2_optimizer = optim.Adam(self.critic2.parameters(), lr=learningrate, weight_decay=1e-4)

        # Adaptive alpha
        self.target_entropy = -(action_dim * 0.5)
        self.log_alpha = torch.tensor(0.0, requires_grad=True, device=device)  # Đảm bảo log_alpha ở đúng thiết bị
        self.alpha_optimizer = optim.Adam([self.log_alpha], lr=learningrate) 

        # Thêm Learning Rate Scheduler cho Actor, Critic và Alpha
        self.actor_scheduler = lr_scheduler.ReduceLROnPlateau(self.actor_optimizer, mode='min', factor=0.99, patience=5)
        self.critic1_scheduler = lr_scheduler.ReduceLROnPlateau(self.critic1_optimizer, mode='min', factor=0.99, patience=5)
        self.critic2_scheduler = lr_scheduler.ReduceLROnPlateau(self.critic2_optimizer, mode='min', factor=0.99, patience=5)
        self.alpha_scheduler = lr_scheduler.ReduceLROnPlateau(self.alpha_optimizer, mode='min', factor=0.99, patience=5)

        self.alpha = self.log_alpha.exp().item()

        self.discount = discount
        self.tau = tau

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

    def get_action(self, state):
        state = self.preprocess_state(state)  #  Normalize state
        return self.actor.get_action_probability(state)
    
    def train(self, batch_size=2048, model_id=0):
        print("Training...")
        if len(self.replay_buffer) < batch_size:
            return
        print("Enough Buffer...")
        
        states, actions, rewards, next_states, dones = self.replay_buffer.sample(batch_size)
        print(f"Negative rewards: {(rewards < 0).sum().item()}")
        print(f"Sum Reward: {rewards.sum().item()}")
        for _ in range(1):  # Cập nhật critic 3 lần trước khi cập nhật actor
            with torch.no_grad():
                next_actions, next_log_probs = self.actor.sample_action(next_states)
                # noise = torch.normal(mean=0, std=0.2, size=next_actions.shape).clamp(-0.5, 0.5).to(device)
                # next_actions = (next_actions + noise).clamp(-1, 1)  # Giới hạn giá trị hành động
                target_q1 = self.target_critic1(next_states, next_actions)
                target_q2 = self.target_critic2(next_states, next_actions)
                target_q = torch.min(target_q1, target_q2) - self.alpha * next_log_probs
                target_q = rewards + (1 - dones) * self.discount * target_q

            current_q1 = self.critic1(states, actions)
            current_q2 = self.critic2(states, actions)

            critic1_loss = F.mse_loss(current_q1, target_q)
            critic2_loss = F.mse_loss(current_q2, target_q)

            self.critic1_optimizer.zero_grad()
            critic1_loss.backward()
            # Gradient clipping for critic1
            # torch.nn.utils.clip_grad_norm_(self.critic1.parameters(), max_norm=1.0)
            self.critic1_optimizer.step()

            self.critic2_optimizer.zero_grad()
            critic2_loss.backward()
            # Gradient clipping for critic2
            # torch.nn.utils.clip_grad_norm_(self.critic2.parameters(), max_norm=1.0)
            self.critic2_optimizer.step()

            self.critic1_loss_history.append(critic1_loss.item())
            self.critic2_loss_history.append(critic2_loss.item())

            # Scheduler step for critics
            self.critic1_scheduler.step(critic1_loss)
            self.critic2_scheduler.step(critic2_loss)

        new_actions, log_probs = self.actor.sample_action(states)
        q1_new = self.critic1(states, new_actions)
        q2_new = self.critic2(states, new_actions)
        actor_loss = (self.alpha * log_probs - torch.min(q1_new, q2_new)).mean()

        self.actor_loss_history.append(actor_loss.item())

        self.actor_optimizer.zero_grad()
        actor_loss.backward()
        self.actor_optimizer.step()

        # Cập nhật alpha
        alpha_loss = -(self.log_alpha.exp() * (log_probs + self.target_entropy).detach()).mean()
        self.alpha_optimizer.zero_grad()
        alpha_loss.backward()
        self.alpha_optimizer.step()

        # Cập nhật giá trị alpha
        self.alpha = self.log_alpha.exp().item()
        self.alpha_loss_history.append(alpha_loss.item())

        # Updating target networks
        for param, target_param in zip(self.critic1.parameters(), self.target_critic1.parameters()):
            target_param.data.copy_(self.tau * param.data + (1 - self.tau) * target_param.data)

        for param, target_param in zip(self.critic2.parameters(), self.target_critic2.parameters()):
            target_param.data.copy_(self.tau * param.data + (1 - self.tau) * target_param.data)

        # Update alpha
        # Alpha loss calculation (based on the SAC algorithm theory)
        # alpha_loss = -(self.log_alpha.exp() * (log_probs + self.target_entropy).detach()).mean()

        # self.alpha_optimizer.zero_grad()
        # alpha_loss.backward()
        # self.alpha_optimizer.step()

        # # Update alpha value
        # self.alpha = self.log_alpha.exp().item()
        # self.alpha_loss_history.append(alpha_loss.item())

        # Scheduler step cho alpha
        # self.alpha_scheduler.step(alpha_loss)
        self.rewards_history.append(rewards.sum().item() / batch_size)
        print("TRAINED")
        self.plot_training_history(model_id)
        if len(self.critic1_loss_history) == 1000:
            self.save_training_history(model_id)

    def add_reward(self, reward):
        print(reward)
        self.rewards_history.append(reward)
        
    def print_action_history(self):
        print("Action history:")
        for action in self.action_history:
            print(action)

    def save_training_history(self, model_id):
        # Tạo thư mục lưu trữ nếu chưa có
        os.makedirs('logs', exist_ok=True)

        # Lưu lịch sử critic1_loss
        with open(f'logs/critic1_loss_history_{model_id}.csv', mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(['Episode', 'Critic 1 Loss'])
            writer.writerows(enumerate(self.critic1_loss_history, start=1))
        
        # Lưu lịch sử critic2_loss
        with open(f'logs/critic2_loss_history_{model_id}.csv', mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(['Episode', 'Critic 2 Loss'])
            writer.writerows(enumerate(self.critic2_loss_history, start=1))

        # Lưu lịch sử actor_loss
        with open(f'logs/actor_loss_history_{model_id}.csv', mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(['Episode', 'Actor Loss'])
            writer.writerows(enumerate(self.actor_loss_history, start=1))

        # Lưu lịch sử alpha_loss
        with open(f'logs/alpha_loss_history_{model_id}.csv', mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(['Episode', 'Alpha Loss'])
            writer.writerows(enumerate(self.alpha_loss_history, start=1))

        # Lưu lịch sử rewards
        with open(f'logs/rewards_history_{model_id}.csv', mode='w', newline='') as file:
            writer = csv.writer(file)
            writer.writerow(['Episode', 'Reward'])
            writer.writerows(enumerate(self.rewards_history, start=1))
        
        print("Lịch sử đào tạo đã được lưu vào các tệp CSV.")
    def plot_training_history(self, model_id):
        # Ensure logs directory exists
        # if not os.path.exists('logs'):
        #     os.makedirs('logs')

        # Print histories for debugging
        # print("Critic1 Loss History:", self.critic1_loss_history)
        # print("Critic2 Loss History:", self.critic2_loss_history)
        # print("Actor Loss History:", self.actor_loss_history)
        # print("Rewards History:", self.rewards_history)
        plt.rcParams.update({'font.size': 14, 'font.family': 'sans-serif'})

        plt.figure(figsize=(16, 4))
        plt.subplot(1, 4, 1)
        plt.plot(self.critic1_loss_history, label='Critic 1 Loss', color='tab:orange')
        plt.plot(self.critic2_loss_history, label='Critic 2 Loss', color='tab:pink')
        plt.xlabel('Episodes')
        plt.ylabel('Value')
        plt.legend()

        plt.subplot(1, 4, 2)
        plt.plot(self.actor_loss_history, color='tab:orange')
        plt.xlabel('Episodes')
        plt.ylabel('Actor Loss')
        # plt.legend()

        plt.subplot(1, 4, 3)
        plt.plot(self.alpha_loss_history, color='tab:orange')
        plt.xlabel('Episodes')
        plt.ylabel('Alpha Loss')
        # plt.legend()

        plt.subplot(1, 4, 4)
        plt.plot(self.rewards_history, color='tab:orange')
        plt.xlabel('Episodes')
        plt.ylabel('Average Reward')
        # plt.legend()

        plt.tight_layout()
        plt.savefig(f'logs/training_history_{model_id}.pdf', format='pdf') 
        plt.close()


class Environment:
    def __init__(self):
        state_dim = 4
        action_dim = 1
        learningrate = 1e-4
        discount = 0.995
        tau = 0.001
        self.agent = SACAgent(state_dim, action_dim, learningrate, discount, tau)
