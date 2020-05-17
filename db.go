package main

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func setLimit(userID string, limit int) {
	ctx := context.Background()
	sa := option.WithCredentialsFile("./credentials.json") // Import from credentials file
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		panic(err)
	}
	client, err := app.Firestore(context.Background()) // Connect to firestore
	if err != nil {
		panic(err)
	}
	db := client.Doc("shots/" + userID)
	user, err := db.Get(ctx)
	if err != nil {
		_, err = db.Create(ctx, map[string][]float64{userID: {}})
	}

	userDoc := map[string][]float64{}
	err = user.DataTo(&userDoc)
	shots := append(userDoc[userID], -float64(limit))
	_, err = db.Set(ctx, map[string][]float64{userID: shots}) // Sets document to entry
	defer client.Close()
}

func getShotHistory(userID string, reset bool, add bool, ts float64) []float64 {
	ctx := context.Background()
	sa := option.WithCredentialsFile("./credentials.json") // Import from credentials file
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		panic(err)
	}
	client, err := app.Firestore(context.Background()) // Connect to firestore
	if err != nil {
		panic(err)
	}
	db := client.Doc("shots/" + userID)
	user, err := db.Get(ctx)
	if err != nil {
		_, err = db.Create(ctx, map[string][]float64{userID: {}})
		user, err = db.Get(ctx)
	}
	
	if err != nil {
		panic(err)
	}

	userDoc := map[string][]float64{}
	err = user.DataTo(&userDoc)
	if err != nil {
		panic(err)
	}

	if add {
		var toAdd float64
		if reset {
			toAdd = 0
		} else if isDuplicate(userDoc[userID], ts) {
			fmt.Println("Duplicate timestamp! Not adding")
			return nil
		} else {
			toAdd = ts
		}
		shots := append(userDoc[userID], toAdd)
		_, err = db.Set(ctx, map[string][]float64{userID: shots}) // Sets document to entry
		defer client.Close()
		return shots
	}
	return userDoc[userID]
}

// Check to make sure we don't re-insert the same timestamp
func isDuplicate(shotHistory []float64, ts float64) bool {
	// Assuming user doesn't spam > 5 shots in a row
	for i := 1; i <= 5 && i <= len(shotHistory); i++ {
		lastTs := shotHistory[len(shotHistory)-i]
		// Leaving in case we need to debug heroku
		fmt.Printf("last: %d, curr: %d\n", lastTs, ts)
		return lastTs == ts
	}
	return false
}

func getAll() ([]User, error) {
	ctx := context.Background()
	sa := option.WithCredentialsFile("./credentials.json") // Import from credentials file
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		panic(err)
	}
	client, err := app.Firestore(context.Background()) // Connect to firestore
	if err != nil {
		panic(err)
	}
	users := make([]User, 10)
	iter := client.Collection("shots").Documents(ctx)
	defer iter.Stop()
	for {
		user, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		userDoc := map[string][]float64{}
		err = user.DataTo(&userDoc)
		for userID, shots := range userDoc {
			highScore, _, _ := analyzeHistory(shots)
			users = append(users, User{userID, shots, highScore})
			break
		}
	}
	return users, err
}

// User - Represents key-value pair in db
type User struct {
	userID    string
	shots     []float64
	highScore int
}
