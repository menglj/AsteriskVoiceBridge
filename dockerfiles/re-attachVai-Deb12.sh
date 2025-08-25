#!/bin/bash

echo "Attempting to re-attach to container: $1 from AVai-Deb12 Image";

echo "Starting Asterisk..."
docker exec -w /usr/src/asterisk $1 asterisk -vvvvg
sleep 10
echo "Starting Webhook Controller..."
docker exec -d -w /usr/src/asteriskvoicebot/IVR-Demo $1 python3 -m http.server 8989
echo "Starting Chatbot..."
docker exec -it -e DG_API_TOKEN=$DG_API_TOKEN -e OPENAI_API_TOKEN=$OPENAI_API_TOKEN -w /usr/src/asteriskvoicebot $1 go run .