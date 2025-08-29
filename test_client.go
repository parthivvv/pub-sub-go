package main

import (
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	// First, let's publish a few messages to have some history
	for i := 1; i <= 3; i++ {
		publishMsg := map[string]interface{}{
			"type":  "publish",
			"topic": "orders",
			"message": map[string]interface{}{
				"id": "msg-" + string(rune(i+'0')),
				"payload": map[string]interface{}{
					"order_id": "ORD-" + string(rune(i+'0')),
					"amount":   99.5 * float64(i),
					"currency": "USD",
				},
			},
			"request_id": "pub-req-" + string(rune(i+'0')),
		}

		if err := c.WriteJSON(publishMsg); err != nil {
			log.Println("write error:", err)
			return
		}

		// Read the ack
		var response map[string]interface{}
		if err := c.ReadJSON(&response); err != nil {
			log.Println("read error:", err)
			return
		}
		log.Printf("Published message %d, received: %v", i, response)

		time.Sleep(100 * time.Millisecond)
	}

	// Now test the subscribe with last_n parameter (the problematic case)
	subscribeMsg := map[string]interface{}{
		"type":       "subscribe",
		"topic":      "orders",
		"client_id":  "test-client-1",
		"last_n":     5,
		"request_id": "req-001",
	}

	log.Println("Sending subscribe message with last_n=5...")
	if err := c.WriteJSON(subscribeMsg); err != nil {
		log.Println("write error:", err)
		return
	}

	// Read responses
	for i := 0; i < 10; i++ { // Read up to 10 responses
		var response map[string]interface{}
		if err := c.ReadJSON(&response); err != nil {
			log.Println("read error:", err)
			break
		}

		responseBytes, _ := json.MarshalIndent(response, "", "  ")
		log.Printf("Response %d: %s", i+1, string(responseBytes))
	}
}
