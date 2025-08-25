# vproxy

`vproxy` is a Go package designed to provide a simple and efficient proxy layer for voice bot applications. It enables seamless routing and management of audio streams and signaling between clients and backend services.

## Features

- Lightweight proxy for voice data and signaling
- Easy integration with Asterisk and other telephony systems
- Converts an rtp stream to audio chunks (StreamTo)
- Converts audio chunks to an rtp stream (StreamFrom)
- Minimal dependencies
- Supports ulaw and linear 16

## Installation

```bash
go get github.com/asterisk/AsteriskVoiceBridge/vproxy
```

## Usage

```go
import "github.com/asterisk/AsteriskVoiceBridge/vproxy"

func main() {
    udpProxy, ok := vproxy.CreateRTPPProxy(DestMediaAddress, DestMediaPort, SrcMediaAddress, SrcMediaPort)
	if !ok {
		return ok
	}

    err := c.udpproxy.StreamTo(io.Writer)
    if err != nil {
        // handle error
    }

    udpproxy.StreamFrom(io.Reader, signalStreamDone, wakefile, filerevived, deadfile)
    udpproxy.PauseAudio()
    udpproxy.ResumeAudio()

    udpProxy.Stop()
}
```

## API

- `GetRTP_MODE()`: Get operating mode, ulaw or slin16
- `GetRTP_PAYLOAD_SIZE`: Gets payload size
- `CreateRTPPProxy(dstMediaAddress string, dstMediaPort int, srcMediaAddress string, srcMediaPort int) (*IOProxy, bool)`: Create an rtp proxy instance
- `Stop()`: Stop the proxy
- `Disengage()`: Disengage streaming
- `Engage()`: Engage streaming
- `(p *IOProxy) StreamTo(w io.Writer) error`: Stream to io.Writer from src address and port
- `(p *IOProxy) StreamFrom(r io.Reader, streamdone chan bool, wakefile chan bool, filerevived chan bool, deadf chan bool) error`: Stream from io.Writer to dest address and port
- `PauseAudio()`: Pause Audio
- `ResumeAudio()`: Resume Audio
- `StopAudio()`: Stop Audio
- `Echo()`: Test Audio Path by echoing from src to dest

