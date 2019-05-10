package hub

import (
	"fmt"

	"github.com/gorilla/websocket"
)

// Conn is the wrapped connection for a client
type Conn struct {
	WSConn *websocket.Conn
	Topic  string
	Room   string
}

// SendJSON sends a message to the client connection
func (c Conn) SendJSON(event string, payload interface{}) {
	c.WSConn.WriteJSON(coreMessage{
		Topic:   c.Topic,
		Room:    c.Room,
		Event:   event,
		Payload: payload,
	})
}

// GetChannel gets the fully named channel in the form of "<topic>:<room>"
func (c Conn) GetChannel() string {
	return fmt.Sprintf("%s:%s", c.Topic, c.Room)
}
