# videosync

A peer-to-peer video syncing tool. Uses webrtc for communication. No video is transmitted, only the syncing info over a text based protocol. The same video file should be present with both the peers if a local video has to be synced.

Videosync connects to mpv using IPC to play the video. 

## Dependencies
- [mpv](https://mpv.io/)
- yt-dlp (optional: can be used to play online videos with mpv)

## Usage
Currently videosync only supports 1-1 connections.  
Each connection has a host
```bash
#host
videosync -host -video=https://www.youtube.com/watch?v=dQw4w9WgXcQ
```
This will generate a webrtc offer that should be sent to the connecting peer. The peer will simply run videosync and paste the offer when prompted. This will in turn generate an answer which should be sent back to the host.
```bash
#peer
videosync
```
Once the host pastes the answer, the connection will be established and the video will be launched.