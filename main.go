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

type Notification struct {
	Is_npub bool
	Pubkey  string
	Amount  int8
}

func sendNotification(ctx context.Context, relays []*nostr.Relay, notif string) {
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
	if receiver != "ddf03aca85ade039e6742d5bef3df352df199d0d31e22b9858e7eda85cb3bbbe" {
		return
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
	for _, conn := range relays {
		if err := conn.Publish(ctx, ev); err != nil {
			fmt.Println(err)
			continue
		}

		fmt.Printf("published to %s\n", conn.URL)
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

func closeConnection(connections []*nostr.Relay) {
	for _, conn := range connections {
		err := conn.Close()
		if err != nil {
			fmt.Printf("Failed to close connection: %s", conn.URL)
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

	relays := []string{"wss://relay.damus.io"}
	connections := setupRelays(relays)
	defer closeConnection(connections)

	minReconn := 10 * time.Second
	maxReconn := time.Minute
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
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
		ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
		go sendNotification(ctx, connections, notification.Extra)
	}
}