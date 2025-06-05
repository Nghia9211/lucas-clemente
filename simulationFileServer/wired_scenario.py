#!/usr/bin/env python

import sys
import time
import argparse
from mininet.log import setLogLevel, info
from mininet.node import OVSController, Node
from mininet.net import Mininet
from mininet.cli import CLI
from mininet.link import TCLink

# Các lệnh điều khiển server
SERVER_CMD = "python ../serverdrl/app.py --client {num} > ./logs/server-flask.logs 2>&1 & ./serverMPQUIC"
CERTPATH = "--certpath ./quic/quic_go_certs"
SCH = "-scheduler %s"
ARGS = "-bind :6121 -www ./www/"
END = ">> ./logs/server.logs 2>&1"

CLIENT_CMD = "./clientMPQUIC{id} -n 1 -t 0 -m -clt {id}"
CLIENT_FIL = "https://10.0.2.2:6121/files/%s-{id}"
CLIENT_END = ">> ./logs/client.logs 2>&1"

# Linux router configuration
class LinuxRouter(Node):
    def config(self, **params):
        super(LinuxRouter, self).config(**params)
        self.cmd('sysctl net.ipv4.ip_forward=1')

    def terminate(self):
        self.cmd('sysctl net.ipv4.ip_forward=0')
        super(LinuxRouter, self).terminate()

def runClient(station, id, client_cmd):
    for i in range(3):
        print(client_cmd.format(id=id))
        station.sendCmd(client_cmd.format(id=id))
        output = station.monitor(timeoutms=30000)
        time.sleep(1)

# Cấu hình client
def configClient(sta, id):
    sta.cmd(f"ifconfig sta{id}-eth0 192.168.2.{id} netmask 255.255.255.0")
    sta.cmd(f"ifconfig sta{id}-eth1 172.16.0.{id} netmask 255.240.0.0")
    sta.cmd(f"ip rule add from 192.168.2.{id}/24 table 1")
    sta.cmd(f"ip rule add from 172.16.0.{id}/12 table 2")
    sta.cmd(f"ip route add default nexthop via 192.168.2.2 dev sta{id}-eth0 weight 1 nexthop via 172.16.0.2 dev sta{id}-eth1 weight 1")
    sta.cmd("ip route add default via 192.168.2.2 table 1")
    sta.cmd("ip route add default via 172.16.0.2 table 2")

