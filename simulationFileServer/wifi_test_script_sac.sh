#!/bin/bash
#!/bin/sleep
#!/bin/sh

sudo mn -c

cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/quic-go ; go build ; go install ./...
cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/simulationFileServer
pwd
cp /home/"$(whoami)"/go/bin/example ./serverMPQUIC
cp /home/"$(whoami)"/go/bin/client_benchmarker ./clientMPQUIC

sudo rm ./logs/*
# sudo rm ./output/result-wireless/*
for i in {3..18}
do
    cp "clientMPQUIC" "clientMPQUIC$i"
done

declare -a mdlArr=("none") #("none" "dif1" "dif2" "mobi")
declare -a numArr=("1") #("1" "4" "8")
declare -a filArr=("2MB") 
declare -a bgrArr=("0")
# declare -a frqArr=("1" "2" "4" "6" "8" "10" "12" "14" "16" "18") #change to k
declare -a frqArr=("100") #change to k
declare -a bwdArr=("30") #bandwidth
declare -a owdArr=("10") #one-way delay
declare -a varArr=("20") #variation delay
declare -a losArr=("1") #pkt loss 
# declare -a schArr=("sacrx" "sacmulti" "random" "rtt" "peek" "multiclients"  "sacmultiJoinCC")
# declare -a schArr=("rtt")
declare -a schArr=("sac-cc")

for mdl in "${mdlArr[@]}"
do 
    for num in "${numArr[@]}"
    do 
        for fil in "${filArr[@]}"
        do
            for bgr in "${bgrArr[@]}"
            do 
                for frq in "${frqArr[@]}"
                do
                    echo -e "${frq}\n0.2" > ./config/sac 
                    for bwd in "${bwdArr[@]}"
                    do 
                        for owd in "${owdArr[@]}"
                        do
                            for var in "${varArr[@]}"
                            do
                                for los in "${losArr[@]}"
                                do 
                                    for sch in "${schArr[@]}"
                                    do
                                        echo "$mdl-$num-$fil-$bgr-$frq-$bwd-$owd-$var-$los-$sch"
                                        sudo -E env "PATH=$PATH" python wifi_scenario2.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
                                        sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
                                        sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
                                        sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
                                        
                                        sudo mv ./logs/statistic.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-statistic.csv   
                                        sudo mv ./logs/server-flask.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-flask.logs  
                                        sudo mv ./logs/training_history.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training.png 
                                        sudo mv ./logs/training_history_3.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_3.png 
                                       
                                        sudo mn -c
                                        sleep 10
                                    done
                                done
                            done
                        done
                    done
                done
            done
        done
    done
done