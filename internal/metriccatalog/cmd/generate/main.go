package main

import (
	"encoding/json"
	"last9-mcp/internal/metriccatalog"
	"log"
	"os"
)

func main() {
	const dir = "internal/metriccatalog/"
	raw, err := os.ReadFile(dir + "receiver_sources.json")
	if err != nil {
		log.Fatal(err)
	}
	var sources []metriccatalog.ReceiverSource
	if err := json.Unmarshal(raw, &sources); err != nil {
		log.Fatal(err)
	}
	raw, err = metriccatalog.GenerateReceivers(sources)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(dir+"receivers_generated.json", raw, 0644); err != nil {
		log.Fatal(err)
	}
}
