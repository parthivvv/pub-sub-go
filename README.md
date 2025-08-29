# In-Memory Pub/Sub System

A high-performance, thread-safe, in-memory publish-subscribe messaging system built in Go with WebSocket and REST API support.

## Features

- **WebSocket-based Pub/Sub**: Real-time message publishing and subscription over WebSocket connections
- **REST API Management**: HTTP endpoints for topic management and system observability
- **Concurrency Safe**: Thread-safe operations supporting multiple publishers and subscribers
- **Message History & Replay**: Ring buffer implementation for message history with configurable replay
- **Backpressure Handling**: Bounded subscriber queues with slow consumer detection and disconnection
- **Health & Statistics**: Built-in monitoring endpoints for system health and metrics
- **Graceful Shutdown**: Clean shutdown handling with signal management

## Architecture & Design Choices

### Core Components

1. **Broker**: Central message coordinator managing all topics
2. **Topic**: Individual message channels with subscriber management and history
3. **Subscriber**: WebSocket client connection wrapper with message queuing
4. **Message**: Immutable message structure with ID and payload

### Key Design Decisions

#### Ring Buffer for Message History
- **Implementation**: Uses a slice with manual ring buffer behavior
- **Rationale**: Provides O(N) message storage with bounded memory usage (For simplicity)
- **Behavior**: When capacity (100 messages) is reached, oldest messages are overwritten
- **Benefits**: Constant memory footprint, efficient historical message replay

#### Backpressure Policy
- **Policy**: Disconnect slow consumers
- **Implementation**: Non-blocking channel sends with default case handling
- **Trigger**: When subscriber's message queue (64 messages) is full
- **Response**: Send SLOW_CONSUMER error and forcefully disconnect client
- **Rationale**: Prevents memory leaks and protects system performance

#### Concurrency Model
- **Broker Level**: Read-write mutex for topic map operations
- **Topic Level**: Read-write mutex for subscriber map and message history
- **Subscriber Level**: Dedicated goroutines for read/write operations
- **Message Flow**: Lock-free message delivery using Go channels

#### Double-Checked Locking
- **Pattern**: Used in GetTopic() method for lazy topic creation
- **Benefit**: Avoids unnecessary write locks when topic already exists
- **Implementation**: Read lock check → Write lock → Double check → Create

### Memory Management

#### Bounded Queues
- **Subscriber Queue**: 64 messages per subscriber
- **History Buffer**: 100 messages per topic
- **Overflow Handling**: Drop oldest (history) or disconnect (subscriber queue)

#### Resource Cleanup
- **Connection Cleanup**: Automatic subscriber removal on disconnect
- **Topic Cleanup**: Subscribers notified when topics are deleted
- **Goroutine Management**: Proper cleanup prevents goroutine leaks

## API Documentation

### WebSocket Protocol (ws://localhost:8080/ws)

#### Client → Server Messages

##### Subscribe
```json
{
  "type": "subscribe",
  "topic": "orders",
  "client_id": "subscriber1",
  "last_n": 5,
  "request_id": "req-123"
}
```

##### Unsubscribe
```json
{
  "type": "unsubscribe",
  "topic": "orders",
  "client_id": "subscriber1",
  "request_id": "req-124"
}
```

##### Publish
```json
{
  "type": "publish",
  "topic": "orders",
  "message": {
    "id": "msg-001",
    "payload": {"order_id": "ORD-123", "amount": 99.5}
  },
  "request_id": "req-125"
}
```

##### Ping
```json
{
  "type": "ping",
  "request_id": "req-126"
}
```

#### Server → Client Messages

##### Acknowledgment
```json
{
  "type": "ack",
  "request_id": "req-123",
  "topic": "orders",
  "status": "subscribed",
  "ts": "2025-08-29T10:00:00Z"
}
```

##### Event (Published Message)
```json
{
  "type": "event",
  "topic": "orders",
  "message": {
    "id": "msg-001",
    "payload": {"order_id": "ORD-123", "amount": 99.5}
  },
  "ts": "2025-08-29T10:01:00Z"
}
```

