package main

import (
	"encoding/hex"
	"fmt"
	"log"
	
	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
)

func main() {
	logger := log.NewNopLogger()
	db, err := badgerdb.New("/home/z/.node/db/mainnet/badger", "", logger, nil)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()
	
	// Check for the state root that's causing the error  
	stateRoot, _ := hex.DecodeString("aedd8be7a060b082b0cb3195d0b5ba017c058468851ed93dd07eca274de000c2")
	
	val, err := db.Get(stateRoot)
	if err != nil {
		fmt.Printf("State root NOT found: %v\n", err)
	} else {
		fmt.Printf("State root FOUND! Length: %d bytes\n", len(val))
	}
}
