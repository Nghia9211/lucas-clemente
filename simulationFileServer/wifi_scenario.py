#!/usr/bin/env python
#!/usr/bin/env bash

'Setting the position of nodes and providing mobility'

import sys
import time
import argparse
from threading import Thread, Event
import threading
import os
import random

from mininet.log import setLogLevel, info
from mininet.node import Controller, Node
from mn_wifi.net import Mininet_wifi
from mn_wifi.cli import CLI
from mn_wifi.wmediumdConnector import interference
from mn_wifi.link import wmediumd
from mn_wifi.node import Station

# SERVER_CMD = "PYTHONPATH=../serverdrl gunicorn -w 12 -b 0.0.0.0:8080 app_multi:app > ./logs/server-gunicorn.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD = "python ../serverdrl/app.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD_SACMULTI = "python ../serverdrl/app_multi.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD_SACMULTIJOINCC = "python ../serverdrl/app_multiJoinCC.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"

CERTPATH = "--certpath ./quic/quic_go_certs"
SCH = "-scheduler %s"
ARGS = "-bind :6121 -www ./www/"
END = ">> ./logs/server.logs 2>&1"

CLIENT_CMD = "./clientMPQUIC{id} -n 1 -t 0 -m -clt {id}"
CLIENT_FIL = "https://10.0.0.20:6121/files/%s-{id}"
CLIENT_END = ">> ./logs/client.logs 2>&1"

with_background = 0  # Global variable to control the creation of background traffic
stop_event = Event()  # Event to signal when to stop background traffic
global_variable = time.time()
global_flag = False

class LinuxRouter(Node):
    def config(self, **params):
        super(LinuxRouter, self).config(**params)
        self.cmd('sysctl net.ipv4.ip_forward=1')

    def terminate(self):
        self.cmd('sysctl net.ipv4.ip_forward=0')
        super(LinuxRouter, self).terminate()

def runClient(station, id, client_cmd):
    global global_flag
    for i in range(200):
        print(client_cmd.format(id=id))
        station.sendCmd(client_cmd.format(id=id))
        output = station.monitor(timeoutms=30000)

        # current_time = time.time()
        # tmp_time = float(5*(i+1)) - float(current_time - global_variable)
        # if tmp_time < 0:
        #     break
        # print(tmp_time)
        # time.sleep(tmp_time)
        time.sleep(1)
        if global_flag == True:
            break
    global_flag = True

def configClient(sta, id):
    sta.cmd("ifconfig sta{id}-wlan0 down".format(id=id))
    sta.cmd("ip link set sta{id}-wlan0 name wlan0".format(id=id))  
    sta.cmd("ifconfig wlan0 up")

    sta.cmd("ifconfig sta{id}-wlan1 down".format(id=id))
    sta.cmd("ip link set sta{id}-wlan1 name wlan1".format(id=id))  
    sta.cmd("ifconfig wlan1 up")

    sta.cmd("ifconfig wlan0 192.168.2.{id} netmask 255.255.255.0".format(id=id))
    sta.cmd("ifconfig wlan1 172.16.0.{id} netmask 255.240.0.0".format(id=id))

    sta.cmd("ip rule add from 192.168.2.{id}/24 table 1".format(id=id))
    sta.cmd("ip rule add from 172.16.0.{id}/12 table 2".format(id=id))

    sta.cmd("ip route add default nexthop via 192.168.2.2 dev wlan0 weight 1 nexthop via 172.16.0.2 dev wlan1 weight 1")
    sta.cmd("ip route add default via 192.168.2.2 table 1")
    sta.cmd("ip route add default via 172.16.0.2 table 2")

def background_traffic(client1, server1, frequency, stop_event):
    # Wait a bit to ensure the network is fully started
    # server1.cmd("iperf3 -s -p 5201")
    #time.sleep(5)
    while not stop_event.is_set():
        # client1.cmd("iperf -c 192.168.5.4 -p 5201 -B 192.168.5.3 -V -t 9 > ./logs/bg.logs")
        server1.cmd("ping -c 1 192.168.5.2")
        client1.cmd("ping -c 1 192.168.5.2")
        time.sleep(frequency)