##### Error
```json
{
  "type": "error",
  "request_id": "req-127",
  "error": {
    "code": "TOPIC_NOT_FOUND",
    "message": "Topic does not exist"
  },
  "ts": "2025-08-29T10:02:00Z"
}
```

##### Pong
```json
{
  "type": "pong",
  "request_id": "req-126",
  "ts": "2025-08-29T10:03:00Z"
}
```

#### Error Codes
- `BAD_REQUEST`: Invalid message format or missing required fields
- `TOPIC_NOT_FOUND`: Attempting to publish/subscribe to non-existent topic
- `SLOW_CONSUMER`: Subscriber queue overflow, connection terminated
- `INTERNAL`: Unexpected server error

### REST API Endpoints

#### Create Topic
```
POST /topics
Content-Type: application/json

{
  "name": "orders"
}

Response: 201 Created
{
  "status": "created",
  "topic": "orders"
}
```

#### Delete Topic
```
DELETE /topics/{name}

Response: 200 OK
{
  "status": "deleted",
  "topic": "orders"
}
```

#### List Topics
```
GET /topics

Response: 200 OK
{
  "topics": [
    {
      "name": "orders",
      "subscribers": 3
    }
  ]
}
```

#### Health Check
```
GET /health

Response: 200 OK
{
  "uptime_sec": 3600,
  "topics": 5,
  "subscribers": 12
}
```

#### Statistics
```
GET /stats

Response: 200 OK
{
  "topics": {
    "orders": {
      "messages": 42,
      "subscribers": 3
    }
  }
}
```

## Running the Application

### Local Development
```bash
# Install dependencies
go mod tidy

# Run the server
go run cmd/server/main.go

# Server will start on http://localhost:8080
# WebSocket endpoint: ws://localhost:8080/ws
```

### Docker
```bash
# Build the image
docker build -t pubsub-server .

# Run the container
docker run -p 8080:8080 pubsub-server

# Access the service at http://localhost:8080
```

### Environment Variables
- `PORT`: Server port (default: 8080)

## Testing

### Manual Testing with WebSocket Client
You can test the WebSocket functionality using browser developer tools or a WebSocket client:

```javascript
// Connect to WebSocket
const ws = new WebSocket('ws://localhost:8080/ws');

// Subscribe to a topic
ws.send(JSON.stringify({
  type: "subscribe",
  topic: "test",
  client_id: "test-client",
  last_n: 5,
  request_id: "req-1"
}));

// Publish a message
ws.send(JSON.stringify({
  type: "publish",
  topic: "test",
  message: {
    id: "msg-1",
    payload: {"hello": "world"}
  },
  request_id: "req-2"
}));
```

### REST API Testing
```bash
# Create a topic
curl -X POST http://localhost:8080/topics -H "Content-Type: application/json" -d '{"name": "test"}'

# List topics
curl http://localhost:8080/topics

# Get health
curl http://localhost:8080/health

# Get stats
curl http://localhost:8080/stats

# Delete topic
curl -X DELETE http://localhost:8080/topics/test
```

## System Guarantees

### Message Delivery
- **At-most-once delivery**: Messages may be lost if subscriber is slow
- **Fan-out**: Each message delivered to all active subscribers of a topic
- **Isolation**: Messages are topic-scoped, no cross-topic leakage

### Concurrency Safety
- **Thread-safe**: All operations are protected by appropriate mutexes
- **Race-free**: Double-checked locking prevents race conditions
- **Deadlock-free**: Consistent lock ordering and bounded lock scope

### Performance Characteristics
- **Memory Bounded**: Fixed memory per topic (history) and subscriber (queue)
- **Low Latency**: Direct in-memory message routing
- **Horizontal Scaling**: Stateless design enables multiple instances with load balancing

## Limitations & Trade-offs

1. **No Persistence**: All data lost on restart (by design)
2. **Single Instance**: No built-in clustering or replication
3. **Memory Bounded**: Fixed limits may cause message/subscriber loss
4**Simple Backpressure**: Aggressive disconnection policy for slow consumers
