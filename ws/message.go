package ws

const (
	MessageTypeAnswer    = "answer"
	MessageTypeCandidate = "candidate"
	MessageTypeOffer     = "offer"
	MessageTypeInfo      = "info"
	MessageTypeChat      = "chat"
)

type WebsocketMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

type ChatMessage struct {
	Nick    string `json:"nick"`
	Message string `json:"message"`
}
