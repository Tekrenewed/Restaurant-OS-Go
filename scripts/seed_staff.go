package main

import (
	"context"
	"log"

	"restaurant-os/internal/firebase"
)

func main() {
	fb := firebase.InitFirebase()
	if fb == nil || fb.Firestore == nil {
		log.Fatalf("Failed to initialize Firebase")
	}

	ctx := context.Background()
	staffRef := fb.Firestore.Collection("staff")

	// Delete existing
	docs, err := staffRef.Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("Failed to get existing staff: %v", err)
	}

	for _, doc := range docs {
		_, err := doc.Ref.Delete(ctx)
		if err != nil {
			log.Printf("Failed to delete %s: %v", doc.Ref.ID, err)
		} else {
			log.Printf("Deleted %s", doc.Ref.ID)
		}
	}

	// Create new
	staffs := []map[string]interface{}{
		{"name": "Staff 1", "role": "staff", "pin": "1234"},
		{"name": "Staff 2", "role": "staff", "pin": "1234"},
		{"name": "Staff 3", "role": "staff", "pin": "1234"},
		{"name": "Staff 4", "role": "staff", "pin": "1234"},
		{"name": "Staff 5", "role": "staff", "pin": "1234"},
		{"name": "Staff 6", "role": "staff", "pin": "1234"},
		{"name": "Staff 7", "role": "staff", "pin": "1234"},
		{"name": "Staff 8", "role": "staff", "pin": "1234"},
		{"name": "Manager 1", "role": "manager", "pin": "1111"},
		{"name": "Manager 2", "role": "manager", "pin": "1111"},
		{"name": "Manager 3", "role": "manager", "pin": "1111"},
		{"name": "Aziz", "role": "owner", "pin": "2244"},
		{"name": "Azmat", "role": "owner", "pin": "2244"},
		{"name": "KDS", "role": "system", "pin": "0000"},
		{"name": "Order Pad", "role": "system", "pin": "1111"},
	}

	for _, s := range staffs {
		_, _, err := staffRef.Add(ctx, s)
		if err != nil {
			log.Printf("Failed to add %v: %v", s["name"], err)
		} else {
			log.Printf("Created %v", s["name"])
		}
	}

	log.Println("Done seeding staff.")
}
