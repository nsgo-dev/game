package pgstore_test

import (
	"context"
	"fmt"
	"os"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/pgstore"
	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

// Open connects to Postgres and returns an accounts.Store with the schema created, ready to pass to accounts.New. The example needs a running database, so it is compiled but not run; read the DSN from the environment or a file, never from a command line.
func ExampleOpen() {
	ctx := context.Background()
	store, err := pgstore.Open(ctx, os.Getenv("ACCOUNTS_DATABASE_URL"))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer store.Close()

	pem, err := os.ReadFile("signing-key.pem")
	if err != nil {
		fmt.Println(err)
		return
	}
	key, err := token.ParsePrivateKey(pem)
	if err != nil {
		fmt.Println(err)
		return
	}
	svc, err := accounts.New(accounts.DefaultConfig("https://accounts.example.com", "my-game"), accounts.Options{Store: store, Key: key})
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = svc
}
