package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"pubsub/internal/broker"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from any origin for demo purposes
		return true
	},
}

type Server struct {
	broker *broker.Broker
}

func NewServer() *Server {
	return &Server{
		broker: broker.NewBroker(),
	}
}

// HTTP Handlers

func (s *Server) createTopicHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Topic name is required", http.StatusBadRequest)
		return
	}

	_, created := s.broker.CreateTopic(req.Name)
	if !created {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Topic already exists",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "created",
		"topic":  req.Name,
	})
}

func (s *Server) deleteTopicHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	topicName := vars["name"]

	if topicName == "" {
		http.Error(w, "Topic name is required", http.StatusBadRequest)
		return
	}

	deleted := s.broker.DeleteTopic(topicName)
	if !deleted {
		http.Error(w, "Topic not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"topic":  topicName,
	})
}

func (s *Server) listTopicsHandler(w http.ResponseWriter, r *http.Request) {
	topics := s.broker.ListTopics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(topics)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	health := s.broker.GetHealth()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats := s.broker.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// WebSocket Handler
func (s *Server) websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Generate a temporary client ID if not provided
	clientID := fmt.Sprintf("client_%d", time.Now().UnixNano())

	subscriber := broker.NewSubscriber(clientID, conn, s.broker)
	log.Printf("New WebSocket connection: %s", clientID)

	// Handle the client in a separate goroutine
	go subscriber.HandleClient()
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for all requests
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight OPTIONS requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging middleware
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	server := NewServer()

	// Create router
	r := mux.NewRouter()

	// Apply middleware
	r.Use(corsMiddleware)
	r.Use(loggingMiddleware)

	// Add a catch-all OPTIONS handler first
	r.Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS headers are already set by middleware
		w.WriteHeader(http.StatusOK)
	})

	// REST API routes
	r.HandleFunc("/topics", server.createTopicHandler).Methods("POST")
	r.HandleFunc("/topics", server.listTopicsHandler).Methods("GET")
	r.HandleFunc("/topics/{name}", server.deleteTopicHandler).Methods("DELETE")
	r.HandleFunc("/health", server.healthHandler).Methods("GET")
	r.HandleFunc("/stats", server.statsHandler).Methods("GET")

	// WebSocket route
	r.HandleFunc("/ws", server.websocketHandler)

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server
	log.Printf("Starting server on port %s", port)
	log.Printf("WebSocket endpoint: ws://localhost:%s/ws", port)
	log.Printf("REST API: http://localhost:%s", port)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	log.Println("Shutting down server...")

	// Implement graceful shutdown
	// Note: In a production system, you'd want to properly close all WebSocket connections
	// and flush pending messages, but for this demo we'll keep it simple

	log.Println("Server stopped")
}
