package hub

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type CoreMessage struct {
	Topic   string      `json:"topic"`
	Channel string      `json:"channel"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

type StatusPayload struct {
	Status string `json:"description,omitempty"`
	Error  string `json:"error,omitempty"`
}

type TopicHandler interface {
	Join(conn *websocket.Conn) error
	Receive(conn *websocket.Conn, event string, payload interface{})
	Unjoin(conn *websocket.Conn)
}

type Hub struct {
	broadcast chan CoreMessage

	topicHandlers map[string]TopicHandler

	clients map[*websocket.Conn][]string
}

func New() (h *Hub) {
	h = &Hub{
		clients:       make(map[*websocket.Conn][]string),
		topicHandlers: make(map[string]TopicHandler),
	}

	go h.startBroadcaster()
	go h.runPinger()

	return
}

func (h *Hub) AddTopicHandler(topic string, handler TopicHandler) {
	h.topicHandlers[topic] = handler
}

func (h *Hub) runPinger() {
	for {
		for conn := range h.clients {
			conn.WriteMessage(websocket.PingMessage, []byte{})
		}
		time.Sleep(pingPeriod)
	}
}

func (h Hub) startBroadcaster() {
	for {
		msg := <-h.broadcast
		for conn := range h.clients {
			err := conn.WriteJSON(msg)
			if err != nil {
				log.Println("[ERR] could not write message:", err)
				h.disconnect(conn)
			}
		}
	}
}

func (h *Hub) Handler(w http.ResponseWriter, r *http.Request) {
	// Upgrade the connection to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer h.disconnect(ws)

	// Add to the list of clients
	h.clients[ws] = []string{}

	// Setup handlers and timeouts
	ws.SetReadLimit(maxMessageSize)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Enter main event loop
	for {
		// Read the message
		var msg CoreMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			// Log errors if not expected message
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Println("[ERR] error reading json:", err)
				break
			}
			continue
		}

		// Check that the topic for the message exists
		if h.topicHandlers[msg.Topic] == nil {
			ws.WriteJSON(CoreMessage{
				Topic:   msg.Topic,
				Channel: msg.Channel,
				Event:   "not_found",
				Payload: StatusPayload{Error: "topic not found"},
			})
			continue
		}

		mergedChanTopic := msg.Topic + ":" + msg.Channel

		if msg.Event == "join" {
			// TODO: make sure the user is not already in the channel

			if err := h.topicHandlers[msg.Topic].Join(ws); err != nil {
				ws.WriteJSON(CoreMessage{
					Topic:   msg.Topic,
					Channel: msg.Channel,
					Event:   "join_failed",
					Payload: StatusPayload{Error: err.Error()},
				})
				continue
			}

			h.clients[ws] = append(h.clients[ws], mergedChanTopic)

			ws.WriteJSON(CoreMessage{
				Topic:   msg.Topic,
				Channel: msg.Channel,
				Event:   "joined",
				Payload: StatusPayload{Status: "success"},
			})
		} else if h.clientAllowedTopic(ws, mergedChanTopic) {
			h.topicHandlers[msg.Topic].Receive(ws, msg.Event, msg.Payload)
		} else {
			ws.WriteJSON(CoreMessage{
				Topic:   msg.Topic,
				Channel: msg.Channel,
				Event:   "not_joined",
				Payload: StatusPayload{Error: "topic not joined"},
			})
		}
	}
}

func (h Hub) clientAllowedTopic(conn *websocket.Conn, checkTopic string) bool {
	for _, topicName := range h.clients[conn] {
		if checkTopic == topicName {
			return true
		}
	}

	return false
}

func (h *Hub) disconnect(ws *websocket.Conn) {
	log.Println("disconnecting client...")
	for _, topic := range h.clients[ws] {
		h.topicHandlers[topic].Unjoin(ws)
	}

	ws.Close()

	delete(h.clients, ws)
}
