package api

import (
	"encoding/json"
	"log"
	"net/http"
	"restaurant-os/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// allowedWsOrigins is the whitelist for WebSocket connections (mirrors CORS in main.go)
var allowedWsOrigins = map[string]bool{
	"https://faloodaandco.web.app":  true,
	"https://faloodaandco.co.uk":    true,
	"https://www.faloodaandco.co.uk": true,
	"http://localhost:5173":          true,
	"http://localhost:3000":          true,
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return allowedWsOrigins[origin]
	},
}

// Client represents a single KDS tablet connected to the WebSocket
type Client struct {
	Hub     *Hub
	Conn    *websocket.Conn
	StoreID uuid.UUID // Ensures Azmoz Kitchen only sees Azmoz orders
	Send    chan []byte
}

// Hub maintains the set of active KDS tablets and broadcasts orders to the correct store
type Hub struct {
	// Registered clients mapped by StoreID for high-efficiency targeted dispatch
	Rooms      map[uuid.UUID]map[*Client]bool
	Broadcast  chan models.InternalOrder
	Register   chan *Client
	Unregister chan *Client
}

// NewHub initializes the central dispatch system for all 4 branches
func NewHub() *Hub {
	return &Hub{
		Rooms:      make(map[uuid.UUID]map[*Client]bool),
		Broadcast:  make(chan models.InternalOrder),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

// Run starts the infinite loop waiting for new connections or orders to broadcast
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if h.Rooms[client.StoreID] == nil {
				h.Rooms[client.StoreID] = make(map[*Client]bool)
			}
			h.Rooms[client.StoreID][client] = true
			log.Printf("KDS Tablet connected to Store %s", client.StoreID)

		case client := <-h.Unregister:
			if _, ok := h.Rooms[client.StoreID][client]; ok {
				delete(h.Rooms[client.StoreID], client)
				close(client.Send)
			}

		case order := <-h.Broadcast:
			// High-efficiency routing: Send the JSON payload only to the specific store's kitchen screens!
			if clients, ok := h.Rooms[order.StoreID]; ok {
				orderJSON, _ := json.Marshal(order)

				for client := range clients {
					select {
					case client.Send <- orderJSON:
					default:
						close(client.Send)
						delete(h.Rooms[order.StoreID], client)
					}
				}
			}
		}
	}
}

// ServeWs handles websocket requests from the KDS React frontend
func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	storeIDStr := r.URL.Query().Get("storeId")
	storeID, err := uuid.Parse(storeIDStr)
	if err != nil {
		http.Error(w, "Invalid storeId parameter missing", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("KDS Upgrade Error:", err)
		return
	}

	client := &Client{
		Hub:     h,
		Conn:    conn,
		StoreID: storeID,
		Send:    make(chan []byte, 256),
	}

	client.Hub.Register <- client

	// Allow Go to write the updates to this tablet
	go client.writePump()
}

func (c *Client) writePump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()
	for {
		message, ok := <-c.Send
		if !ok {
			c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		w, err := c.Conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		w.Write(message)
		if err := w.Close(); err != nil {
			return
		}
	}
}
