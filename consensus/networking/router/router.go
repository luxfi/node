// Package router provides networking router functionality
package router

// Router handles network message routing
type Router interface {
	// Route sends a message to the appropriate handler
	Route(msg Message) error
	
	// RegisterHandler registers a message handler
	RegisterHandler(msgType string, handler Handler) error
}

// Message represents a network message
type Message interface {
	Type() string
	Payload() []byte
}

// Handler processes messages
type Handler interface {
	Handle(msg Message) error
}