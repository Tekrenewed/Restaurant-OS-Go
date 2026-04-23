package email

import (
	"fmt"
	"net/smtp"
	"os"
)

// SendMarketingEmail sends an email using Google Workspace SMTP.
func SendMarketingEmail(to string, subject string, body string) error {
	from := "sales@faloodaandco.co.uk"
	password := os.Getenv("SMTP_PASSWORD") // App password from Google Workspace

	if password == "" {
		return fmt.Errorf("SMTP_PASSWORD environment variable is not set")
	}

	// Create RFC 822 standard email message
	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n\r\n"+
		"%s\r\n", from, to, subject, body)

	// Google Workspace SMTP uses port 587 with TLS
	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, password, "smtp.gmail.com"),
		from, []string{to}, []byte(msg))

	if err != nil {
		return fmt.Errorf("failed to send email to %s: %v", to, err)
	}

	return nil
}
