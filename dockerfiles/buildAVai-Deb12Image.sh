#!/bin/bash

echo "Building Debian 12 Image for asterisk voice ai";

tar -czvf Deb12-AsteriskVai/asteriskvoicebot.tar.gz ../ariman ../deepgram ../IVR-Demo ../rclocal ../voiceai ../voicebot ../vproxy ../main.go ../go.mod ../go.sum

docker build -t asterisk-vai/deb12-asteriskvai Deb12-AsteriskVai