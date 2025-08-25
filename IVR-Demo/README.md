# IVR-Demo Folder Documentation

## Overview

The `IVR-Demo` folder contains json configuration files for demonstrating an Interactive Voice Response (IVR) system. This demo includes configuration files and a runner script to launch the IVR application.

## Folder Contents

- **\*.json**: A set of json files the voicebot that defines the IVR structure. ivr-root.json contains the initial configuration pulled by the voicebot which references the other json files that define the 'leaves' of the IVR.
- **ivr-runner.sh**: Shell script to start the IVR demo application.

## How to Run the IVR Demo

1. **Ensure Prerequisites**:
    - Make sure you have the necessary dependencies installed (python3)
    - Verify that you have execution permissions for `ivr-runner.sh`:
      ```sh
      chmod +x ivr-runner.sh
      ```

2. **Start the IVR Demo**:
    Run the following command from within the `IVR-Demo` directory:
    ```sh
    ./ivr-runner.sh
    ```