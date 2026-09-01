package main

import (
	"context"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/testdb"
)

func main() {
	if err := testdb.Require(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
