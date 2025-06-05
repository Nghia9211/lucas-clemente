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

# SERVER_CMD = "PYTHONPATH=../serverdrl gunicorn -w 12 -b 0.0.0.0:8080 app:app > ./logs/server-gunicorn.logs 2>&1 & ./serverMPQUIC"
# SERVER_CMD = "PYTHONPATH=../serverdrl gunicorn -w 4 -b 0.0.0.0:8080 app:app --log-file ./logs/server-flask.logs --log-level info & ./serverMPQUIC"
# SERVER_CMD="export PYTHONPATH=../serverdrl && export CLIENT_NUM={num} && uvicorn app_FASTAPI:app --host 0.0.0.0 --port 8080 --log-level debug > ./logs/server-uvicorn.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD = "python ../serverdrl/app.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD_SACMULTI = "python ../serverdrl/app_multi.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"
SERVER_CMD_SACMULTIJOINCC = "python ../serverdrl/app_multiJoinCC.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"

CERTPATH = "--certpath ./quic/quic_go_certs"
SCH = "-scheduler %s"
ARGS = "-bind :6121 -www ./www/"
END = ">> ./logs/server.logs 2>&1"

CLIENT_CMD = "./clientMPQUIC{id} -n 1 -t 0 -m -clt {id}"
# CLIENT_CMD = "./clientMPQUIC{id} -n 1 -t 0 -m -b -clt {id}" #for bulk file 
CLIENT_FIL = "https://10.0.0.20:6121/files/%s-{id}"
CLIENT_END = ">> ./logs/client.logs 2>&1"

with_background = 0  # Global variable to control the creation of background traffic
stop_event = Event()  # Event to signal when to stop background traffic
global_variable = time.time()
global_flag = False

def measure_rtt_wlan0(sta, ip, duration=3):
    """Đo RTT từ client đến server qua giao diện wlan0."""
    log_file = f"./logs/wlan0_rtt_{sta.name}.log"
    error_log = f"./logs/wlan0_rtt_{sta.name}_error.log"
    sta.cmd(f"ping -I wlan0 {ip} -w {duration} >> {log_file} 2>> {error_log}")

def measure_bandwidth_wlan0(sta, ip, duration=3):
    """Đo băng thông giữa client và server qua giao diện wlan0."""
    log_file = f"./logs/wlan0_bw_{sta.name}.log"
    error_log = f"./logs/wlan0_bw_{sta.name}_error.log"
    sta.cmd(f"iperf3 -c {ip} -B 192.168.2.{sta.name[-1]} -R -t {duration} -i 1 >> {log_file} 2>> {error_log}")

def log_station_position(sta):
    """Ghi lại tọa độ hiện tại của station vào tệp log."""
    log_file = f"./logs/wlan0_position_{sta.name}.log"
    with open(log_file, 'a') as f:
        f.write(f"Time: {time.time()} - Position: {sta.position}\n")
        # print(f"Logging position for {sta.name}: {position}")


class LinuxRouter(Node):
    def config(self, **params):
        super(LinuxRouter, self).config(**params)
        self.cmd('sysctl net.ipv4.ip_forward=1')

    def terminate(self):
        self.cmd('sysctl net.ipv4.ip_forward=0')
        super(LinuxRouter, self).terminate()

def runClient(station, id, client_cmd):
    global global_flag
    server_ip = "10.0.0.20"  # Địa chỉ IP của server
    for i in range(1000):
        print(client_cmd.format(id=id))
        # output = station.monitor(timeoutms=30000)
                
        # Ghi lại vị trí của station
        log_station_position(station)

        # Đo RTT và băng thông qua giao diện wlan0
        measure_rtt_wlan0(station, server_ip, duration=5)
        measure_bandwidth_wlan0(station, server_ip, duration=5)

        time.sleep(1)
        if global_flag:
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

