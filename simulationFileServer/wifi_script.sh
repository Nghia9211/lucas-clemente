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

declare -a mdlArr=("none")
declare -a numArr=("1")
declare -a filArr=("1MB") 
declare -a bwdArr=("30") #bandwidth
declare -a owdArr=("10") #one-way delay
declare -a varArr=("0") #variation delay
declare -a losArr=("0") #pkt loss 
declare -a schArr=("qsat" "sac")

for mdl in "${mdlArr[@]}"
do 
    for num in "${numArr[@]}"
    do 
        for fil in "${filArr[@]}"
        do
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
                                echo "$mdl-$num-$fil-$bwd-$owd-$var-$los-$sch"
                                sudo -E env "PATH=$PATH" python wifi_scenario.py --model ${mdl} --client ${num} --file ${fil} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
                                sudo mv ./logs/server.logs ./output/result-wireless/${mdl}-${num}-${fil}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
                                sudo mv ./logs/client.logs ./output/result-wireless/${mdl}-${num}-${fil}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
                                sudo mv ./logs/result.csv ./output/result-wireless/${mdl}-${num}-${fil}-${bwd}-${owd}-${var}-${los}-${sch}-result.csv   
                                sudo mv ./logs/server-flask.logs ./output/result-wireless/${mdl}-${num}-${fil}-${bwd}-${owd}-${var}-${los}-${sch}-flask.logs  
                                sudo mv ./logs/training_history.png ./output/result-wireless/${mdl}-${num}-${fil}-${bwd}-${owd}-${var}-${los}-${sch}-traininghistory.png 
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
