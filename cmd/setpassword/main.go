package main

import (
	"context"
	"flag"
	"log"
	"os"

	"restaurant-os/internal/firebase"
	"firebase.google.com/go/v4/auth"
)

func main() {
	email := flag.String("email", "", "User's email")
	password := flag.String("password", "", "New password")
	flag.Parse()

	if *email == "" || *password == "" {
		log.Fatal("ERROR: Must provide -email and -password")
	}

	fb := firebase.InitFirebase()
	ctx := context.Background()

	user, err := fb.Auth.GetUserByEmail(ctx, *email)
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	params := (&auth.UserToUpdate{}).Password(*password)
	_, err = fb.Auth.UpdateUser(ctx, user.UID, params)
	if err != nil {
		log.Fatalf("ERROR setting password: %v", err)
	}

	log.Printf("Successfully updated password for %s", *email)
	os.Exit(0)
}
