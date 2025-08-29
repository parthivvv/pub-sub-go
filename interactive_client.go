package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type ClientMessage struct {
	Type      string   `json:"type"`
	Topic     string   `json:"topic,omitempty"`
	Message   *Message `json:"message,omitempty"`
	ClientID  string   `json:"client_id,omitempty"`
	LastN     int      `json:"last_n,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

type Message struct {
	ID      string      `json:"id"`
	Payload interface{} `json:"payload"`
}

type ServerMessage struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id,omitempty"`
	Topic     string     `json:"topic,omitempty"`
	Message   *Message   `json:"message,omitempty"`
	Error     *ErrorInfo `json:"error,omitempty"`
	Status    string     `json:"status,omitempty"`
	Msg       string     `json:"msg,omitempty"`
	TS        time.Time  `json:"ts"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run interactive_client.go <client_id>")
		os.Exit(1)
	}

	clientID := os.Args[1]
	serverURL := "ws://localhost:8080/ws"

	// Connect to WebSocket server
	c, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	fmt.Printf("Connected to %s as client %s\n", serverURL, clientID)
	fmt.Println("Commands:")
	fmt.Println("  sub <topic> [last_n] - Subscribe to topic")
	fmt.Println("  unsub <topic> - Unsubscribe from topic")
	fmt.Println("  pub <topic> <message_id> <payload> - Publish message")
	fmt.Println("  ping - Send ping")
	fmt.Println("  quit - Exit")
	fmt.Println()

	// Handle incoming messages
	go func() {
		for {
			var msg ServerMessage
			err := c.ReadJSON(&msg)
			if err != nil {
				log.Println("read:", err)
				return
			}

			switch msg.Type {
			case "ack":
				fmt.Printf("✓ ACK [%s]: %s on topic %s\n", msg.RequestID, msg.Status, msg.Topic)
			case "event":
				fmt.Printf("📨 EVENT on %s: %s - %v\n", msg.Topic, msg.Message.ID, msg.Message.Payload)
			case "error":
				fmt.Printf("❌ ERROR [%s]: %s - %s\n", msg.RequestID, msg.Error.Code, msg.Error.Message)
			case "pong":
				fmt.Printf("🏓 PONG [%s]\n", msg.RequestID)
			case "info":
				fmt.Printf("ℹ️  INFO on %s: %s\n", msg.Topic, msg.Msg)
			default:
				fmt.Printf("Unknown message type: %s\n", msg.Type)
			}
		}
	}()

	// Handle user input
	scanner := bufio.NewScanner(os.Stdin)
	requestCounter := 1

	// Handle Ctrl+C gracefully
	c_interrupt := make(chan os.Signal, 1)
	signal.Notify(c_interrupt, os.Interrupt)

	go func() {
		<-c_interrupt
		fmt.Println("\nClosing connection...")
		c.Close()
		os.Exit(0)
	}()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		requestID := fmt.Sprintf("req-%d", requestCounter)
		requestCounter++

		switch parts[0] {
		case "sub", "subscribe":
			if len(parts) < 2 {
				fmt.Println("Usage: sub <topic> [last_n]")
				continue
			}
			lastN := 0
			if len(parts) >= 3 {
				fmt.Sscanf(parts[2], "%d", &lastN)
			}

			msg := ClientMessage{
				Type:      "subscribe",
				Topic:     parts[1],
				ClientID:  clientID,
				LastN:     lastN,
				RequestID: requestID,
			}
			c.WriteJSON(msg)

		case "unsub", "unsubscribe":
			if len(parts) < 2 {
				fmt.Println("Usage: unsub <topic>")
				continue
			}

			msg := ClientMessage{
				Type:      "unsubscribe",
				Topic:     parts[1],
				ClientID:  clientID,
				RequestID: requestID,
			}
			c.WriteJSON(msg)

		case "pub", "publish":
			if len(parts) < 4 {
				fmt.Println("Usage: pub <topic> <message_id> <payload>")
				continue
			}

			// Join remaining parts as payload
			payload := strings.Join(parts[3:], " ")

			msg := ClientMessage{
				Type:  "publish",
				Topic: parts[1],
				Message: &Message{
					ID:      parts[2],
					Payload: payload,
				},
				RequestID: requestID,
			}
			c.WriteJSON(msg)

		case "ping":
			msg := ClientMessage{
				Type:      "ping",
				RequestID: requestID,
			}
			c.WriteJSON(msg)

		case "quit", "exit":
			return

		default:
			fmt.Println("Unknown command. Available: sub, unsub, pub, ping, quit")
		}
	}
}
