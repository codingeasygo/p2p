#!/bin/bash
##############################
#####Setting Environments#####
echo "Setting Environments"
set -e
export cpwd=$(pwd)
export LD_LIBRARY_PATH=/usr/local/lib:/usr/lib
export PATH=$PATH:$GOPATH/bin:$HOME/bin:$GOROOT/bin
output=build

#### Package ####
srv_name=p2pc
srv_ver=0.1.0
srv_out=$output/$srv_name
rm -rf $srv_out
mkdir -p $srv_out
##build normal
echo "Build $srv_name normal executor..."
go build -o $srv_out/$srv_name github.com/codingeasygo/p2p/p2pc
cp -f p2pc-install.sh $srv_out
cp -f p2pc.service $srv_out
cp -f p2pc.json $srv_out

###
cd $output
rm -f $srv_name-$srv_ver-$(uname).zip
zip -r $srv_name-$srv_ver-$(uname).zip $srv_name
cd ../
echo "Package $srv_name done..."
