package main

import (
	"github.com/gorilla/websocket"
	"log"
	"net/url"
)

func main() {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws"}
	log.Printf("Connecting to %s to setup test data", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	// Publish 5 test messages to create history
	for i := 1; i <= 5; i++ {
		publishMsg := map[string]interface{}{
			"type":  "publish",
			"topic": "orders",
			"message": map[string]interface{}{
				"id": "msg-" + string(rune(i+'0')),
				"payload": map[string]interface{}{
					"order_id": "ORD-" + string(rune(i+'0')),
					"amount":   99.5 * float64(i),
					"currency": "USD",
					"status":   "pending",
				},
			},
			"request_id": "setup-req-" + string(rune(i+'0')),
		}

		log.Printf("Publishing message %d...", i)
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
		log.Printf("✓ Message %d published successfully", i)
	}

	log.Println("✓ Test data setup complete! Now you can test with wscat.")
	log.Println("Use: wscat -c ws://localhost:8080/ws")
}
