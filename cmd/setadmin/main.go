package main

import (
	"context"
	"flag"
	"log"
	"os"

	"restaurant-os/internal/firebase"
)

func main() {
	email := flag.String("email", "", "The Firebase Email to grant admin access to")
	revoke := flag.Bool("revoke", false, "Set to true to revoke admin access")
	flag.Parse()

	if *email == "" {
		log.Fatal("ERROR: You must provide an email using -email=<user-email>")
	}

	// Make sure GOOGLE_APPLICATION_CREDENTIALS is set locally, or rely on gcloud ADC
	log.Println("Initializing Firebase Admin SDK...")
	fb := firebase.InitFirebase()
	if fb == nil || fb.Auth == nil {
		log.Fatal("ERROR: Failed to initialize Firebase Auth Client. Are your credentials set?")
	}

	ctx := context.Background()

	// Fetch existing claims to avoid overwriting other potential custom claims
	user, err := fb.Auth.GetUserByEmail(ctx, *email)
	if err != nil {
		log.Fatalf("ERROR: Failed to find user with email %s: %v", *email, err)
	}

	claims := user.CustomClaims
	if claims == nil {
		claims = make(map[string]interface{})
	}

	if *revoke {
		claims["admin"] = false
		log.Printf("Revoking admin access for %s (%s)...", user.Email, user.UID)
	} else {
		claims["admin"] = true
		log.Printf("Granting admin access to %s (%s)...", user.Email, user.UID)
	}

	// Apply claims
	err = fb.Auth.SetCustomUserClaims(ctx, user.UID, claims)
	if err != nil {
		log.Fatalf("ERROR: Failed to set custom claims: %v", err)
	}

	if *revoke {
		log.Println("SUCCESS: Admin access revoked.")
	} else {
		log.Println("SUCCESS: User is now a cryptographically verified Admin.")
		log.Println("NOTE: The user must log out and log back in for the token to refresh and pick up the new claims.")
	}
	os.Exit(0)
}
