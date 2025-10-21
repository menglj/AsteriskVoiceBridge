#!/bin/bash

# AI Assistant System Environment Variables
# Copy this file and modify according to your setup

# Proxy settings (if needed)
export http_proxy=http://192.168.1.91:7890
export https_proxy=http://192.168.1.91:7890

# Google Cloud settings
export USE_GOOGLE_STT_TTS=true
export STT_LANGUAGE=en-US
export TTS_LANGUAGE=cmn-CN
export TRANSLATE_SOURCE_LANGUAGE=en-US
export TRANSLATE_TARGET_LANGUAGE=zh-CN
export GOOGLE_APPLICATION_CREDENTIALS=/usr/src/extended-acumen-472-fd9.json
export GOOGLE_STT_HEARTBEAT_INTERVAL=5s

# OpenAI settings (if using OpenAI)
export OPENAI_API_TOKEN="sk-AvAP"

# Operating mode
export OPMODE=text

# Redis settings for AI Assistant
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=""
export REDIS_DB=0

# Agent call timeout (seconds)
export AGENT_CALL_TIMEOUT=30

# Agent URI format (PJSIP, SIP, LOCAL, PJSIP_LOCAL, OPENSIPS, PJSIP_OPENSIPS, PJSIP_TRUNK)
export AGENT_URI_FORMAT="PJSIP_TRUNK" # Use trunk format for OpenSIPS proxy routing

# PJSIP trunk/proxy name for dialing via trunk, e.g., router01
export AGENT_PJSIP_TRUNK="router01"

# Example usage:
# 1. Copy this file: cp env.example.sh env.sh
# 2. Modify the values according to your setup
# 3. Source the file: source env.sh
# 4. Run the application: go run .
