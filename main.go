package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cvhariharan/videosync/video"
	"github.com/pion/webrtc/v3"
)

const compress = false

var (
	version = "dev"
	commit  = "none"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var videoName, turnServer, username, credential string
	var host, versionCmd bool

	baseDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	flag.StringVar(&videoName, "video", "", "Video location or URL")
	flag.BoolVar(&host, "host", false, "Start as host")
	flag.StringVar(&turnServer, "turn", "", "TURN server")
	flag.StringVar(&username, "username", "", "TURN server username")
	flag.StringVar(&credential, "password", "", "TURN server password")
	flag.StringVar(&baseDir, "basedir", baseDir, "Base directory for videos. Default is current working directory")
	flag.BoolVar(&versionCmd, "version", false, "Shows the current version")
	flag.Parse()

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{
					"stun:stun.l.google.com:19302",
					"stun:stun1.l.google.com:19302",
					"stun:stun2.l.google.com:19302",
					"stun:stun3.l.google.com:19302",
					"stun:stun4.l.google.com:19302",
				},
			},
		},
	}

	if turnServer != "" && username != "" && credential != "" {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:           []string{turnServer},
			Username:       username,
			Credential:     credential,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}

	if videoName == "" && host {
		log.Fatal("video cannot be empty")
	}

	if versionCmd {
		fmt.Printf("Version: %s Commit: %s\n", version, commit)
	}

	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatal("Could not initiate peer: ", err)
	}

	player := video.NewMPVPlayer("")

	var datachannel *webrtc.DataChannel
	if host {
		datachannel, err = peerConnection.CreateDataChannel("video-sync-channel", nil)
		if err != nil {
			panic(err)
		}

		offer, err := peerConnection.CreateOffer(nil)
		if err != nil {
			log.Fatal(err)
		}

		err = peerConnection.SetLocalDescription(offer)
		if err != nil {
			panic(err)
		}

		peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				fmt.Println("Offer: ", Encode(peerConnection.LocalDescription()))
			}
		})

		answer := webrtc.SessionDescription{}
		reader := bufio.NewReader(os.Stdin)
		str, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		Decode(strings.ReplaceAll(str, "\n", ""), &answer)

		err = peerConnection.SetRemoteDescription(answer)
		if err != nil {
			panic(err)
		}

		datachannel.OnOpen(func() {
			err = datachannel.SendText("ECHO")
			if err != nil {
				panic(err)
			}

			// Send events
			go SendEvents(datachannel, player, host)

			// Sync every 5 seconds
			if host {
				go Sync(datachannel, player)
			}
		})

		datachannel.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Println("Created datachannel")
			Execute(datachannel, msg, player, baseDir, videoName, host)

		})
	}

	// For peer
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Println("Created datachannel")
		// Send events
		go SendEvents(d, player, host)

		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			Execute(d, msg, player, baseDir, videoName, host)
		})
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	if !host {
		fmt.Println("Offer:")
		offer := webrtc.SessionDescription{}
		reader := bufio.NewReader(os.Stdin)
		str, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		Decode(strings.ReplaceAll(str, "\n", ""), &offer)

		// Set the remote SessionDescription
		err = peerConnection.SetRemoteDescription(offer)
		if err != nil {
			panic(err)
		}

		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			panic(err)
		}

		gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

		err = peerConnection.SetLocalDescription(answer)
		if err != nil {
			panic(err)
		}

		<-gatherComplete

		fmt.Println("Answer: ", Encode(answer))
	}

	select {}
}

func Execute(d *webrtc.DataChannel, msg webrtc.DataChannelMessage, player video.VideoPlayer, baseDir, videoName string, host bool) {
	fmt.Println(string(msg.Data))

	if string(msg.Data) == "ECHO" {
		if host {
			go func() {
				err := player.StartVideo(filepath.Join(baseDir, videoName))
				if err != nil {
					log.Println(err)
				}
			}()
			return
		}
		d.SendText("ECHO")
	}

	command := strings.Split(string(msg.Data), ";")
	if len(command) != 2 {
		return
	}

	switch string(command[0]) {
	case "[VIDEO]":
		filename := command[1]
		u, err := url.Parse(filename)
		if err != nil || u.Scheme == "" || u.Host == "" {
			// Not a URL
			filename = filepath.Join(baseDir, filename)
		}
		go func() {
			err := player.StartVideo(filename)
			if err != nil {
				log.Println(err)
			}
		}()

	case "[SEEK]":
		seekStr := command[1]
		seek, err := strconv.Atoi(seekStr)
		if err != nil {
			log.Println(err)
		}

		current, err := player.Progress()
		if err != nil {
			log.Println(err)
		}

		if math.Abs(float64(current)-float64(seek)) > 1 {
			err = player.Seek(seek)
			if err != nil {
				log.Println(err)
			}
		}

	case "[PAUSE]":
		err := player.Pause()
		if err != nil {
			log.Println(err)
		}

	case "[UNPAUSE]":
		err := player.Play()
		if err != nil {
			log.Println(err)
		}
	}
}

func SendEvents(datachannel *webrtc.DataChannel, player video.VideoPlayer, host bool) {
	events := player.Listener()
	for {
		event := <-events
		switch event.Name {
		case "pause":
			datachannel.SendText("[PAUSE];")
		case "unpause":
			datachannel.SendText("[UNPAUSE];")
		case "seek":
			if event.Value.(int) > 0 {
				datachannel.SendText("[SEEK];" + strconv.Itoa(event.Value.(int)))
			}
		case "start-file":
			if host {
				datachannel.SendText("[VIDEO];" + event.Value.(string))
			}
		}
	}
}

func Sync(datachannel *webrtc.DataChannel, player video.VideoPlayer) {
	for {
		if !player.IsPlaying() {
			continue
		}
		seek, err := player.Progress()
		if err != nil {
			log.Println(err)
		}
		if seek != -1 {
			datachannel.SendText("[SEEK];" + strconv.Itoa(seek))
		}
		time.Sleep(5 * time.Second)
	}
}

// Encode encodes the input in base64
// It can optionally zip the input before encoding
func Encode(obj interface{}) string {
	b, err := json.Marshal(obj)
	if err != nil {
		fmt.Println(err)
	}

	if compress {
		b = zip(b)
	}

	return base64.StdEncoding.EncodeToString(b)
}

// Decode decodes the input from base64
// It can optionally unzip the input after decoding
func Decode(in string, obj interface{}) {
	b, err := base64.StdEncoding.DecodeString(strings.Replace(in, "\n", "", -1))
	if err != nil {
		fmt.Println(err)
	}

	if compress {
		b = unzip(b)
	}

	err = json.Unmarshal(b, obj)
	if err != nil {
		fmt.Println(err)
	}
}

func zip(in []byte) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	_, err := gz.Write(in)
	if err != nil {
		fmt.Println(err)
	}
	err = gz.Flush()
	if err != nil {
		fmt.Println(err)
	}
	err = gz.Close()
	if err != nil {
		fmt.Println(err)
	}
	return b.Bytes()
}

func unzip(in []byte) []byte {
	var b bytes.Buffer
	_, err := b.Write(in)
	if err != nil {
		fmt.Println(err)
	}
	r, err := gzip.NewReader(&b)
	if err != nil {
		fmt.Println(err)
	}
	res, err := ioutil.ReadAll(r)
	if err != nil {
		fmt.Println(err)
	}
	return res
}
