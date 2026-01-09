// Simple example showing basic query usage.
package main

import (
	"context"
	"fmt"
	"log"

	claude "github.com/character-tech/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// Simple synchronous query
	text, result, err := claude.QuerySync(ctx, "What is 2 + 2? Reply with just the number.")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", text)
	if result != nil {
		fmt.Printf("Cost: $%.4f, Tokens: %d in / %d out\n",
			result.Cost, result.InputTokens, result.OutputTokens)
	}
}
