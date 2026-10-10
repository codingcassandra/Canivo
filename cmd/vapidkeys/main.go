// Command vapidkeys prints a new VAPID key pair for browser push.
// Put both values in the server's environment; never commit the private key.
package main

import (
	"fmt"
	"log"

	"github.com/codingcassandra/Canivo/internal/notify"
)

func main() {
	pub, priv, err := notify.GenerateVAPIDKeys()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("VAPID_PUBLIC_KEY=" + pub)
	fmt.Println("VAPID_PRIVATE_KEY=" + priv)
}
