package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
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

var testers = []string{"ddf03aca85ade039e6742d5bef3df352df199d0d31e22b9858e7eda85cb3bbbe", "0e8c41eb946657188ea6f2aac36c25e393fff4d4149a83679220d66595ff0faa", "d3d74124ddfb5bdc61b8f18d17c3335bbb4f8c71182a35ee27314a49a4eb7b1d", "57324d9d19581e6b630368ca179015c80f970bd75eb236728ebc49738f81ad8d"}

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
	if !slices.Contains(testers, receiver) {
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

		fmt.Printf("published to %s\n", relay.URL)
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

	relays := []string{"wss://relay.damus.io"}
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
