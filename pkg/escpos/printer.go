package escpos

import (
	"fmt"
	"net"
	"restaurant-os/internal/models"
)

// Printer represents an ESC/POS compatible thermal printer (e.g., Toast TP200)
type Printer struct {
	IPAddress string
}

// NewPrinter connects via raw TCP to port 9100
func NewPrinter(ip string) *Printer {
	return &Printer{IPAddress: ip}
}

// PrintOrder connects to the printer and outputs the receipt
func (p *Printer) PrintOrder(order models.InternalOrder, customerName string) error {
	// Port 9100 is standard for raw EPSON ESC/POS printing
	address := fmt.Sprintf("%s:9100", p.IPAddress)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return fmt.Errorf("printer unreachable at %s: %w", address, err)
	}
	defer conn.Close()

	// ESC/POS Commands
	var ESC = []byte{0x1b}
	var GS = []byte{0x1d}
	var LF = []byte{0x0a}

	// Initialize Printer
	initCmd := append(ESC, '@')
	// Center Text
	centerCmd := append(ESC, 'a', 1)
	// Left Align Text
	leftCmd := append(ESC, 'a', 0)
	// Bold On
	boldOnCmd := append(ESC, 'E', 1)
	// Bold Off
	boldOffCmd := append(ESC, 'E', 0)
	// Cut Paper
	cutCmd := append(GS, 'V', 1)

	// Build the receipt payload
	var payload []byte
	payload = append(payload, initCmd...)
	payload = append(payload, centerCmd...)
	payload = append(payload, boldOnCmd...)

	// Header
	payload = append(payload, []byte("TASTE OF VILLAGE / AZMOZ")...)
	payload = append(payload, LF...)
	payload = append(payload, boldOffCmd...)
	payload = append(payload, []byte(fmt.Sprintf("Order Ref: %s", order.ID.String()[:8]))...)
	payload = append(payload, LF...)
	payload = append(payload, []byte(fmt.Sprintf("Source: %s", order.Source))...)
	payload = append(payload, LF...)
	payload = append(payload, []byte(fmt.Sprintf("Customer: %s", customerName))...)
	payload = append(payload, LF...)
	payload = append(payload, LF...)

	// Items
	payload = append(payload, leftCmd...)
	for _, item := range order.Items {
		line := fmt.Sprintf("1x %-20s  £%.2f", item.Name, item.PricePaid)
		payload = append(payload, []byte(line)...)
		payload = append(payload, LF...)
	}

	payload = append(payload, LF...)
	
	// Footer sums
	payload = append(payload, []byte("--------------------------------")...)
	payload = append(payload, LF...)
	payload = append(payload, []byte(fmt.Sprintf("VAT:              £%.2f", order.VATTotal))...)
	payload = append(payload, LF...)
	payload = append(payload, []byte(fmt.Sprintf("Service Charge:   £%.2f", order.ServiceCharge))...)
	payload = append(payload, LF...)
	payload = append(payload, boldOnCmd...)
	payload = append(payload, []byte(fmt.Sprintf("GROSS TOTAL:      £%.2f", order.GrossTotal))...)
	payload = append(payload, boldOffCmd...)
	payload = append(payload, LF...)
	payload = append(payload, LF...)
	payload = append(payload, LF...)

	// Cut
	payload = append(payload, cutCmd...)

	// Send to physical printer
	_, err = conn.Write(payload)
	return err
}
