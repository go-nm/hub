package hub

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Conn is the wrapped connection for a client
type Conn struct {
	WS       *websocket.Conn
	Channels map[string]bool
	InitData map[string]interface{}
	mux      sync.Mutex
}

// SendJSON sends a message to the client connection
func (c *Conn) SendJSON(topic, room, event string, payload interface{}) {
	c.mux.Lock()
	c.WS.WriteJSON(coreMessage{
		Topic:   topic,
		Room:    room,
		Event:   event,
		Payload: payload,
	})
	c.mux.Unlock()
}
