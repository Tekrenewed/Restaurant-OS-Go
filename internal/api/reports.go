package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"restaurant-os/internal/email"
)

// DailyReportSummary contains the calculated Z-Report data
type DailyReportSummary struct {
	Date           string  `json:"date"`
	TotalRevenue   float64 `json:"total_revenue"`
	OrderCount     int     `json:"order_count"`
	CashTotal      float64 `json:"cash_total"`
	CardTotal      float64 `json:"card_total"`
	DojoTotal      float64 `json:"dojo_total"`
	RefundCount    int     `json:"refund_count"`
	RefundTotal    float64 `json:"refund_total"`
	AverageOrder   float64 `json:"average_order"`
	TopItems       []TopItem `json:"top_items"`
}

// TopItem represents a best-selling item for the day
type TopItem struct {
	Name     string `json:"name" db:"name"`
	Quantity int    `json:"quantity" db:"qty"`
	Revenue  float64 `json:"revenue" db:"revenue"`
}

// HandleDailyReport generates a Z-Report summary and emails it to the owner.
// POST /api/v1/reports/daily
//
// Triggered by Google Cloud Scheduler at 23:59 every day.
// Can also be triggered manually for testing.
func (s *Server) HandleDailyReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.DB == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}


	// Parse optional date param (default: today)
	dateStr := r.URL.Query().Get("date")
	var reportDate time.Time
	if dateStr != "" {
		var err error
		reportDate, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_date","message":"Use YYYY-MM-DD format"}`, http.StatusBadRequest)
			return
		}
	} else {
		reportDate = time.Now()
	}

	startOfDay := time.Date(reportDate.Year(), reportDate.Month(), reportDate.Day(), 0, 0, 0, 0, time.Local)
	endOfDay := time.Date(reportDate.Year(), reportDate.Month(), reportDate.Day(), 23, 59, 59, 999999999, time.Local)

	// 1. Query revenue by payment method (exclude cancelled and refunded)
	var revenueRows []struct {
		Method string  `db:"payment_method"`
		Total  float64 `db:"total"`
		Count  int     `db:"cnt"`
	}
	revenueQuery := `
		SELECT COALESCE(payment_method, 'unknown') AS payment_method, 
		       COALESCE(SUM(gross_total), 0) AS total, 
		       COUNT(*) AS cnt
		FROM orders 
		WHERE created_at >= $1 AND created_at <= $2 
		  AND status NOT IN ('cancelled', 'refunded')
		GROUP BY payment_method
	`
	if err := s.DB.Select(&revenueRows, revenueQuery, startOfDay, endOfDay); err != nil {
		log.Printf("Daily Report: Failed to query revenue: %v", err)
		http.Error(w, `{"error":"query_failed"}`, http.StatusInternalServerError)
		return
	}

	summary := DailyReportSummary{
		Date: reportDate.Format("02 January 2006"),
	}

	for _, row := range revenueRows {
		summary.TotalRevenue += row.Total
		summary.OrderCount += row.Count
		switch row.Method {
		case "cash":
			summary.CashTotal = row.Total
		case "card":
			summary.CardTotal = row.Total
		case "dojo":
			summary.DojoTotal = row.Total
		default:
			summary.CardTotal += row.Total // Assume card for unknown
		}
	}

	if summary.OrderCount > 0 {
		summary.AverageOrder = summary.TotalRevenue / float64(summary.OrderCount)
	}

	// 2. Count refunds
	var refundData struct {
		Count int     `db:"cnt"`
		Total float64 `db:"total"`
	}
	refundQuery := `
		SELECT COUNT(*) AS cnt, COALESCE(SUM(gross_total), 0) AS total 
		FROM orders 
		WHERE created_at >= $1 AND created_at <= $2 AND status = 'refunded'
	`
	if err := s.DB.Get(&refundData, refundQuery, startOfDay, endOfDay); err != nil {
		log.Printf("Daily Report: Failed to query refunds: %v", err)
	}
	summary.RefundCount = refundData.Count
	summary.RefundTotal = refundData.Total

	// 3. Top 5 selling items
	topItemsQuery := `
		SELECT oi.name, COUNT(*) AS qty, SUM(oi.price_paid) AS revenue
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE o.created_at >= $1 AND o.created_at <= $2 
		  AND o.status NOT IN ('cancelled', 'refunded')
		GROUP BY oi.name
		ORDER BY qty DESC
		LIMIT 5
	`
	if err := s.DB.Select(&summary.TopItems, topItemsQuery, startOfDay, endOfDay); err != nil {
		log.Printf("Daily Report: Failed to query top items: %v", err)
		summary.TopItems = []TopItem{}
	}

	// 4. Generate HTML email body
	htmlBody := generateZReportHTML(summary)

	// 5. Send Email via direct SMTP
	err := email.SendMarketingEmail(
		"sales@faloodaandco.co.uk",
		fmt.Sprintf("📊 Falooda & Co — Daily Z-Report — %s", summary.Date),
		htmlBody,
	)
	if err != nil {
		log.Printf("Daily Report: Failed to send email via SMTP: %v", err)
	} else {
		log.Printf("Daily Report: Email sent via SMTP for %s", summary.Date)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// generateZReportHTML builds the formatted HTML email body
func generateZReportHTML(s DailyReportSummary) string {
	topItemsHTML := ""
	for i, item := range s.TopItems {
		topItemsHTML += fmt.Sprintf(`
			<tr style="border-bottom:1px solid #eee;">
				<td style="padding:8px;">%d. %s</td>
				<td style="padding:8px;text-align:center;">%d</td>
				<td style="padding:8px;text-align:right;">£%.2f</td>
			</tr>`, i+1, item.Name, item.Quantity, item.Revenue)
	}

	if topItemsHTML == "" {
		topItemsHTML = `<tr><td colspan="3" style="padding:12px;text-align:center;color:#999;">No orders today</td></tr>`
	}

	return fmt.Sprintf(`
	<div style="font-family:'Segoe UI',Arial,sans-serif;max-width:600px;margin:0 auto;background:#fafafa;border-radius:12px;overflow:hidden;">
		<div style="background:linear-gradient(135deg,#1a1a2e,#16213e);padding:24px 32px;color:white;">
			<h1 style="margin:0;font-size:22px;">📊 Daily Z-Report</h1>
			<p style="margin:4px 0 0;opacity:0.8;font-size:14px;">Falooda & Co — %s</p>
		</div>
		
		<div style="padding:24px 32px;">
			<div style="display:flex;gap:16px;margin-bottom:24px;">
				<div style="flex:1;background:white;border-radius:8px;padding:16px;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
					<p style="margin:0;font-size:12px;color:#666;text-transform:uppercase;">Total Revenue</p>
					<p style="margin:4px 0 0;font-size:28px;font-weight:700;color:#10b981;">£%.2f</p>
				</div>
				<div style="flex:1;background:white;border-radius:8px;padding:16px;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
					<p style="margin:0;font-size:12px;color:#666;text-transform:uppercase;">Orders</p>
					<p style="margin:4px 0 0;font-size:28px;font-weight:700;color:#3b82f6;">%d</p>
				</div>
				<div style="flex:1;background:white;border-radius:8px;padding:16px;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
					<p style="margin:0;font-size:12px;color:#666;text-transform:uppercase;">Avg. Order</p>
					<p style="margin:4px 0 0;font-size:28px;font-weight:700;color:#8b5cf6;">£%.2f</p>
				</div>
			</div>

			<h3 style="margin:0 0 12px;font-size:14px;color:#374151;">💳 Payment Breakdown</h3>
			<table style="width:100%%;background:white;border-radius:8px;border-collapse:collapse;margin-bottom:24px;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
				<tr style="border-bottom:1px solid #eee;"><td style="padding:10px;">💵 Cash</td><td style="padding:10px;text-align:right;font-weight:600;">£%.2f</td></tr>
				<tr style="border-bottom:1px solid #eee;"><td style="padding:10px;">💳 Card (Manual)</td><td style="padding:10px;text-align:right;font-weight:600;">£%.2f</td></tr>
				<tr style="border-bottom:1px solid #eee;"><td style="padding:10px;">📱 Dojo (Automated)</td><td style="padding:10px;text-align:right;font-weight:600;">£%.2f</td></tr>
				<tr><td style="padding:10px;">🔄 Refunds (%d)</td><td style="padding:10px;text-align:right;font-weight:600;color:#ef4444;">-£%.2f</td></tr>
			</table>

			<h3 style="margin:0 0 12px;font-size:14px;color:#374151;">🏆 Top Sellers</h3>
			<table style="width:100%%;background:white;border-radius:8px;border-collapse:collapse;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
				<tr style="background:#f9fafb;border-bottom:2px solid #e5e7eb;">
					<th style="padding:10px;text-align:left;font-size:12px;color:#6b7280;">Item</th>
					<th style="padding:10px;text-align:center;font-size:12px;color:#6b7280;">Qty</th>
					<th style="padding:10px;text-align:right;font-size:12px;color:#6b7280;">Revenue</th>
				</tr>
				%s
			</table>
		</div>

		<div style="padding:16px 32px;background:#f3f4f6;text-align:center;font-size:11px;color:#9ca3af;">
			Generated automatically by Falooda OS • Do not reply
		</div>
	</div>`,
		s.Date,
		s.TotalRevenue, s.OrderCount, s.AverageOrder,
		s.CashTotal, s.CardTotal, s.DojoTotal,
		s.RefundCount, s.RefundTotal,
		topItemsHTML,
	)
}

// Ensure sort and context are used
var _ = sort.Strings
var _ = context.Background
