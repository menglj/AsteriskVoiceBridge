# ⚠️ Demonstration Only

**This repository is for demonstration purposes only and should not be used for production under any circumstances.**

---

## Module Overview

### 1. `voicebot`
The core module that orchestrates voice interactions. It manages audio input/output, session state, and coordinates between the other modules to deliver voice-based features.

### 2. `ariman`
Handles telephony integration, connecting the voicebot to Asterisk via ARI. It manages call events and sets up the audio streams.

### 3. `deepgram`
Provides speech-to-text (STT) and text-to-speech (TTS) capabilities. Audio streams from `vproxy` are sent to and from `deepgram` for transcription and recitation, enabling the voicebot to understand spoken input and respond accordingly.

### 4. `rclocal`
Responsible for building the service by providing tools and instructions for the AI and executing choices based on it's output.

### 5. `voiceai`
Implements natural language understanding and response generation via OpenAI Realtime. Receives transcribed text from `deepgram`, processes user intent, and generates appropriate replies for the voicebot to deliver.

### 6. `vproxy`
Acts as a voice proxy layer, managing RTP traffic between modules and external services.

---

## Module Interaction Summary

- **ariman** receives calls and streams audio to **deepgram** via **vproxy** for transcription.
- **deepgram** returns text to **voiceai**, which interprets and generates responses.
- **voiceai** sends responses back to **deepgram** for delivery to the caller via **vproxy**.
- **voicebot** manages the overall session and coordinates with **rclocal** which queries the IVR-Demo http server for external AI instruction and function parsing.

---

## Installing the Application
The application is installed via the follwoing command:
`go install .`

---

## Running the Application
This application requires an OpenAI and Deepgram API key in order to use those services.  These are set with the following environment variables:

- **OPENAI_API_TOKEN** - Your OpenAI API token
- **DG_API_TOKEN** - Your Deepgram API token

Once these are set, the application is run via:
`go run .`

---
## Example IVR
The application contains a folder [IVR-Demo](./IVR-Demo/) which defines a sample IVR.  Inside this folder is a script [ivr-runner.sh](./IVR-Demo/ivr-runner.sh) that will launch a simple python http server that hosts all files within the folder.  The rclocal package is hard coded to perform a GET on `http://localhost:8989/ivr-root.json` which contains the base configuration for the IVR.  The [IVR-Demo](./IVR-Demo/) also includes the IVR leaves that the AI service may pick from.  Each leaf json file transfers the call to a specific point in the included [Asterisk Dialplan](./dockerfiles/Deb12-AsteriskVai/ast-config/extensions.conf).

---

## Docker Container
This application contains a folder [dockerfiles](./dockerfiles/) with two scripts: [buildAVai-Deb12Image.sh](./dockerfiles/buildAVai-Deb12Image.sh) which will build and package a Debian 12 Docker image containing Asterisk, the mini http server and the voicebridge application and [runAVai-Deb12.sh](./dockerfiles/runAVai-Deb12.sh) which will run a named instance. See [dockerfiles/README.md](./dockerfiles/README.md) for more information.

---

## License

This project is licensed under the **GNU Affero General Public License (AGPL)**.

See [LICENSE](./LICENSE) for details.

## AI Translator (Dual Topology)

- Set env:
  - `TRANSLATOR_DUAL_TOPOLOGY=true`
  - `TRANSLATOR_AGENT_EXT=1100`
  - `USE_GOOGLE_STT_TTS=true`
  - `CUSTOMER_STT_LANG=en-US`
  - `AGENT_STT_LANG=cmn-CN`
  - `CUSTOMER_TO_AGENT_TRANSLATE=en-US->zh-CN`
  - `AGENT_TO_CUSTOMER_TRANSLATE=zh-CN->en-US`
  - `GOOGLE_APPLICATION_CREDENTIALS=/path/creds.json`
  - proxy env as needed (`http_proxy`, `https_proxy`)

- Dial: customer extension calls 1100; when agent answers, ARI builds dual bridges and translation starts automatically.

- Notes:
  - STT is paused during TTS playback with interrupt detection enabled.
  - Both directions run as sub-sessions: `<callid>-c2a` and `<callid>-a2c`.
  - On hangup, sub-sessions and RTP proxies are cleaned up.