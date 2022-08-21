package video

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/DexterLB/mpvipc"
	uuid "github.com/satori/go.uuid"
)

type VideoPlayer interface {
	StartVideo(string) error
	Pause() error
	Play() error
	Seek(int) error
	Progress() (int, error)
	IsPlaying() bool
	Listener() chan Event
}

type Event struct {
	Name  string
	Value interface{}
}

type MPVPlayer struct {
	isPlaying     bool
	filename      string
	socket        string
	conn          *mpvipc.Connection
	receivedEvent string
	mu            *sync.Mutex
}

func NewMPVPlayer(filename string) VideoPlayer {
	u := uuid.NewV4()
	socket := filepath.Join("/tmp", u.String())

	go func() {
		cmd := exec.Command("mpv", "--idle", "--input-unix-socket="+socket)
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	}()

	time.Sleep(5 * time.Second)

	conn := mpvipc.NewConnection(socket)
	err := conn.Open()
	if err != nil {
		log.Fatal(err)
	}

	return &MPVPlayer{
		filename: filename,
		socket:   socket,
		conn:     conn,
		mu:       &sync.Mutex{},
	}
}

func (m *MPVPlayer) StartVideo(video string) error {
	if len(video) == 0 && len(m.filename) == 0 {
		return errors.New("No filename provided")
	}

	if len(video) == 0 {
		video = m.filename
	}

	_, err := m.conn.Call("loadfile", video, "replace")
	if err != nil {
		log.Println(err)
		return err
	}
	err = m.conn.Set("pause", false)
	if err != nil {
		log.Println(err)
		return err
	}
	m.setReceivedEvent("unpause")
	m.isPlaying = true
	return nil
}

func (m *MPVPlayer) Pause() error {
	err := m.conn.Set("pause", true)
	if err != nil {
		log.Println(err)
		return err
	}
	m.setReceivedEvent("pause")
	m.isPlaying = false
	return nil
}

func (m *MPVPlayer) Play() error {
	err := m.conn.Set("pause", false)
	if err != nil {
		log.Println(err)
		return err
	}
	m.setReceivedEvent("unpause")
	m.isPlaying = true
	return nil
}

func (m *MPVPlayer) Seek(seekTo int) error {
	_, err := m.conn.Call("seek", seekTo, "absolute")
	if err != nil {
		log.Println(err)
		return err
	}
	m.setReceivedEvent("seek")
	return nil
}

func (m *MPVPlayer) Progress() (int, error) {
	val, err := m.conn.Get("time-pos")
	if err != nil {
		log.Println(err)
		return -1, err
	}
	return int(val.(float64)), nil
}

func (m *MPVPlayer) IsPlaying() bool {
	return m.isPlaying
}

func (m *MPVPlayer) Listener() chan Event {
	e := make(chan Event)

	go func(e chan Event) {
		events, stopListening := m.conn.NewEventListener()
		go func() {
			m.conn.WaitUntilClosed()
			stopListening <- struct{}{}
		}()

		for event := range events {
			var currEvent Event
			currEvent.Name = event.Name
			if event.Name == "seek" {
				val, err := m.Progress()
				if err != nil {
					log.Println(err)
				}
				currEvent.Value = val
			}
			if currEvent.Name != m.receivedEvent {
				m.setReceivedEvent("")
				e <- currEvent
			}
		}
	}(e)

	return e
}

func (m *MPVPlayer) setReceivedEvent(ev string) {
	m.mu.Lock()
	m.receivedEvent = ev
	m.mu.Unlock()
}
