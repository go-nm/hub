package hub

import (
  "fmt"

  "github.com/gorilla/websocket"
)

type Conn struct {
	WSConn *websocket.Conn
	Topic  string
	Room   string
}

func (c Conn) SendJSON(event string, payload interface{}) {
	c.WSConn.WriteJSON(CoreMessage{
		Topic:   c.Topic,
		Room:    c.Room,
		Event:   event,
		Payload: payload,
	})
}

func (c Conn) GetChannel() string {
	return fmt.Sprintf("%s:%s", c.Topic, c.Room)
}
