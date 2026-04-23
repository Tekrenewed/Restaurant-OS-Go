package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// cloudPrntPollRequest represents the JSON body sent by the Star printer
type cloudPrntPollRequest struct {
	PrinterMAC     string `json:"printerMAC"`
	StatusCode     string `json:"statusCode"`
	ClientAction   interface{} `json:"clientAction"`
}

// cloudPrntPollResponse tells the printer if there's a task waiting
type cloudPrntPollResponse struct {
	JobReady   bool     `json:"jobReady"`
	MediaTypes []string `json:"mediaTypes,omitempty"`
}

// HandleCloudPrntPoll handles POST /api/v1/printer
// Star printer checks this endpoint continuously
func (s *Server) HandleCloudPrntPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req cloudPrntPollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// In a real multi-printer setup, we would filter by MAC address.
	// For now, we check the DB for ANY pending unprinted kitchen orders.
	var pendingCount int
	err := s.DB.Get(&pendingCount, "SELECT COUNT(*) FROM orders WHERE is_printed = false AND status = 'kitchen'")
	if err != nil {
		log.Printf("DB error checking print jobs: %v", err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	resp := cloudPrntPollResponse{
		JobReady: pendingCount > 0,
	}
	if resp.JobReady {
		// We tell the printer we will supply Star Document Markup
		resp.MediaTypes = []string{"text/vnd.star.markup"}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleCloudPrntDownload handles GET /api/v1/printer
// If JobReady was true, the printer calls this to fetch the actual receipt
func (s *Server) HandleCloudPrntDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Fetch the oldest unprinted kitchen order
	type printOrder struct {
		ID            uuid.UUID `db:"id"`
		TableNumber   *int      `db:"table_number"`
		CustomerName  *string   `db:"customer_name"`
		OrderSource   *string   `db:"order_source"`
		GrossTotal    float64   `db:"gross_total"`
	}

	var fetchOrder printOrder
	err := s.DB.Get(&fetchOrder, "SELECT id, table_number, customer_name, order_source, gross_total FROM orders WHERE is_printed = false AND status = 'kitchen' ORDER BY created_at ASC LIMIT 1")
	if err != nil {
		// E.g., sql.ErrNoRows if nothing is there
		http.Error(w, "No jobs available", http.StatusNoContent)
		return
	}

	// Fetch items for this order
	type printItem struct {
		Name      string  `db:"name"`
		PricePaid float64 `db:"price_paid"`
		Quantity  int
	}

	var items []printItem
	// Since we don't have quantity directly in order_items right now, we group by name
	err = s.DB.Select(&items, "SELECT name, MAX(price_paid) as price_paid, COUNT(*) as quantity FROM order_items WHERE order_id = $1 GROUP BY name", fetchOrder.ID)
	if err != nil {
		log.Printf("Failed to fetch order items for printing: %v", err)
	}

	// Generate Star Document Markup (a simple tagging language they support natively)
	var sb strings.Builder
	sb.WriteString("[align: centre][font: a]\n")
	sb.WriteString("[mag: w 2, h 2]Falooda & Co[mag]\n")
	sb.WriteString("Luxury Desserts & Street Food\n")
	sb.WriteString("Farnham Road, Slough\n\n")

	sb.WriteString("[align: left]\n")
	sb.WriteString(fmt.Sprintf("Order Ref: %s\n", strings.Split(fetchOrder.ID.String(), "-")[0]))
	
	if fetchOrder.TableNumber != nil && *fetchOrder.TableNumber > 0 {
		sb.WriteString(fmt.Sprintf("[mag: w 2, h 2]Table %d[mag]\n", *fetchOrder.TableNumber))
	} else if fetchOrder.CustomerName != nil && *fetchOrder.CustomerName != "" {
		sb.WriteString(fmt.Sprintf("[mag: w 2, h 2]Name: %s[mag]\n", *fetchOrder.CustomerName))
	} else {
		sb.WriteString(fmt.Sprintf("Source: %s\n", *fetchOrder.OrderSource))
	}
	sb.WriteString("--------------------------------\n")

	for _, item := range items {
		sb.WriteString(fmt.Sprintf("%dx %s\n", item.Quantity, item.Name))
	}

	sb.WriteString("--------------------------------\n")
	sb.WriteString("[align: right]\n")
	sb.WriteString(fmt.Sprintf("[mag: w 2, h 2]Total: £%.2f[mag]\n", fetchOrder.GrossTotal))

	sb.WriteString("\n[align: centre]\n")
	sb.WriteString("Thank you for your order!\n\n")
	
	// Join Loyalty Call to Action
	sb.WriteString("[mag: w 1, h 1]Earn Free Rewards![mag]\n")
	sb.WriteString("Scan to join our loyalty club\n")
	sb.WriteString("[barcode: type qr, data https://faloodaandco.co.uk/join?ref=receipt, module 6]\n")
	
	sb.WriteString("[cut]\n")

	// We MUST pass the token so the printer can tell us WHICH order printed successfully
	w.Header().Set("X-StarPRNT-Job-Token", fetchOrder.ID.String())
	w.Header().Set("Content-Type", "text/vnd.star.markup")
	w.Write([]byte(sb.String()))
}

// HandleCloudPrntDelete handles DELETE /api/v1/printer
// The printer calls this with the job token to acknowledge it finished printing
func (s *Server) HandleCloudPrntDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	// Format code e.g. 200... something starting with 2 for success
	statusCode := r.URL.Query().Get("code") 
	if statusCode != "" && !strings.HasPrefix(statusCode, "2") {
		log.Printf("Printer returned error code %s for job %s", statusCode, token)
		// Usually we wouldn't mark it printed if the code says it jammed
		w.WriteHeader(http.StatusOK)
		return
	}

	jobID, err := uuid.Parse(token)
	if err != nil {
		http.Error(w, "Invalid token format", http.StatusBadRequest)
		return
	}

	// Mark printed
	_, err = s.DB.Exec("UPDATE orders SET is_printed = true, printed_at = NOW() WHERE id = $1", jobID)
	if err != nil {
		log.Printf("Failed to mark order %s as printed: %v", token, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
