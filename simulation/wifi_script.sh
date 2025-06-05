#!/bin/bash
#!/bin/sleep
#!/bin/sh

cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/quic-go ; go build ; go install ./...
cd /home/"$(whoami)"/go/src/github.com/lucas-clemente/example-network
pwd
cp /home/"$(whoami)"/go/bin/example .
cp /home/"$(whoami)"/go/bin/client_benchmarker .

sudo rm ./logs/*
sudo rm ./output/result-wireless/*


file_path="www/listwebsite.txt"

#initial: array stores web list
declare -a webArr

#read file and store to arr
while IFS= read -r line || [ -n "$line" ]; do
    line=$(echo "$line" | sed 's/^https:\/\///')
    webArr+=("$line")
done < "$file_path"

#echo "Print arr store folder website:"
for web in "${webArr[@]}"; do
    echo "$web"
done

# declare -a schArr=("LowLatency" "ECF" "SA-ECF" "QSAT")
# declare -a stmArr=("RR" "WRR" "FCFS" "NII" "SWRR")
# declare -a brsArr=("safari" "firefox" "chrome")
# declare -a mdlArr=("none" "TruncatedLevyWalk")
declare -a schArr=("LowLatency")
declare -a stmArr=("RR" "WRR" "NII")
declare -a brsArr=("safari")
declare -a mdlArr=("none")

for web in "${webArr[@]}"
do
    for sch in "${schArr[@]}"
    do
        for stm in "${stmArr[@]}"
        do 
            for brs in "${brsArr[@]}"
            do 
                for mdl in "${mdlArr[@]}"
                do 
                    echo "$sch-$stm-$brs-$mdl"
                    sudo python wifi_scenario.py --website ${web} --scheduler ${sch} --stream ${stm} --model ${mdl} --client 1 --browser ${brs}
                    sudo mv ./logs/server.logs ./output/result-wifi/${web}-server-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/client.logs ./output/result-wifi/${web}-client-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/data-time.csv ./output/result-wifi/${web}-time-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/data-byte.csv ./output/result-wifi/${web}-byte-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/server-detail.logs ./output/result-wifi/${web}-detail-${sch}-${stm}-${brs}-${mdl}.logs
                    sleep 10
                done
            done
        done
    done
done

sudo mn -c
declare -a schArr=("LowLatency")
declare -a stmArr=("WRR" "NII" "SWRR")
declare -a brsArr=("firefox")
declare -a mdlArr=("none")

for web in "${webArr[@]}"
do
    for sch in "${schArr[@]}"
    do
        for stm in "${stmArr[@]}"
        do 
            for brs in "${brsArr[@]}"
            do 
                for mdl in "${mdlArr[@]}"
                do 
                    echo "$sch-$stm-$brs-$mdl"
                    sudo python wifi_scenario.py --website ${web} --scheduler ${sch} --stream ${stm} --model ${mdl} --client 1 --browser ${brs}
                    sudo mv ./logs/server.logs ./output/result-wireless/${web}-server-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/client.logs ./output/result-wireless/${web}-client-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/data-time.csv ./output/result-wireless/${web}-time-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/data-byte.csv ./output/result-wireless/${web}-byte-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/server-detail.logs ./output/result-wireless/${web}-detail-${sch}-${stm}-${brs}-${mdl}.logs
                    sleep 10
                done
            done
        done
    done
done

sudo mn -c
declare -a schArr=("LowLatency")
declare -a stmArr=("FCFS")
declare -a brsArr=("chrome")
declare -a mdlArr=("none")

for web in "${webArr[@]}"
do
    for sch in "${schArr[@]}"
    do
        for stm in "${stmArr[@]}"
        do 
            for brs in "${brsArr[@]}"
            do 
                for mdl in "${mdlArr[@]}"
                do 
                    echo "$sch-$stm-$brs-$mdl"
                    sudo python wifi_scenario.py --website ${web} --scheduler ${sch} --stream ${stm} --model ${mdl} --client 1 --browser ${brs}
                    sudo mv ./logs/server.logs ./output/result-wifi/${web}-server-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/client.logs ./output/result-wifi/${web}-client-${sch}-${stm}-${brs}-${mdl}.logs
                    sudo mv ./logs/data-time.csv ./output/result-wifi/${web}-time-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/data-byte.csv ./output/result-wifi/${web}-byte-${sch}-${stm}-${brs}-${mdl}.csv   
                    sudo mv ./logs/server-detail.logs ./output/result-wifi/${web}-detail-${sch}-${stm}-${brs}-${mdl}.logs
                    sleep 10
                done
            done
        done
    done
done