def topology(args, server_cmd, client_cmd):
    net = Mininet_wifi(controller=Controller, link=wmediumd, wmediumd_mode=interference, fading_cof=3)

    info("*** Creating nodes\n")
    h1 = net.addHost('h1', mac='00:00:00:00:00:01', ip='10.0.0.20/8', defaultRoute='10.0.0.2')
    s1 = net.addSwitch('s1', mac='00:00:00:00:00:02')
    r0 = net.addHost('r0', cls=LinuxRouter, ip='192.168.2.2/24')
    ap1 = net.addAccessPoint('ap1', mac='00:00:00:00:00:04', ssid='lte-ssid', mode='g', channel='1', position='55,50,0')
    ap2 = net.addAccessPoint('ap2', mac='00:00:00:00:00:05', ssid='wifi-ssid', mode='g', channel='6', position='45,50,0')
    #ax2: channel 1; g,n2: channel 1-6; a: channel 36-40
    stations = []
    for i in range(3, 3 + int(args.clt)):
        if args.mdl == 'mobi':
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i)
            stations.append(sta)
        elif args.mdl == 'none': #same place
            x = 50
            y = 70
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)
        elif args.mdl == 'dif1': 
            x = i * 5
            y = i * 5
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)
        elif args.mdl == 'dif2':
            x = i * 5
            y = 50 - i * 3 * (-1)
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)

    c1 = net.addController('c1')

    info("*** Configuring Propagation Model\n")
    net.setPropagationModel(model="logDistance", exp=3)

    info("*** Configuring wifi nodes\n")
    net.configureWifiNodes()

    info("*** Associating and Creating links\n")
    net.addLink(h1, s1, bw=300, use_hfsc=True)
    net.addLink(ap1, r0, intfName2='r0-eth1', use_hfsc=True, params2={'ip': '192.168.2.2/24'})
    net.addLink(ap2, r0, intfName2='r0-eth2', use_hfsc=True, params2={'ip': '172.16.0.2/12'})
    net.addLink(s1, r0, intfName2='r0-eth3', use_hfsc=True, params2={'ip': '10.0.0.2/8'})

    if with_background == 1:
        server1 = net.addHost('server1', mac='01:00:00:00:00:01', ip='192.168.5.4/24', defaultRoute='192.168.5.2')
        client1 = net.addHost('client1', mac='01:00:00:00:00:02', ip='192.168.5.3/24', defaultRoute='192.168.5.2')
        net.addLink(server1, s1, bw=300, use_hfsc=True)
        net.addLink(client1, r0, intfName2='r0-eth4', params2={'ip': '192.168.5.2/24'})

        client1.cmd("ip rule add from 192.168.5.3/24 table 1")
        client1.cmd("ip route add default nexthop via 192.168.5.2 dev client-eth0 weight 1")
        client1.cmd("ip route add default via 192.168.5.2 table 1")

    info("*** Starting network\n")
    # if '-p' not in args:
    #     net.plotGraph(max_x = 100, max_y = 100)

    if args.mdl == 'mobi':
        net.setMobilityModel(time=0, model='TruncatedLevyWalk', max_x=100, max_y=100, seed=20, min_v=2, max_v=4, velocity=(2., 4.), FL_MAX=200., alpha=0.5, variance=4.)

    net.build()

    for i, sta in enumerate(stations, start=3):
        configClient(sta, i)

    h1.cmd('ip route add default via 10.0.0.2')
    c1.start()
    ap1.start([c1])
    ap2.start([c1])
    s1.start([c1])

    if with_background == 1:
        r0.cmd('tcdel r0-eth1 --all')
        r0.cmd('tcset r0-eth1 --rate 10Mbps')
        r0.cmd('tcset r0-eth2 --rate {}Mbps'.format(args.bwd))
        ap1.cmd('tcset ap1-eth2 --rate 10Mbps')
        ap2.cmd('tcset ap2-eth2 --rate {}Mbps'.format(args.bwd))

        # Start background traffic in a separate thread after network is fully up
        bg_thread = threading.Thread(target=background_traffic, args=(client1, server1, float(args.freq), stop_event))
        bg_thread.daemon = True
        bg_thread.start()
    else:
        if int(args.var) == 0:
            r0.cmd('tcdel r0-eth1 --all')
            r0.cmd('tcdel r0-eth2 --all')
            r0.cmd('tcset r0-eth1 --rate {}Mbps --delay {}ms'.format(args.bwd, args.owd))
            r0.cmd('tcset r0-eth2 --rate 50Mbps --delay 15ms')
            ap1.cmd('tcset ap1-eth2 --rate {}Mbps --delay {}ms'.format(args.bwd, args.owd))
            ap2.cmd('tcset ap2-eth2 --rate 50Mbps --delay 15ms')

            # r0.cmd('tc qdisc del root dev r0-eth1')
            # r0.cmd('tc qdisc del root dev r0-eth2')
            # r0.cmd('tc qdisc add dev r0-eth1 root handle 5:0 hfsc default 1')
            # r0.cmd('tc class add dev r0-eth1 parent 5:0 classid 5:1 hfsc sc rate {}Mbit'.format(args.bwd))
            # r0.cmd('tc qdisc add dev r0-eth1 parent 5:1 netem delay {}ms'.format(args.owd))

            # r0.cmd('tc qdisc add dev r0-eth2 root handle 5:0 hfsc default 1')
            # r0.cmd('tc class add dev r0-eth2 parent 5:0 classid 5:1 hfsc sc rate 5Mbit')
            # r0.cmd('tc qdisc add dev r0-eth2 parent 5:1 netem delay 20ms')

            # ap1.cmd('tc qdisc del root dev ap1-eth2')
            # ap1.cmd('tc qdisc add dev ap1-eth2 root handle 5:0 hfsc default 1')
            # ap1.cmd('tc class add dev ap1-eth2 parent 5:0 classid 5:1 hfsc sc rate {}Mbit'.format(args.bwd))
            # ap1.cmd('tc qdisc add dev ap1-eth2 parent 5:1 netem delay {}ms'.format(args.owd))

            # ap2.cmd('tc qdisc del root dev ap2-eth2')
            # ap2.cmd('tc qdisc add dev ap2-eth2 root handle 5:0 hfsc default 1')
            # ap2.cmd('tc class add dev ap2-eth2 parent 5:0 classid 5:1 hfsc sc rate 5Mbit')
            # ap2.cmd('tc qdisc add dev ap2-eth2 parent 5:1 netem delay 20ms')
        else:
            varrate1 = 15.0 * float(args.var) / 100
            varrate2 = float(args.owd) * float(args.var) / 100
            r0.cmd('tcdel r0-eth1 --all')
            r0.cmd('tcdel r0-eth2 --all')
            r0.cmd('tcset r0-eth1 --rate {}Mbps --delay {}ms --delay-distro {} --delay-distribution pareto --loss {}%'.format(args.bwd, args.owd, varrate2, args.los))
            r0.cmd('tcset r0-eth2 --rate 50Mbps --delay 15ms --delay-distro {} --delay-distribution pareto --loss {}%'.format(varrate2, args.los))
            ap2.cmd('tcset ap1-eth2 --rate {}Mbps --delay {}ms'.format(args.bwd, args.owd))
            ap1.cmd('tcset ap2-eth2 --rate 50Mbps --delay 15ms')

            # r0.cmd('tc qdisc del root dev r0-eth1')
            # r0.cmd('tc qdisc del root dev r0-eth2')
            # r0.cmd('tc qdisc add dev r0-eth1 root handle 5:0 hfsc default 1')
            # r0.cmd('tc class add dev r0-eth1 parent 5:0 classid 5:1 hfsc sc rate {}Mbit'.format(args.bwd))
            # r0.cmd('tc qdisc add dev r0-eth1 parent 5:1 netem delay {}ms 1ms 75%'.format(args.owd))

            # r0.cmd('tc qdisc add dev r0-eth2 root handle 5:0 hfsc default 1')
            # r0.cmd('tc class add dev r0-eth2 parent 5:0 classid 5:1 hfsc sc rate 55Mbit ul rate 60Mbit')
            # r0.cmd('tc qdisc add dev r0-eth2 parent 5:1 netem delay 15ms 1.5ms 75%')

            # ap1.cmd('tc qdisc del root dev ap1-eth2')
            # ap1.cmd('tc qdisc add dev ap1-eth2 root handle 5:0 hfsc default 1')
            # ap1.cmd('tc class add dev ap1-eth2 parent 5:0 classid 5:1 hfsc sc rate {}Mbit'.format(args.bwd))
            # ap1.cmd('tc qdisc add dev ap1-eth2 parent 5:1 netem delay {}ms 1ms 75%'.format(args.owd))

            # ap2.cmd('tc qdisc del root dev ap2-eth2')
            # ap2.cmd('tc qdisc add dev ap2-eth2 root handle 5:0 hfsc default 1')
            # ap2.cmd('tc class add dev ap2-eth2 parent 5:0 classid 5:1 hfsc sc rate 55Mbit ul rate 60Mbit')
            # ap2.cmd('tc qdisc add dev ap2-eth2 parent 5:1 netem delay 15ms 1.5ms 75%')

            # r0.cmd("tc qdisc add dev r0-eth1 root netem limit 1000 delay {0}ms 1ms 75% loss 0.5 50% rate {1}Mbit".format(args.owd, args.bwd))
            # r0.cmd("tc qdisc add dev r0-eth2 root netem limit 1000 delay 15ms 1.5ms 75% loss 0.5 50% rate 50Mbit")

            # ap1.cmd("tc qdisc add dev ap1-eth2 root netem limit 1000 delay {0}ms 1ms 75% loss 0.5 50% rate {1}Mbit".format(args.owd, args.bwd))
            # ap2.cmd("tc qdisc add dev ap2-eth2 root netem limit 1000 delay 15ms 1.5ms 75% loss 0.5 50% rate 50Mbit")

    # print(args)
    print(server_cmd.format(num=args.clt))
    # print(client_cmd)
    CLI(net)
    h1.sendCmd(server_cmd.format(num=args.clt))
    time.sleep(10)
    global global_variable
    global_variable = time.time()

    threads = []
    for i, sta in enumerate(stations, start=3):
        t = threading.Thread(target=runClient, args=(sta, i, client_cmd))
        threads.append(t)
        t.start()

    for t in threads:
        t.join()

    h1.sendInt()
    h1.monitor()
    h1.waiting = False

    # Signal the background traffic thread to stop and wait for it to finish
    stop_event.set()
    if with_background == 1:
        bg_thread.join()
    # CLI(net)

    info("*** Stopping network\n")
    net.stop()
    time.sleep(1)

