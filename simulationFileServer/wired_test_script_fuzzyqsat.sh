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
declare -a frqArr=("0.4") #change to epsilon
# declare -a frqArr=("0.1" "0.2" "0.3" "0.4" "0.5" "0.6" "0.7" "0.8" "0.9" "1.0") #change to epsilon
declare -a bwdArr=("30") #bandwidth
declare -a owdArr=("10") #one-way delay
declare -a varArr=("10") #variation delay
declare -a losArr=("1") #pkt loss 
# declare -a schArr=("sacrx" "sacmulti" "random" "rtt" "peek" "multiclients"  "sacmultiJoinCC")
# declare -a schArr=("rtt")
declare -a schArr=("fuzzyqsat")

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
                    echo -e "0.3\n0.5\n0.5\n0.4\n${frq}\n1.0" > ./config/qsat 
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
                                        sudo -E env "PATH=$PATH" python wired_scenario.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
                                        sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
                                        sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
                                        sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
   
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

# declare -a varArr=("10") #variation delay
# declare -a losArr=("1") #pkt loss 
# # declare -a schArr=("sacrx" "sacmulti" "random" "rtt" "peek" "multiclients"  "sacmultiJoinCC")
# # declare -a schArr=("rtt")
# declare -a schArr=("fuzzyqsat")

# for mdl in "${mdlArr[@]}"
# do 
#     for num in "${numArr[@]}"
#     do 
#         for fil in "${filArr[@]}"
#         do
#             for bgr in "${bgrArr[@]}"
#             do 
#                 for frq in "${frqArr[@]}"
#                 do
#                     echo -e "0.3\n0.5\n0.5\n0.4\n${frq}\n1.0" > ./config/qsat 
#                     for bwd in "${bwdArr[@]}"
#                     do 
#                         for owd in "${owdArr[@]}"
#                         do
#                             for var in "${varArr[@]}"
#                             do
#                                 for los in "${losArr[@]}"
#                                 do 
#                                     for sch in "${schArr[@]}"
#                                     do
#                                         echo "$mdl-$num-$fil-$bgr-$frq-$bwd-$owd-$var-$los-$sch"
#                                         sudo -E env "PATH=$PATH" python wired_scenario.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
#                                         sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
#                                         sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
#                                         sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
   
#                                         sudo mn -c
#                                         sleep 10
#                                     done
#                                 done
#                             done
#                         done
#                     done
#                 done
#             done
#         done
#     done
# done