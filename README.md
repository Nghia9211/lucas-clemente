requirement:
- ubuntu 22.04
- go 1.20
- tcconfig (sudo pip install tcconfig)
- mininet-wifi 
        git clone https://github.com/intrig-unicamp/mininet-wifi
        cd mininet-wifi
        mininet-wifi$ sudo util/install.sh -Wlnfv6

serverdrl:
- flask (pip install Flask)
- pytorch (pip3 install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu118)