#               wlan1-ap1-eth2     (192.168.2.2/24) eth1-r0-eth2 (10.0.1.2/24)  (10.0.1.1/24)-eth2
#   sta3                                                                                          s1-eth1 (10.0.0.1/24)      (10.0.0.20/24) eth1-h1
#               wlan1-ap2-eth2     (172.16.0.2/12)  eth1-r1-eth2 (10.0.2.2/24)  (10.0.2.1/24)-eth3
def topology(args, server_cmd, client_cmd):
    net = Mininet_wifi(controller=Controller, link=wmediumd, wmediumd_mode=interference, fading_cof=3)

    info("*** Creating nodes\n")
    h1 = net.addHost('h1', mac='00:00:00:00:00:01', ip='10.0.0.20/24', defaultRoute='10.0.0.1')
    
    # Thay switch s1 bằng router s1
    s1 = net.addHost('s1', cls=LinuxRouter, ip='10.0.0.1/24')  # Router s1 có IP chính là 10.0.0.1/8
    r0 = net.addHost('r0', cls=LinuxRouter, ip='192.168.2.2/24')
    r1 = net.addHost('r1', cls=LinuxRouter, ip='172.16.0.2/12')

    # ap1 = net.addAccessPoint('ap1', mac='00:00:00:00:00:04', ssid='lte-ssid', mode='g', channel='1', position='55,50,0', datapath='user')
    # ap2 = net.addAccessPoint('ap2', mac='00:00:00:00:00:05', ssid='wifi-ssid', mode='g', channel='6', position='45,50,0', datapath='user')
    ap1 = net.addAccessPoint('ap1', mac='00:00:00:00:00:04', ssid='lte-ssid', mode='g', channel='1', position='25,50,0')
    ap2 = net.addAccessPoint('ap2', mac='00:00:00:00:00:05', ssid='wifi-ssid', mode='g', channel='6', position='75,50,0')
    stations = []
    for i in range(3, 3 + int(args.clt)):
        if args.mdl == 'mob1' or args.mdl == 'mob2' or args.mdl == 'mob3':
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i)
            stations.append(sta)
        elif args.mdl == 'none': #same place
            x = 50
            y = 50
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)
        elif args.mdl == 'dif1': 
            # x = i * 5
            # y = i * 5
            x = 75
            y = 75
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)
        elif args.mdl == 'dif2':
            # x = i * 5
            # y = 50 - i * 3 * (-1)
            x = 95
            y = 95
            sta = net.addStation('sta%d' % i, wlans=2, mac='00:00:00:00:01:%02d' % i, position='%d,%d,0' % (x, y))
            stations.append(sta)

    c1 = net.addController('c1')

    info("*** Configuring Propagation Model\n")
    net.setPropagationModel(model="logDistance", exp=3)

    info("*** Configuring wifi nodes\n")
    net.configureWifiNodes()

    info("*** Associating and Creating links\n")

    net.addLink(h1, s1, intfName2='s1-eth1', params2={'ip': '10.0.0.1/24'})
    # net.addLink(ap1, r0, intfName1='ap1-eth2', intfName2='r0-eth1', use_hfsc=True, params1={'ip': '192.168.2.1/24'}, params2={'ip': '192.168.2.2/24'})
    # net.addLink(ap2, r1, intfName1='ap2-eth2', intfName2='r1-eth1', use_hfsc=True, params1={'ip': '172.16.0.1/12'}, params2={'ip': '172.16.0.2/12'})
    net.addLink(ap1, r0, intfName1='ap1-eth2', intfName2='r0-eth1', params2={'ip': '192.168.2.2/24'})
    net.addLink(ap2, r1, intfName1='ap2-eth2', intfName2='r1-eth1', params2={'ip': '172.16.0.2/12'})

    net.addLink(s1, r0, intfName1='s1-eth2', intfName2='r0-eth2', params1={'ip': '10.0.1.1/24'}, params2={'ip': '10.0.1.2/24'})
    net.addLink(s1, r1, intfName1='s1-eth3', intfName2='r1-eth2', params1={'ip': '10.0.2.1/24'}, params2={'ip': '10.0.2.2/24'})
    
    info("*** Starting network\n")
    # if '-p' not in args:
    #     net.plotGraph(max_x = 100, max_y = 100)

    if args.mdl == 'mob1':
        net.setMobilityModel(time=0, model='TruncatedLevyWalk', max_x=100, max_y=100, seed=20, min_v=0.5, max_v=1, velocity=(0.5, 1.), FL_MAX=200., alpha=0.5, variance=1.)
    elif args.mdl == 'mob2':
        net.setMobilityModel(time=0, model='GaussMarkov', max_x=100, max_y=100, seed=20, min_v=0.2, max_v=0.5, velocity=(0.2, 0.5), FL_MAX=200., alpha=0.5, variance=2.)
    elif args.mdl == 'mob3':
        net.setMobilityModel(time=0, model='TimeVariantCommunity', max_x=100, max_y=100, velocity=(1., 2.), FL_MAX=200., alpha=0.5, variance=4.)

    net.build()
    # CLI(net)

    for i, sta in enumerate(stations, start=3):
        configClient(sta, i)
        # CLI(net)

    h1.cmd('ip route add default via 10.0.0.1')
    s1.cmd('ip route add default via 10.0.1.2')
    s1.cmd('ip route add 192.168.2.0/24 via 10.0.1.2 dev s1-eth2')
    s1.cmd('ip route add 172.16.0.0/12 via 10.0.2.2 dev s1-eth3')

    r0.cmd('ip route add default via 10.0.1.1')
    r1.cmd('ip route add default via 10.0.2.1')

    c1.start()
    ap1.start([c1])
    ap2.start([c1])
    # time.sleep(10)

    r0.cmd('tc qdisc del dev r0-eth1 root')
    r0.cmd('tc qdisc del dev r0-eth2 root')
    r1.cmd('tc qdisc del dev r1-eth1 root')
    r1.cmd('tc qdisc del dev r1-eth2 root')
    s1.cmd('tc qdisc del dev s1-eth2 root')
    s1.cmd('tc qdisc del dev s1-eth3 root')
    if int(args.var) == 0:
        varrate1 = 1.2
        los1 = 1.5
        r1.cmd('sudo tc qdisc add dev r1-eth2 root handle 1:0 netem delay 15ms {}ms 75% loss {}% 50%'.format(varrate1, los1))
        s1.cmd('sudo tc qdisc add dev s1-eth3 root handle 1:0 netem delay 15ms {}ms 75% loss {}% 50%'.format(varrate1, los1))
        if int(args.var) == 0:
            r0.cmd('sudo tc qdisc add dev r0-eth2 root handle 1:0 netem delay {}ms'.format(args.owd))
            s1.cmd('sudo tc qdisc add dev s1-eth2 root handle 1:0 netem delay {}ms'.format(args.owd))
        else:
            r0.cmd('sudo tc qdisc add dev r0-eth2 root handle 1:0 netem delay {}ms loss {}% 50%'.format(args.owd, args.los))
            s1.cmd('sudo tc qdisc add dev s1-eth2 root handle 1:0 netem delay {}ms loss {}% 50%'.format(args.owd, args.los))  

        r1.cmd('sudo tc qdisc add dev r1-eth2 parent 1:1 handle 10:0 tbf rate 40Mbit burst 50kb limit 500kb')
        r0.cmd('sudo tc qdisc add dev r0-eth2 parent 1:1 handle 10:0 tbf rate {}Mbit burst 50kb limit 500kb'.format(args.bwd))
        s1.cmd('sudo tc qdisc add dev s1-eth3 parent 1:1 handle 10:0 tbf rate 40Mbit burst 50kb limit 500kb')
        s1.cmd('sudo tc qdisc add dev s1-eth2 parent 1:1 handle 10:0 tbf rate {}Mbit burst 50kb limit 500kb'.format(args.bwd))

    else:
        # varrate1 = 15.0 * float(args.var) / 100
        varrate1 = 1.2
        los1 = 1.5
        varrate2 = float(args.owd) * float(args.var) / 100

        r1.cmd('sudo tc qdisc add dev r1-eth2 root handle 1:0 netem delay 15ms {}ms 75% loss {}% 50%'.format(varrate1, los1))
        r0.cmd('sudo tc qdisc add dev r0-eth2 root handle 1:0 netem delay {}ms {}ms 75% loss {}% 50%'.format(args.owd, varrate2, args.los))
        s1.cmd('sudo tc qdisc add dev s1-eth3 root handle 1:0 netem delay 15ms {}ms 75% loss {}% 50%'.format(varrate1, los1))
        s1.cmd('sudo tc qdisc add dev s1-eth2 root handle 1:0 netem delay {}ms {}ms 75% loss {}% 50%'.format(args.owd, varrate2, args.los))

        r1.cmd('sudo tc qdisc add dev r1-eth2 parent 1:1 handle 10:0 tbf rate 40Mbit burst 50kb limit 500kb')
        r0.cmd('sudo tc qdisc add dev r0-eth2 parent 1:1 handle 10:0 tbf rate {}Mbit burst 50kb limit 500kb'.format(args.bwd))
        s1.cmd('sudo tc qdisc add dev s1-eth3 parent 1:1 handle 10:0 tbf rate 40Mbit burst 50kb limit 500kb')
        s1.cmd('sudo tc qdisc add dev s1-eth2 parent 1:1 handle 10:0 tbf rate {}Mbit burst 50kb limit 500kb'.format(args.bwd))

        # r0.cmd('sudo tc qdisc add dev r0-eth2 parent 1:1 handle 10:0 hfsc sc rate 40Mbit')
        # r1.cmd('sudo tc qdisc add dev r1-eth2 parent 1:1 handle 10:0 hfsc sc rate {}Mbit'.format(args.bwd))
        # s1.cmd('sudo tc qdisc add dev s1-eth2 parent 1:1 handle 10:0 hfsc sc rate 40Mbit')
        # s1.cmd('sudo tc qdisc add dev s1-eth3 parent 1:1 handle 10:0 hfsc sc rate {}Mbit'.format(args.bwd))


    # print(args)
    print(server_cmd.format(num=args.clt))
    # print(client_cmd)
    # CLI(net)
    h1.cmd("iperf3 -s > ./logs/iperf_server.log 2>&1 &")
    time.sleep(5)
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
