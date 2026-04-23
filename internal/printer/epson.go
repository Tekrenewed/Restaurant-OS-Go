package printer

import (
	"fmt"
	"net"
	"time"
)

// EpsonPrinter handles ESC/POS commands over TCP to the TM-m30.
type EpsonPrinter struct {
	IPAddress string
}

func NewEpsonPrinter(ip string) *EpsonPrinter {
	return &EpsonPrinter{IPAddress: ip}
}

// PrintReceipt sends standard ESC/POS commands to print and cut.
func (p *EpsonPrinter) PrintReceipt(content string) error {
	// Connect to the printer on default TCP port 9100
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:9100", p.IPAddress), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to printer at %s: %v", p.IPAddress, err)
	}
	defer conn.Close()

	// 1. Initialize Printer (ESC @)
	conn.Write([]byte{0x1B, 0x40})

	// 2. Select character code table (ESC t n) - PC858 for Euro/UK characters
	conn.Write([]byte{0x1B, 0x74, 0x13})

	// 3. Print Content
	conn.Write([]byte(content))

	// 4. Feed a few lines
	conn.Write([]byte{0x0A, 0x0A, 0x0A, 0x0A, 0x0A})

	// 5. Partial cut (GS V 66)
	conn.Write([]byte{0x1D, 0x56, 0x42, 0x00})

	return nil
}
