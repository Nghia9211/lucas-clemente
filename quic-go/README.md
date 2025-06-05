# Welcome to Scheduling in MPQUIC!

This is a source code about scheduling in MPQUIC, including: RR, minRTT, BLEST, ECF and Peekaboo. We would like to thank Quentin De Coninck for his contribution to MPQUIC. 

## Overall

We compile the mpquic code first to generate the executable file for the mpquic client and server. We then put the mpquic client and server into the docker and control the experiment from the Jupyter notebook. 

## Hello World
Download environment: https://drive.google.com/file/d/1vZIiXYLLVfWeINMcY9revN_hjZ7Em5XR/view?usp=sharing
cd ~/go/src/github.com/lucas-clemente/quic-go and replace by this repo

## Compile the mpquic code 

- Prepare the Ubuntu server LTS (18.04). Mpquic is written in Go which can run on any OS for the real-world tests (we tested on both Ubuntu and macOS). However, for the emulated scenario, since the Mininet is used, we have to choose Ubuntu. If you have got the Ubuntu machine nearby, please make sure the version is ubuntu server LTS (18.04). Otherwise, please prepare the virtual machine under Ubuntu server LTS (18.04).
- Intall Go. Please install go1.10.3, as this is the version we test. You can download [go1.10.3.linux-amd64.tar.gz](https://dl.google.com/go/go1.10.3.linux-amd64.tar.gz) by clicking this link. And then follow this [instruction](https://golang.org/doc/install) to install the go. 
- Compile Go. We do observe that the interdependency between different software packages can cause the compiling issue as the time goes and version varies. Thus, we wrap all the packages in the provided go folder, so the reader can directly compile it as it is.  First, copy the provided go folder to the home folder of the Ubuntu machine. Then, 
> $ sudo apt install libhdf5-dev
> $ cd ~/go/src/github.com/lucas-clemente/quic-go
> $ go build
> $ go install ./...
- Rename and copy the generated client and server bins to the docker folder. The generated bins are in the `$HOME/go/bin` and rename `client_benchmarker` and `example` to `client_mt` and `server_mt`, respectively. The docker folder is at the RL_dev folder we provide, and, more specifically, at `RL_dev/quic/`.

## Run the experiment within docker  and controlled by Jupyter notebook

- Install docker.
- Go to the `RL_dev/` folder.
- Build the docker container. 
> $ docker build -t peekaboo .
- Run the docker container.(prefer remote server by PuTTy, you can easy get token key to sign in Jupyter Notebook) 
> $ docker run --privileged=true --cap-add=ALL -v /lib/modules:/lib/modules -p 8888:8888 -it --add-host quic.clemente.io:10.0.0.20 peekaboo
  (sometime, add option: --net=host )
- Access Jupyter Notebook from the running output. ([IP Server]:8888)

