package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"restaurant-os/internal/models"
	"restaurant-os/pkg/escpos"
)

func main() {
	log.Println("svc-hardware (Print Bridge) starting...")

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
		log.Printf("WARNING: API_URL not set in .env. Defaulting to %s", apiURL)
	}

	storeID := os.Getenv("STORE_ID")
	if storeID == "" {
		storeID = "f4100da2-1111-1111-1111-000000000001" // Default main store
		log.Printf("WARNING: STORE_ID not set in .env. Defaulting to %s", storeID)
	}

	printerIP := os.Getenv("PRINTER_IP")
	if printerIP == "" {
		printerIP = "192.168.1.200"
		log.Printf("WARNING: PRINTER_IP not set in .env. Defaulting to %s", printerIP)
	}

	printer := escpos.NewPrinter(printerIP)
	
	pollInterval := 5 * time.Second
	log.Printf("Starting polling every %v for Store: %s", pollInterval, storeID)

	// Create a channel to handle clean shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	httpClient := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-stopChan:
			log.Println("svc-hardware shutting down.")
			return
		case <-ticker.C:
			pollPrintQueue(httpClient, apiURL, storeID, printer)
		}
	}
}

func pollPrintQueue(client *http.Client, apiURL, storeID string, printer *escpos.Printer) {
	url := fmt.Sprintf("%s/api/v1/stores/%s/print-queue", apiURL, storeID)
	
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("ERROR: Failed to connect to %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("ERROR: Print queue returned status %d", resp.StatusCode)
		return
	}

	var orders []models.InternalOrder
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		log.Printf("ERROR: Failed to decode print queue: %v", err)
		return
	}

	if len(orders) > 0 {
		log.Printf("Found %d order(s) in print queue", len(orders))
	}

	for _, order := range orders {
		log.Printf("--> Formatting ESC/POS commands for Order %s...", order.ID.String()[:8])
		
		customerName := "Customer"
		if order.CustomerName != nil && *order.CustomerName != "" {
			customerName = *order.CustomerName
		} else if order.TableNumber != nil {
			customerName = fmt.Sprintf("Table %d", *order.TableNumber)
		}

		// Print order
		if err := printer.PrintOrder(order, customerName); err != nil {
			log.Printf("CRITICAL: Failed to print order %s: %v", order.ID.String()[:8], err)
			continue // Do not mark as printed if printing failed
		}

		log.Printf("✅ Successfully sent order %s to printer!", order.ID.String()[:8])

		// Mark as printed
		markPrinted(client, apiURL, order.ID.String())
	}
}

func markPrinted(client *http.Client, apiURL, orderID string) {
	url := fmt.Sprintf("%s/api/v1/orders/%s/printed", apiURL, orderID)
	
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewBuffer(nil))
	if err != nil {
		log.Printf("ERROR: Failed to create request to mark printed %s: %v", orderID, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to mark order %s printed: %v", orderID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("ERROR: Failed to mark order %s printed, status %d", orderID, resp.StatusCode)
	} else {
		log.Printf("✅ Order %s marked as printed in database", orderID[:8])
	}
}