# Tạo topology
def topology(args, server_cmd, client_cmd):
    net = Mininet(link=TCLink)

    info("*** Creating nodes\n")
    client = net.addHost('client', ip='10.0.1.2/24', defaultRoute='via 10.0.1.1')
    server = net.addHost('server', ip='10.0.2.2/24', defaultRoute='via 10.0.2.1')

    r1 = net.addHost('r1', cls=LinuxRouter, ip='10.0.0.1/30')
    r2 = net.addHost('r2', cls=LinuxRouter, ip='10.0.0.2/30')
    r3 = net.addHost('r3', cls=LinuxRouter, ip='10.0.0.5/30')
    r4 = net.addHost('r4', cls=LinuxRouter, ip='10.0.0.6/30')
    r5 = net.addHost('r5', cls=LinuxRouter, ip='10.0.0.9/30')

    c1 = net.addController('c1')

    info("*** Associating and Creating links\n")
    net.addLink(r1, r2, intfName1='r1-eth0', intfName2='r2-eth0')
    net.addLink(r3, r4, intfName1='r3-eth0', intfName2='r4-eth0')
    net.addLink(r2, r5, intfName1='r2-eth1', params1={ 'ip' : '10.0.0.9/30' }, intfName2='r5-eth0', params2={ 'ip' : '10.0.0.10/30' })
    net.addLink(r4, r5, intfName1='r4-eth1', params1={ 'ip' : '10.0.0.13/30' }, intfName2='r5-eth1', params2={ 'ip' : '10.0.0.14/30' })

    # client
    net.addLink( client, r1, intfName2='r1-eth1', params2={ 'ip' : '10.0.1.1/24' } )
    net.addLink( client, r3, intfName1='client-eth1', params1={ 'ip' : '10.0.3.2/24' }, intfName2='r3-eth1', params2={ 'ip' : '10.0.3.1/24' } )
    # server
    net.addLink( server, r5, intfName2='r5-eth2', params2={ 'ip' : '10.0.2.1/24' } )

    info("*** Starting network\n")
    #           eth1-r1-eth0        eth0-r2-eth1
    # client -                                      r5-eth2      server
    #           eth1-r3-eth0        eth0-r4-eth1
    # net.build()
    net.start()

    if int(args.var) == 0:
        #configuration r1
        net[ 'r1' ].cmd("ifconfig r1-eth2 10.0.5.1/24")
        net[ 'r1' ].cmd("route add default gw 10.0.0.2")
        net[ 'r1' ].cmd("tc qdisc add dev r1-eth0 root netem limit 1000 rate {0}Mbit".format(args.bwd))
        
        for i in [3, 11]:
            net[ 'r1' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.1.2".format(i))

        #configuration r2
        net[ 'r2' ].cmd("ifconfig r2-eth2 10.0.6.1/24")
        net[ 'r2' ].cmd("route add default gw 10.0.0.1")
        net[ 'r2' ].cmd("ip route add 10.0.2.0/24 via 10.0.0.10 dev r2-eth1")
        net[ 'r2' ].cmd("tc qdisc add dev r2-eth0 root netem limit 67 delay {0}ms rate {1}Mbit".format(args.owd, args.bwd))

        #configuration r3
        net[ 'r3' ].cmd("ifconfig r3-eth2 10.0.7.1/24")
        net[ 'r3' ].cmd("route add default gw 10.0.0.6")
        net[ 'r3' ].cmd("tc qdisc add dev r3-eth0 root netem limit 1000 rate {0}Mbit".format(args.bwd))

        for i in [1, 9, 13]:
            net[ 'r3' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.3.2".format(i))

        #configuration r4
        net[ 'r4' ].cmd("ifconfig r4-eth2 10.0.8.1/24")    
        net[ 'r4' ].cmd("route add default gw 10.0.0.5")
        net[ 'r4' ].cmd("route add -net 10.0.2.0 netmask 255.255.255.0 gw 10.0.0.14")
        net[ 'r4' ].cmd("tc qdisc add dev r4-eth0 root netem limit 67 delay {0}ms rate {1}Mbit".format(args.owd, args.bwd))
    else:
        #configuration r1
        net[ 'r1' ].cmd("ifconfig r1-eth2 10.0.5.1/24")
        net[ 'r1' ].cmd("route add default gw 10.0.0.2")
        # net[ 'r1' ].cmd("tc qdisc add dev r1-eth0 root netem limit 1000 delay {0}ms 3ms 75% loss 0.5 50% rate {1}Mbit".format(args.owd, args.bwd))
        r1.cmd('tc qdisc add dev r1-eth0 root handle 5:0 hfsc default 1')
        r1.cmd('tc class add dev r1-eth0 parent 5:0 classid 5:1 hfsc sc rate 30Mbit')
        r1.cmd('tc qdisc add dev r1-eth0 parent 5:1 netem loss 0.5% 50% delay 10ms 1ms 75%')
        for i in [3, 11]:
            net[ 'r1' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.1.2".format(i))

        #configuration r2
        net[ 'r2' ].cmd("ifconfig r2-eth2 10.0.6.1/24")
        net[ 'r2' ].cmd("route add default gw 10.0.0.1")
        net[ 'r2' ].cmd("ip route add 10.0.2.0/24 via 10.0.0.10 dev r2-eth1")
        # net[ 'r2' ].cmd("tc qdisc add dev r2-eth0 root netem limit 1000 delay {0}ms 3ms 75% loss 0.5 50% rate {1}Mbit".format(args.owd, args.bwd))
        r2.cmd('tc qdisc add dev r2-eth0 root handle 5:0 hfsc default 1')
        r2.cmd('tc class add dev r2-eth0 parent 5:0 classid 5:1 hfsc sc rate 30Mbit')
        r2.cmd('tc qdisc add dev r2-eth0 parent 5:1 netem loss 0.5% 50% delay 10ms 1ms 75%')

        #configuration r3
        net[ 'r3' ].cmd("ifconfig r3-eth2 10.0.7.1/24")
        net[ 'r3' ].cmd("route add default gw 10.0.0.6")
        # net[ 'r3' ].cmd("tc qdisc add dev r3-eth0 root netem limit 1000 delay 15ms 3ms 75% loss 0.5 50% rate 50Mbit")
        r3.cmd('tc qdisc add dev r3-eth0 root handle 5:0 hfsc default 1')
        r3.cmd('tc class add dev r3-eth0 parent 5:0 classid 5:1 hfsc sc rate 55Mbit ul rate 60Mbit')
        r3.cmd('tc qdisc add dev r3-eth0 parent 5:1 netem loss 0.5% 50% delay 15ms 1.5ms 75%')
        for i in [1, 9, 13]:
            net[ 'r3' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.3.2".format(i))

        #configuration r4
        net[ 'r4' ].cmd("ifconfig r4-eth2 10.0.8.1/24")    
        net[ 'r4' ].cmd("route add default gw 10.0.0.5")
        net[ 'r4' ].cmd("route add -net 10.0.2.0 netmask 255.255.255.0 gw 10.0.0.14")
        # net[ 'r4' ].cmd("tc qdisc add dev r4-eth0 root netem limit 1000 delay 15ms 3ms 75% loss 0.5 50% rate 50Mbit")
        r4.cmd('tc qdisc add dev r4-eth0 root handle 5:0 hfsc default 1')
        r4.cmd('tc class add dev r4-eth0 parent 5:0 classid 5:1 hfsc sc rate 55Mbit ul rate 60Mbit')
        r4.cmd('tc qdisc add dev r4-eth0 parent 5:1 netem loss 0.5% 50% delay 15ms 1.5ms 75%')

    #configuration r5
    net[ 'r5' ].cmd("ifconfig r5-eth0 10.0.0.10/30")    
    net[ 'r5' ].cmd("ifconfig r5-eth2 10.0.2.1/24")    
    net[ 'r5' ].cmd("route add default gw 10.0.0.9")

    for i in [1, 9, 13]:
        net[ 'r5' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.0.9".format(i))

    for i in [3, 11]:
        net[ 'r5' ].cmd("route add -net 10.0.{0}.0 netmask 255.255.255.0 gw 10.0.0.13".format(i))

    #configuration client
    # This creates two different routing tables, that we use based on the source-address.
    net[ 'client' ].cmd("ip rule add from 10.0.1.2 table 1")
    net[ 'client' ].cmd("ip rule add from 10.0.3.2 table 2")
    # Configure the two different routing tables
    net[ 'client' ].cmd("ip route add 10.0.1.0/24 dev client-eth0 scope link table 1")
    net[ 'client' ].cmd("ip route add default via 10.0.1.1 dev client-eth0 table 1")

    net[ 'client' ].cmd("ip route add 10.0.3.0/24 dev client-eth1 scope link table 2")
    net[ 'client' ].cmd("ip route add default via 10.0.3.1 dev client-eth1 table 2")
    
    # default route for the selection process of normal internet-traffic
    net[ 'client' ].cmd("ip route add default scope global nexthop via 10.0.1.1 dev client-eth0")

    #configuration server
    # This creates two different routing tables, that we use based on the source-address.
    net[ 'server' ].cmd("ip rule add from 10.0.2.2 table 1")
    
    # Configure the two different routing tables
    net[ 'server' ].cmd("ip route add 10.0.2.0/24 dev server-eth0 scope link table 1")
    net[ 'server' ].cmd("ip route add default via 10.0.2.1 dev server-eth0 table 1")

    # default route for the selection process of normal internet-traffic
    net[ 'server' ].cmd("ip route add default scope global nexthop via 10.0.2.1 dev server-eth0")

    CLI(net)

    server.sendCmd(server_cmd.format(num=args.clt))
    time.sleep(10)

    runClient(client, 3, client_cmd)

    info("*** Stopping network\n")
    net.stop()
    time.sleep(1)

# Hàm thực thi
def do_training(args):
    server_cmd = " ".join([SERVER_CMD, CERTPATH, SCH % args.sch, ARGS, END])
    client_cmd = " ".join([CLIENT_CMD, CLIENT_FIL % args.fil, CLIENT_END])
    setLogLevel('info')
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
