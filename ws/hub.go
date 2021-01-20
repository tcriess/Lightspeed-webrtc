package ws

import (
	"container/ring"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	maxMessageSize  = 4096
	pongWait        = 2 * time.Minute
	pingPeriod      = time.Minute
	writeWait       = 10 * time.Second
	chatHistorySize = 10
)

type Info struct {
	NoConnections int `json:"no_connections"`
}

type Hub struct {
	// Registered clients.
	clients map[*Client]struct{}

	// Broadcast messages to all clients.
	Broadcast chan []byte

	// Register a new client to the hub.
	Register chan *Client

	// Unregister a client from the hub.
	Unregister chan *Client

	// keep the chat history in a ring buffer
	chatHistory                      chan ChatMessage
	chatHistoryStart, chatHistoryEnd *ring.Ring
}

func NewHub() *Hub {
	chatHistory := ring.New(chatHistorySize)
	return &Hub{
		clients:          make(map[*Client]struct{}),
		Broadcast:        make(chan []byte),
		Register:         make(chan *Client),
		Unregister:       make(chan *Client),
		chatHistory:      make(chan ChatMessage, 100),
		chatHistoryStart: chatHistory,
		chatHistoryEnd:   chatHistory,
	}
}

// NoClients returns the number of clients registered
func (h *Hub) NoClients() int {
	return len(h.clients)
}

func sendToClient(client *Client, message []byte, wg *sync.WaitGroup) {
	defer wg.Done()
	client.Send <- message
}

// Run is the main hub event loop handling register, unregister and broadcast events.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.clients[client] = struct{}{}
		case client := <-h.Unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.PeerConnection.Close()
				client.conn.Close()
				close(client.Send)
				go h.SendInfo(h.GetInfo()) // this way the number of clients does not change between calling the goroutine and executing it
			}
		case message := <-h.Broadcast:
			var wg sync.WaitGroup
			for client := range h.clients {
				wg.Add(1)
				go sendToClient(client, message, &wg)
			}
			wg.Wait()
		case chatMessage := <-h.chatHistory:
			h.chatHistoryEnd.Value = chatMessage
			h.chatHistoryEnd = h.chatHistoryEnd.Next()
			if h.chatHistoryEnd == h.chatHistoryStart {
				h.chatHistoryStart = h.chatHistoryStart.Next()
			}
		}
	}
}

func (h *Hub) GetInfo() Info {
	return Info{
		NoConnections: h.NoClients(),
	}
}

func (h *Hub) GetChatHistory() []ChatMessage {
	chatHistory := make([]ChatMessage, 0)
	current := h.chatHistoryStart
	for ; current != h.chatHistoryEnd ; current = current.Next() {
		chatHistory = append(chatHistory, current.Value.(ChatMessage))
	}
	return chatHistory
}



// SendInfo broadcasts hub statistics to all clients.
func (h *Hub) SendInfo(info Info) {
	i, err := json.Marshal(info)
	if err != nil {
		log.Printf("could not marshal ws info: %s", err)
	}
	if msg, err := json.Marshal(WebsocketMessage{
		Event: MessageTypeInfo,
		Data:  i,
	}); err == nil {
		h.Broadcast <- msg
	} else {
		log.Printf("could not marshal ws message: %s", err)
	}
}
