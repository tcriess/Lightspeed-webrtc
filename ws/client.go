package ws

import (
	"encoding/json"
	"log"
	"time"

	"github.com/folkengine/goname"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	Send chan []byte

	// webRTC peer connection
	PeerConnection *webrtc.PeerConnection

	// mockup user name
	username string
}

func NewClient(hub *Hub, conn *websocket.Conn, webrtcConn *webrtc.PeerConnection) *Client {
	name := goname.New(goname.FantasyMap).FirstLast()
	return &Client{
		hub:            hub,
		conn:           conn,
		Send:           make(chan []byte),
		PeerConnection: webrtcConn,
		username:       name,
	}
}

// ReadLoop pumps messages from the websocket connection to the hub.
//
// The application runs ReadLoop in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) ReadLoop() {
	defer func() {
		c.hub.Unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	message := &WebsocketMessage{}
	for {
		// _, message, err := c.conn.ReadMessage()
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("could not read message: %s", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println("ws closed unexpected")
			}
			return
		}

		err = json.Unmarshal(raw, &message)
		if err != nil {
			log.Printf("could not unmarshal ws message: %s", err)
			return
		}

		switch message.Event {
		case MessageTypeChat:
			chatMsg := struct{Message string `json:"message"`
				Nick string `json:"nick"`}{}
			err = json.Unmarshal([]byte(message.Data), &chatMsg)
			if err != nil {
				log.Printf("could not unmarshal chat message")
				return
			}
			if chatMsg.Nick == "" {
				chatMsg.Nick = c.username
				newChatMsg, err := json.Marshal(chatMsg)
				if err != nil {
					log.Printf("could not marshal chat message")
					return
				}
				message.Data = string(newChatMsg)
			}
			raw, err = json.Marshal(message)
			if err != nil {
				log.Printf("could not marshal chat message")
				return
			}
			c.hub.Broadcast <- raw

		case MessageTypeCandidate:
			candidate := webrtc.ICECandidateInit{}
			if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
				log.Printf("could not unmarshal candidate msg: %s", err)
				return
			}

			if err := c.PeerConnection.AddICECandidate(candidate); err != nil {
				log.Printf("could not add ice candidate: %s", err)
				return
			}

		case MessageTypeAnswer:
			answer := webrtc.SessionDescription{}
			if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
				log.Printf("could not unmarshal answer msg: %s", err)
				return
			}

			if err := c.PeerConnection.SetRemoteDescription(answer); err != nil {
				log.Printf("could not set remote description: %s", err)
				return
			}
		}
	}
}

// WriteLoop pumps messages from the hub to the websocket connection.
//
// A goroutine running WriteLoop is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) WriteLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued messages to the current websocket message.
			n := len(c.Send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				message = <-c.Send
				_, _ = w.Write(message)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