def do_training(args):
    global with_background
    if args.sch == "sac":
        server_cmd = " ".join([SERVER_CMD, CERTPATH, SCH % args.sch, ARGS, END])
    elif args.sch == "sacmulti" or args.sch == "sacrx":
        server_cmd = " ".join([SERVER_CMD_SACMULTI, CERTPATH, SCH % args.sch, ARGS, END])
    else:
        server_cmd = " ".join([SERVER_CMD_SACMULTIJOINCC, CERTPATH, SCH % args.sch, ARGS, END])

    client_cmd = " ".join([CLIENT_CMD, CLIENT_FIL % args.fil, CLIENT_END])
    setLogLevel('info')
    with_background = int(args.bg)  # Set the global variable based on the command-line argument
    topology(args, server_cmd, client_cmd)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Executes a test with defined scheduler')
    parser.add_argument('--model', dest="mdl", help="Mobility Model, or: none", required=True)
    parser.add_argument('--client', dest="clt", help="Client Number", required=True)
    parser.add_argument('--file', dest="fil", help="File (1MB, 2MB, 4MB)", required=True)
    parser.add_argument('--bandwidth', dest="bwd", help="bandwidth", required=True)
    parser.add_argument('--delay', dest="owd", help="delay", required=True)
    parser.add_argument('--variation', dest="var", help="variation", required=True)
    parser.add_argument('--loss', dest="los", help="loss", required=True)
    parser.add_argument('--scheduler', dest="sch", help="Scheduler (rtt, qsat, sac)", required=True)
    parser.add_argument('--background', dest="bg", help="Enable background traffic (1 or 0)", required=False, default=0)
    parser.add_argument('--frequency', dest="freq", help="Frequency of background traffic (seconds)", required=False, default=1)
    args = parser.parse_args()

    do_training(args)
