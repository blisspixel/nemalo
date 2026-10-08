package main

import (
	"context"
	"fmt"
	"os"

	"github.com/blisspixel/nemalo/internal/verify"
)

func main() {
	if err := verify.Run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
