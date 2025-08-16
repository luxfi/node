// Package tracker provides network tracking functionality
package tracker

// Tracker tracks network connections and peers
type Tracker interface {
	// Track starts tracking a peer
	Track(peerID string) error
	
	// Untrack stops tracking a peer
	Untrack(peerID string) error
	
	// IsTracked checks if a peer is being tracked
	IsTracked(peerID string) bool
	
	// GetTrackedPeers returns all tracked peers
	GetTrackedPeers() []string
}