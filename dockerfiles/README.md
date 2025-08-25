# Docker Image and Scripts

This folder contains the Docker image configuration and scripts to build and run the image.

## Prerequisites

- Docker is installed.
- Docker network named `my-net` exists:

    ```sh
    docker network create -d bridge my-net
    ```

## Usage

### 1. Build the Docker Image

```sh
./buildAVai-Deb12Image.sh
```

### 2. Run an Instance

Replace `<name>` with your desired container name:

```sh
./runAVai-Deb12.sh <name>
```
Note: the `OPENAI_API_TOKEN` and `DG_API_TOKEN` environment variables need to be set before running the `runAVai-Deb12.sh` script.

### 3. Connect to Asterisk

Register one of the following SIP extensions to Asterisk inside the running container `localhost:5060`:

- `1000/onethousand`
- `1001/onethousandone`
- `1002/onethousandtwo`

Once registered, dial `613` from your registered extension to reach the example IVR.
