package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lib/pq"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var (
	sk string
	pk string
)

var relays = []string{"wss://relay.damus.io", "wss://nos.lol", "wss://relay.current.fyi", "wss://nostr-verified.wellorder.net", "wss://nostr.mom"}

type Notification struct {
	Is_npub bool
	Pubkey  string
	Amount  int8
}

func sendNotification(pool *nostr.SimplePool, urls []string, notif string) {
	var v Notification
	json.Unmarshal([]byte(notif), &v)
	var receiver string
	if v.Is_npub {
		_, data, _ := nip19.Decode(v.Pubkey)
		if str, ok := data.(string); ok {
			receiver = str
		} else {
			return
		}
	} else {
		receiver = v.Pubkey
	}
	key, err := nip04.ComputeSharedSecret(receiver, sk)
	if err != nil {
		fmt.Println("Failed to compute secret")
		return
	}
	s := fmt.Sprintf("Someone zapped your npub.cash address: Received %d SATS \nGo to https://npub.cash/claim to claim them", v.Amount)
	content, err := nip04.Encrypt(s, key)
	ev := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindEncryptedDirectMessage,
		Tags:      nostr.Tags{[]string{"p", receiver}},
		Content:   content,
	}
	ev.Sign(sk)

	if err != nil {
		fmt.Println("Failed to encrypt message")
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
	for _, url := range urls {
		relay, err := pool.EnsureRelay(url)
		if err != nil {
			fmt.Println("Could not ensure relay connection...")
			continue
		}
		if err := relay.Publish(ctx, ev); err != nil {
			fmt.Printf("Publishing failed... %v", err)
			continue
		}
	}
}

func setupRelays(urls []string) []*nostr.Relay {
	var result []*nostr.Relay
	for _, url := range urls {
		conn, err := nostr.RelayConnect(context.Background(), url)
		if err != nil {
			fmt.Printf("could not connect to %s\n", url)
		}
		fmt.Printf("Connected to %s\n", conn.URL)
		result = append(result, conn)
	}
	return result
}

func closeConnection(pool *nostr.SimplePool, urls []string) {
	for _, url := range urls {
		relay, ok := pool.Relays.Load(url)
		if ok {
			relay.Close()
		}
	}
}

func main() {
	conninfo := os.Getenv("DB_CONN_STRING")
	sk = os.Getenv("SECRET_KEY")
	var err error
	pk, err = nostr.GetPublicKey(sk)
	if err != nil {
		log.Fatal("Could not create service: No SK/PK available or invalid")
	}

	pool := nostr.NewSimplePool(context.Background())
	defer closeConnection(pool, relays)

	minReconn := 10 * time.Second
	maxReconn := time.Minute
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			fmt.Println("Database subscription error:")
			fmt.Println(err.Error())
		}
	}
	listener := pq.NewListener(conninfo, minReconn, maxReconn, reportProblem)
	err = listener.Listen("payment_notifs")
	if err != nil {
		panic(err)
	}
	for {
		notification := <-listener.Notify
		go sendNotification(pool, relays, notification.Extra)
	}
}
