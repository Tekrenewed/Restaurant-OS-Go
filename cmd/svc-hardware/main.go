package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"restaurant-os/internal/models"
	"restaurant-os/pkg/escpos"
	"restaurant-os/pkg/events"
)

func main() {
	log.Println("svc-hardware starting... Connecting to Event Bus (Firebase/Redis adapter)")

	// Initialize Event Bus (Must match the instance or connect to real Redis in prod)
	// For local simulation, we initialize a new instance (in reality, Redis PubSub links them)
	eventBus := events.NewLocalPubSub()

	log.Println("Connected to Event Bus. Subscribing to PRINT_JOB topic...")

	err := eventBus.Subscribe(context.Background(), "PRINT_JOB", func(e events.Event) {
		log.Printf("🔥 RECEIVED PRINT JOB FOR TENANT [%s] 🔥", e.TenantID)
		
		var order models.InternalOrder
		if err := json.Unmarshal(e.Payload, &order); err != nil {
			log.Printf("ERROR: Failed to unmarshal PRINT_JOB payload: %v", err)
			return
		}

		printerIP := os.Getenv("PRINTER_IP")
		if printerIP == "" {
			log.Println("WARNING: PRINTER_IP not set in .env. Defaulting to 192.168.1.200")
			printerIP = "192.168.1.200"
		}

		log.Printf("--> Formatting ESC/POS commands for Order %s...", order.ID.String()[:8])
		log.Printf("--> Sending to Network Printer IP: %s", printerIP)

		printer := escpos.NewPrinter(printerIP)
		
		// In a real system, we might pull customer name from the payload if it existed
		customerName := "Delivery Customer" 
		
		if err := printer.PrintOrder(order, customerName); err != nil {
			log.Printf("CRITICAL: Failed to print order: %v", err)
			// Implement retry logic or alert management here in V2
		} else {
			log.Printf("✅ Successfully sent order %s to printer!", order.ID.String()[:8])
		}
	})

	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}

	// Wait for interrupt
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("svc-hardware shutting down.")
}
