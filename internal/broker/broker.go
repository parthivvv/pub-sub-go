package broker

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Default buffer size for a subscriber's message queue.
	defaultSubscriberBufferSize = 64
	// Default number of messages to store for replay.
	defaultReplayBufferSize = 100
	// Write wait timeout
	writeWait = 10 * time.Second
	// Pong wait timeout
	pongWait = 60 * time.Second
	// Ping period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10
)

// Message represents a single message published to a topic.
type Message struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

// Event represents a message delivered to a subscriber.
type Event struct {
	Type      string     `json:"type"`
	RequestID string     `json:"request_id,omitempty"`
	Topic     string     `json:"topic,omitempty"`
	Message   *Message   `json:"message,omitempty"`
	Error     *ErrorInfo `json:"error,omitempty"`
	Status    string     `json:"status,omitempty"`
	Msg       string     `json:"msg,omitempty"`
	TS        time.Time  `json:"ts"`
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ClientMessage represents incoming client messages
type ClientMessage struct {
	Type      string   `json:"type"`
	Topic     string   `json:"topic,omitempty"`
	Message   *Message `json:"message,omitempty"`
	ClientID  string   `json:"client_id,omitempty"`
	LastN     int      `json:"last_n,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

// Subscriber represents a single client subscribed to a topic.
type Subscriber struct {
	ClientID string
	conn     *websocket.Conn
	send     chan *Event
	mu       sync.Mutex
	topics   map[string]bool // Track subscribed topics
	broker   *Broker
}

// NewSubscriber creates a new subscriber
func NewSubscriber(clientID string, conn *websocket.Conn, broker *Broker) *Subscriber {
	return &Subscriber{
		ClientID: clientID,
		conn:     conn,
		send:     make(chan *Event, defaultSubscriberBufferSize),
		topics:   make(map[string]bool),
		broker:   broker,
	}
}

// Topic manages a set of subscribers and the message history.
type Topic struct {
	name         string
	subscribers  map[string]*Subscriber
	history      []*Message // Ring buffer for message history
	maxHistory   int
	messageCount int64 // Total messages published
	mu           sync.RWMutex
}

// Broker is the central component managing all topics.
type Broker struct {
	topics    map[string]*Topic
	mu        sync.RWMutex
	startTime time.Time
}

// Stats represents broker statistics
type Stats struct {
	Topics map[string]TopicStats `json:"topics"`
}

type TopicStats struct {
	Messages    int64 `json:"messages"`
	Subscribers int   `json:"subscribers"`
}

// HealthInfo represents health information
type HealthInfo struct {
	UptimeSec   int64 `json:"uptime_sec"`
	Topics      int   `json:"topics"`
	Subscribers int   `json:"subscribers"`
}

// TopicsResponse represents the response for listing topics
type TopicsResponse struct {
	Topics []TopicInfo `json:"topics"`
}

type TopicInfo struct {
	Name        string `json:"name"`
	Subscribers int    `json:"subscribers"`
}

// NewBroker creates a new Broker.
func NewBroker() *Broker {
	return &Broker{
		topics:    make(map[string]*Topic),
		startTime: time.Now(),
	}
}

// newTopic creates a new Topic.
func newTopic(name string) *Topic {
	return &Topic{
		name:        name,
		subscribers: make(map[string]*Subscriber),
		history:     make([]*Message, 0, defaultReplayBufferSize),
		maxHistory:  defaultReplayBufferSize,
	}
}

// GetTopic returns an existing topic or creates a new one.
func (b *Broker) GetTopic(name string) *Topic {
	b.mu.RLock()
	topic, ok := b.topics[name]
	b.mu.RUnlock()
	if ok {
		return topic
	}
	// Topic doesn't exist, create it under a write lock.
	b.mu.Lock()
	defer b.mu.Unlock()
	// Double-check if another goroutine created it while we were waiting for the lock.
	if topic, ok := b.topics[name]; ok {
		return topic
	}
	topic = newTopic(name)
	b.topics[name] = topic
	return topic
}

// CreateTopic creates a new topic and returns an error if it already exists.
func (b *Broker) CreateTopic(name string) (*Topic, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.topics[name]; ok {
		return nil, false // Already exists
	}
	topic := newTopic(name)
	b.topics[name] = topic
	return topic, true
}

// DeleteTopic removes a topic and notifies all subscribers
func (b *Broker) DeleteTopic(name string) bool {
	b.mu.Lock()
	topic, exists := b.topics[name]
	if !exists {
		b.mu.Unlock()
		return false
	}
	delete(b.topics, name)
	b.mu.Unlock()

	// Notify all subscribers about topic deletion
	topic.mu.Lock()
	for _, sub := range topic.subscribers {
		event := &Event{
			Type:  "info",
			Topic: name,
			Msg:   "topic_deleted",
			TS:    time.Now(),
		}
		select {
		case sub.send <- event:
		default:
			// Subscriber is slow, close connection
			close(sub.send)
		}
	}
	topic.mu.Unlock()

	return true
}

// ListTopics returns all topics with their subscriber counts
func (b *Broker) ListTopics() TopicsResponse {
	b.mu.RLock()
	defer b.mu.RUnlock()

	topics := make([]TopicInfo, 0, len(b.topics))
	for name, topic := range b.topics {
		topic.mu.RLock()
		subscriberCount := len(topic.subscribers)
		topic.mu.RUnlock()

		topics = append(topics, TopicInfo{
			Name:        name,
			Subscribers: subscriberCount,
		})
	}

	return TopicsResponse{Topics: topics}
}

// GetHealth returns broker health information
func (b *Broker) GetHealth() HealthInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	totalSubscribers := 0
	for _, topic := range b.topics {
		topic.mu.RLock()
		totalSubscribers += len(topic.subscribers)
		topic.mu.RUnlock()
	}

	return HealthInfo{
		UptimeSec:   int64(time.Since(b.startTime).Seconds()),
		Topics:      len(b.topics),
		Subscribers: totalSubscribers,
	}
}

// GetStats returns detailed broker statistics
func (b *Broker) GetStats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	topicStats := make(map[string]TopicStats)
	for name, topic := range b.topics {
		topic.mu.RLock()
		topicStats[name] = TopicStats{
			Messages:    topic.messageCount,
			Subscribers: len(topic.subscribers),
		}
		topic.mu.RUnlock()
	}

	return Stats{Topics: topicStats}
}

// Subscribe adds a subscriber to a topic
func (t *Topic) Subscribe(sub *Subscriber, lastN int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Add subscriber
	t.subscribers[sub.ClientID] = sub
	sub.topics[t.name] = true

	// Send historical messages if requested
	if lastN > 0 {
		historyLen := len(t.history)
		start := 0
		if lastN < historyLen {
			start = historyLen - lastN
		}

		for i := start; i < historyLen; i++ {
			event := &Event{
				Type:    "event",
				Topic:   t.name,
				Message: t.history[i],
				TS:      time.Now(),
			}
			select {
			case sub.send <- event:
			default:
				// Subscriber is slow, skip historical messages
				log.Printf("Subscriber %s is slow, skipping historical messages", sub.ClientID)
				return
			}
		}
	}
}

// Unsubscribe removes a subscriber from a topic
func (t *Topic) Unsubscribe(clientID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	sub, exists := t.subscribers[clientID]
	if !exists {
		return false
	}

	delete(t.subscribers, clientID)
	delete(sub.topics, t.name)
	return true
}

// Publish sends a message to all subscribers of a topic
func (t *Topic) Publish(msg *Message) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Add to history with ring buffer behavior
	if len(t.history) >= t.maxHistory {
		// Remove oldest message (ring buffer)
		copy(t.history, t.history[1:])
		t.history[len(t.history)-1] = msg
	} else {
		t.history = append(t.history, msg)
	}

	t.messageCount++

	// Create event
	event := &Event{
		Type:    "event",
		Topic:   t.name,
		Message: msg,
		TS:      time.Now(),
	}

	// Send to all subscribers
	for clientID, sub := range t.subscribers {
		select {
		case sub.send <- event:
		default:
			// Subscriber is slow, implement backpressure policy
			log.Printf("Subscriber %s is slow, implementing backpressure", clientID)

			// Send SLOW_CONSUMER error and close connection
			errorEvent := &Event{
				Type: "error",
				Error: &ErrorInfo{
					Code:    "SLOW_CONSUMER",
					Message: "Subscriber queue overflow",
				},
				TS: time.Now(),
			}

			select {
			case sub.send <- errorEvent:
			default:
			}

			// Remove slow subscriber
			delete(t.subscribers, clientID)
			delete(sub.topics, t.name)
			close(sub.send)
		}
	}
}

// HandleClient handles WebSocket client connection
func (s *Subscriber) HandleClient() {
	defer func() {
		s.cleanup()
		s.conn.Close()
	}()

	// Set up ping/pong handling
	s.conn.SetReadDeadline(time.Now().Add(pongWait))
	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start writer goroutine
	go s.writer()

	// Handle incoming messages
	for {
		var msg ClientMessage
		err := s.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		s.handleMessage(&msg)
	}
}

// writer handles outgoing messages to client
func (s *Subscriber) writer() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-s.send:
			s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := s.conn.WriteJSON(event); err != nil {
				log.Printf("Write error: %v", err)
				return
			}

		case <-ticker.C:
			s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming client messages
func (s *Subscriber) handleMessage(msg *ClientMessage) {
	switch msg.Type {
	case "subscribe":
		s.handleSubscribe(msg)
	case "unsubscribe":
		s.handleUnsubscribe(msg)
	case "publish":
		s.handlePublish(msg)
	case "ping":
		s.handlePing(msg)
	default:
		s.sendError(msg.RequestID, "BAD_REQUEST", "Unknown message type")
	}
}

// handleSubscribe processes subscribe requests
func (s *Subscriber) handleSubscribe(msg *ClientMessage) {
	if msg.Topic == "" || msg.ClientID == "" {
		s.sendError(msg.RequestID, "BAD_REQUEST", "topic and client_id are required")
		return
	}

	// Update client ID if provided
	if msg.ClientID != s.ClientID {
		s.ClientID = msg.ClientID
	}

	topic := s.broker.GetTopic(msg.Topic)
	topic.Subscribe(s, msg.LastN)

	s.sendAck(msg.RequestID, msg.Topic, "subscribed")
}

// handleUnsubscribe processes unsubscribe requests
func (s *Subscriber) handleUnsubscribe(msg *ClientMessage) {
	if msg.Topic == "" || msg.ClientID == "" {
		s.sendError(msg.RequestID, "BAD_REQUEST", "topic and client_id are required")
		return
	}

	s.broker.mu.RLock()
	topic, exists := s.broker.topics[msg.Topic]
	s.broker.mu.RUnlock()

	if !exists {
		s.sendError(msg.RequestID, "TOPIC_NOT_FOUND", "Topic does not exist")
		return
	}

	if topic.Unsubscribe(msg.ClientID) {
		s.sendAck(msg.RequestID, msg.Topic, "unsubscribed")
	} else {
		s.sendError(msg.RequestID, "BAD_REQUEST", "Not subscribed to topic")
	}
}

// handlePublish processes publish requests
func (s *Subscriber) handlePublish(msg *ClientMessage) {
	if msg.Topic == "" || msg.Message == nil {
		s.sendError(msg.RequestID, "BAD_REQUEST", "topic and message are required")
		return
	}

	if msg.Message.ID == "" {
		s.sendError(msg.RequestID, "BAD_REQUEST", "message.id must be provided")
		return
	}

	s.broker.mu.RLock()
	topic, exists := s.broker.topics[msg.Topic]
	s.broker.mu.RUnlock()

	if !exists {
		s.sendError(msg.RequestID, "TOPIC_NOT_FOUND", "Topic does not exist")
		return
	}

	topic.Publish(msg.Message)
	s.sendAck(msg.RequestID, msg.Topic, "published")
}

// handlePing processes ping requests
func (s *Subscriber) handlePing(msg *ClientMessage) {
	event := &Event{
		Type:      "pong",
		RequestID: msg.RequestID,
		TS:        time.Now(),
	}

	select {
	case s.send <- event:
	default:
		// Channel full, drop pong
	}
}

// sendAck sends acknowledgment messages
func (s *Subscriber) sendAck(requestID, topic, status string) {
	event := &Event{
		Type:      "ack",
		RequestID: requestID,
		Topic:     topic,
		Status:    status,
		TS:        time.Now(),
	}

	select {
	case s.send <- event:
	default:
		// Channel full, connection will be closed due to slow consumer
	}
}

// sendError sends error messages
func (s *Subscriber) sendError(requestID, code, message string) {
	event := &Event{
		Type:      "error",
		RequestID: requestID,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
		TS: time.Now(),
	}

	select {
	case s.send <- event:
	default:
		// Channel full, connection will be closed due to slow consumer
	}
}

// cleanup removes subscriber from all topics
func (s *Subscriber) cleanup() {
	for topicName := range s.topics {
		s.broker.mu.RLock()
		topic, exists := s.broker.topics[topicName]
		s.broker.mu.RUnlock()

		if exists {
			topic.Unsubscribe(s.ClientID)
		}
	}
}
