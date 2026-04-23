package api

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
)

// TimesheetRow represents a single shift entry for CSV export
type TimesheetRow struct {
	StaffName string
	ClockIn   time.Time
	ClockOut  time.Time
	Duration  time.Duration
}

// HandleExportTimesheets generates a downloadable CSV of shift data for payroll.
// GET /api/v1/shifts/export?from=2026-04-07&to=2026-04-14
//
// Returns a CSV file with columns: Staff Name, Date, Clock In, Clock Out, Hours Worked
// If no date range is specified, defaults to the last 7 days.
func (s *Server) HandleExportTimesheets(w http.ResponseWriter, r *http.Request) {
	tc := s.GetFirestoreForRequest(r)
	if tc == nil || tc.Firestore == nil {
		http.Error(w, `{"error":"service_unavailable","message":"Firestore not available"}`, http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	fs := tc.Firestore

	// Parse date range from query params (default: last 7 days)
	now := time.Now()
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	var fromDate, toDate time.Time
	var err error

	if fromStr != "" {
		fromDate, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_date","message":"'from' must be YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
	} else {
		fromDate = now.AddDate(0, 0, -7)
	}
	fromDate = time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, time.Local)

	if toStr != "" {
		toDate, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_date","message":"'to' must be YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
	} else {
		toDate = now
	}
	toDate = time.Date(toDate.Year(), toDate.Month(), toDate.Day(), 23, 59, 59, 0, time.Local)

	// Query completed shifts within date range
	iter := fs.Collection("shifts").
		Where("status", "==", "completed").
		Where("clockIn", ">=", fromDate).
		Where("clockIn", "<=", toDate).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil {
		log.Printf("ERROR: Failed to fetch shifts for export: %v", err)
		http.Error(w, `{"error":"server_error","message":"Failed to fetch shift data"}`, http.StatusInternalServerError)
		return
	}

	// Build rows
	var rows []TimesheetRow
	staffTotals := make(map[string]time.Duration)

	for _, doc := range docs {
		data := doc.Data()

		name, _ := data["name"].(string)
		if name == "" {
			name = "Unknown Staff"
		}

		var clockIn, clockOut time.Time

		// Firestore timestamps come back as time.Time
		if ci, ok := data["clockIn"].(time.Time); ok {
			clockIn = ci
		}
		if co, ok := data["clockOut"].(time.Time); ok {
			clockOut = co
		}

		if clockIn.IsZero() {
			continue
		}

		duration := time.Duration(0)
		if !clockOut.IsZero() {
			duration = clockOut.Sub(clockIn)
		}

		rows = append(rows, TimesheetRow{
			StaffName: name,
			ClockIn:   clockIn,
			ClockOut:  clockOut,
			Duration:  duration,
		})

		staffTotals[name] += duration
	}

	// Sort by clock-in time
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ClockIn.Before(rows[j].ClockIn)
	})

	// Generate CSV
	filename := fmt.Sprintf("%s_Timesheets_%s_to_%s.csv",
		tc.Config.DisplayName,
		fromDate.Format("02-Jan-2006"),
		toDate.Format("02-Jan-2006"),
	)

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header row
	writer.Write([]string{
		"Staff Name",
		"Date",
		"Clock In",
		"Clock Out",
		"Hours Worked",
		"Decimal Hours",
	})

	// Data rows
	for _, row := range rows {
		clockOutStr := ""
		hoursStr := ""
		decimalStr := ""

		if !row.ClockOut.IsZero() {
			clockOutStr = row.ClockOut.Format("15:04")
			hours := int(row.Duration.Hours())
			mins := int(row.Duration.Minutes()) % 60
			hoursStr = fmt.Sprintf("%dh %dm", hours, mins)
			decimalStr = fmt.Sprintf("%.2f", row.Duration.Hours())
		} else {
			clockOutStr = "STILL ACTIVE"
			hoursStr = "-"
			decimalStr = "-"
		}

		writer.Write([]string{
			row.StaffName,
			row.ClockIn.Format("02/01/2006"),
			row.ClockIn.Format("15:04"),
			clockOutStr,
			hoursStr,
			decimalStr,
		})
	}

	// Empty row separator
	writer.Write([]string{})

	// Summary section
	writer.Write([]string{"--- WEEKLY TOTALS ---", "", "", "", "", ""})

	// Sort staff names for consistent output
	staffNames := make([]string, 0, len(staffTotals))
	for name := range staffTotals {
		staffNames = append(staffNames, name)
	}
	sort.Strings(staffNames)

	var grandTotal time.Duration
	for _, name := range staffNames {
		total := staffTotals[name]
		grandTotal += total
		hours := int(total.Hours())
		mins := int(total.Minutes()) % 60
		writer.Write([]string{
			name,
			"",
			"",
			"TOTAL:",
			fmt.Sprintf("%dh %dm", hours, mins),
			fmt.Sprintf("%.2f", total.Hours()),
		})
	}

	// Grand total
	writer.Write([]string{})
	writer.Write([]string{
		"ALL STAFF",
		"",
		"",
		"GRAND TOTAL:",
		fmt.Sprintf("%dh %dm", int(grandTotal.Hours()), int(grandTotal.Minutes())%60),
		fmt.Sprintf("%.2f", grandTotal.Hours()),
	})

	log.Printf("Timesheet CSV exported: %d shifts, %d staff, %s to %s",
		len(rows), len(staffTotals),
		fromDate.Format("02-Jan-2006"), toDate.Format("02-Jan-2006"))
}
