#!/bin/bash
#!/bin/sleep
#!/bin/sh

sudo mn -c

cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/quic-go ; go build ; go install ./...
cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/simulationFileServer
pwd
cp /home/"$(whoami)"/go/bin/example ./serverMPQUIC
cp /home/"$(whoami)"/go/bin/client_benchmarker ./clientMPQUIC

# sudo rm ./logs/*
# sudo rm ./output/result-wireless/*
for i in {3..5}
do
    cp "clientMPQUIC" "clientMPQUIC$i"
done

echo -e "0.3\n0.5\n0.5\n0.4\n0.4\n0.0" > ./config/qsat 
echo -e "12\n0.3" > ./config/sac 

declare -a mdlArr=("none") #("none" "dif1" "dif2" "mobi" "mob2" "mob3" "mob1")
declare -a numArr=("1") #("1" "3" "5")
declare -a filArr=("4MB") 
declare -a bgrArr=("0")
declare -a frqArr=("0")
declare -a bwdArr=("5") #bandwidth
declare -a owdArr=("15")  #one-way delay
declare -a varArr=("0")
declare -a losArr=("5")

# declare -a bwdArr=("5" "10" "15" "20" "25") #bandwidth
# declare -a owdArr=("5" "10" "15" "20" "25")  #one-way delay

# declare -a varArr=("0" "2" "4" "6" "8" "10" "12" "14" "16") #variation delay
# declare -a losArr=("0" "0.3" "0.6" "0.9" "1.2" "1.5" "1.8" "2.1" "2.4" "2.7" "3.0") #pkt loss

# declare -a schArr=("sacrx" "sacmulti" "random" "rtt" "peek" "multiclients"  "sacmultiJoinCC")
# declare -a schArr=("peek" "rtt" "fuzzyqsat")
declare -a schArr=("sac-cc")
# declare -a schArr=("rtt")
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
                                        sudo rm ./logs/*
                                        sudo -E env "PATH=$PATH" python wifi_scenario_singlepath.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
                                        sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
                                        sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
                                        sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
                                        # sudo mv ./logs/result4.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result4.csv   
                                        # sudo mv ./logs/result5.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result5.csv   
                                        # sudo mv ./logs/result6.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result6.csv   
                                        # sudo mv ./logs/result7.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result7.csv   
                                        # sudo mv ./logs/result8.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result8.csv   
                                        # sudo mv ./logs/result9.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result9.csv   
                                        # sudo mv ./logs/result10.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result10.csv   
                                        # sudo mv ./logs/result11.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result11.csv   
                                        # sudo mv ./logs/state.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state.csv   
                                        # sudo mv ./logs/state_dis.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state_dis.csv   
                                        # sudo mv ./logs/reward.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-reward.csv   
                                        # sudo mv ./logs/action.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-action.csv   
                                        # sudo mv ./logs/statistic.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-statistic.csv   

                                        sudo mv ./logs/server-flask.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-flask.logs  
                                        sudo mv ./logs/training_history.pdf ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training.pdf 
                                        sudo mv ./logs/training_history_3.pdf ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_3.pdf 
                                        # sudo mv ./logs/training_history_4.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_4.png 
                                        # sudo mv ./logs/training_history_5.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_5.png 
                                        # sudo mv ./logs/training_history_6.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_6.png 

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

# declare -a schArr=("path1")
# # declare -a varArr=("10") #variation delay
# # declare -a losArr=("1") #pkt loss 
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
#                                         sudo rm ./logs/*
#                                         sudo -E env "PATH=$PATH" python wifi_scenario_singlepath.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
#                                         sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
#                                         sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
#                                         sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
#                                         # sudo mv ./logs/result4.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result4.csv   
#                                         # sudo mv ./logs/result5.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result5.csv   
#                                         # sudo mv ./logs/result6.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result6.csv   
#                                         # sudo mv ./logs/result7.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result7.csv   
#                                         # sudo mv ./logs/result8.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result8.csv   
#                                         # sudo mv ./logs/result9.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result9.csv   
#                                         # sudo mv ./logs/result10.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result10.csv   
#                                         # sudo mv ./logs/result11.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result11.csv   
#                                         # sudo mv ./logs/state.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state.csv   
#                                         # sudo mv ./logs/state_dis.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state_dis.csv   
#                                         # sudo mv ./logs/reward.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-reward.csv   
#                                         # sudo mv ./logs/action.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-action.csv   
#                                         sudo mv ./logs/statistic.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-statistic.csv   

#                                         sudo mv ./logs/server-flask.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-flask.logs  
#                                         sudo mv ./logs/training_history.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training.png 
#                                         sudo mv ./logs/training_history_3.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_3.png 
#                                         # sudo mv ./logs/training_history_4.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_4.png 
#                                         # sudo mv ./logs/training_history_5.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_5.png 
#                                         # sudo mv ./logs/training_history_6.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_6.png 

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

# declare -a mdlArr=("none" "mobi") #("none" "dif1" "dif2" "mobi")
# declare -a varArr=("20") #variation delay
# declare -a losArr=("2") #pkt loss 
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
#                                         sudo rm ./logs/*
#                                         sudo -E env "PATH=$PATH" python wifi_scenario3.py --model ${mdl} --client ${num} --file ${fil} --background ${bgr} --frequency ${frq} --bandwidth ${bwd} --delay ${owd} --variation ${var} --loss ${los} --scheduler ${sch} 
#                                         sudo mv ./logs/server.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-server.logs
#                                         sudo mv ./logs/client.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-client.logs
#                                         sudo mv ./logs/result3.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result3.csv   
#                                         # sudo mv ./logs/result4.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result4.csv   
#                                         # sudo mv ./logs/result5.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result5.csv   
#                                         # sudo mv ./logs/result6.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result6.csv   
#                                         # sudo mv ./logs/result7.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result7.csv   
#                                         # sudo mv ./logs/result8.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result8.csv   
#                                         # sudo mv ./logs/result9.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result9.csv   
#                                         # sudo mv ./logs/result10.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result10.csv   
#                                         # sudo mv ./logs/result11.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-result11.csv   
#                                         # sudo mv ./logs/state.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state.csv   
#                                         # sudo mv ./logs/state_dis.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-state_dis.csv   
#                                         # sudo mv ./logs/reward.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-reward.csv   
#                                         # sudo mv ./logs/action.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-action.csv   
#                                         sudo mv ./logs/statistic.csv ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-statistic.csv   

#                                         sudo mv ./logs/server-flask.logs ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-flask.logs  
#                                         sudo mv ./logs/training_history.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training.png 
#                                         sudo mv ./logs/training_history_3.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_3.png 
#                                         # sudo mv ./logs/training_history_4.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_4.png 
#                                         # sudo mv ./logs/training_history_5.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_5.png 
#                                         # sudo mv ./logs/training_history_6.png ./output/${mdl}-${num}-${fil}-${bgr}-${frq}-${bwd}-${owd}-${var}-${los}-${sch}-training_6.png 

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