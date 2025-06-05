#!/usr/bin/python

from mininet.net import Mininet
from mininet.node import Controller, OVSController, OVSSwitch, Node
from mininet.cli import CLI
from mininet.log import setLogLevel, info
from mininet.link import TCLink, Intf
import os

def myNetwork():

    net = Mininet(controller=OVSController, link=TCLink, switch=OVSSwitch)

    info('*** Adding controller\n')
    net.addController('c0')

    info('*** Adding hosts\n')
    h1 = net.addHost('h1', ip='10.0.0.1/24', defaultRoute='via 10.0.0.254')
    h2 = net.addHost('h2', ip='10.0.0.2/24', defaultRoute='via 10.0.0.254')

    info('*** Adding switch\n')
    s1 = net.addSwitch('s1')

    info('*** Adding router\n')
    r1 = net.addHost('r1', ip='10.0.0.254/24')

    info('*** Creating links\n')
    net.addLink(h1, s1)
    net.addLink(h2, s1)
    net.addLink(s1, r1)
    
    # Connect router to physical interface
    Intf('br0', node=r1)

    info('*** Starting network\n')
    net.start()

    info('*** Configuring router\n')
    # Cấu hình giao diện thứ hai của router để lấy IP từ mạng vật lý
    r1.cmd('ifconfig r1-eth1 up')
    r1.cmd('dhclient r1-eth1')  # Yêu cầu địa chỉ IP từ DHCP server của mạng vật lý

    info('*** Configuring NAT\n')
    r1.cmd('sysctl -w net.ipv4.ip_forward=1')
    r1.cmd('iptables -t nat -A POSTROUTING -o r1-eth1 -j MASQUERADE')
    r1.cmd('iptables -A FORWARD -i r1-eth1 -o r1-eth0 -m state --state RELATED,ESTABLISHED -j ACCEPT')
    r1.cmd('iptables -A FORWARD -i r1-eth0 -o r1-eth1 -j ACCEPT')

    info('*** Configuring DNS\n')
    # Cấu hình DNS cho các host
    h1.cmd('echo "nameserver 8.8.8.8" > /etc/resolv.conf')
    h2.cmd('echo "nameserver 8.8.8.8" > /etc/resolv.conf')

    info('*** Post configure switches and hosts\n')
    CLI(net)

    net.stop()

if __name__ == '__main__':
    setLogLevel('info')
    myNetwork()
