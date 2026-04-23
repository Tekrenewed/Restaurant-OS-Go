package main

import (
	"context"
	"fmt"
	"log"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()
	opt := option.WithCredentialsFile("service-account.json")
	config := &firebase.Config{ProjectID: "faloodaandco"}
	app, err := firebase.NewApp(ctx, config, opt)
	if err != nil {
		log.Fatalf("error initializing app: %v\n", err)
	}

	client, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("error getting Auth client: %v\n", err)
	}
	defer client.Close()

	iter := client.Collection("staff").Documents(ctx)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		fmt.Printf("ID: %s, Name: %v, PIN: %v\n", doc.Ref.ID, data["name"], data["pin"])
	}
}
