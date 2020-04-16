// Copyright(c) 2016 Derek Collison (derek.collison@gmail.com)

package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/nats-io/nats.go"
)

const (
	appName       = "secure-nats-chat"
	version       = "0.4"
	proto         = "1"
	serverName    = "demo.nats.io"
	secureNatsURL = "tls://demo.nats.io:4443"
)

func usage() {
	log.Fatalf("Usage: nats-chat <subject> <key>\n")
}

// Hold name, key and subject
var name, subj, key string
var keyHash []byte

// Cipher
var gcm cipher.AEAD
var nonce []byte

// Messages we send
type chat struct {
	Name string `json:"name"`
	Msg  string `json:"encrypted_msg"`
}

func main() {
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
	}

	subj, key = args[0], args[1]
	subj = fmt.Sprintf("snats.%s.%s", proto, subj)

	h := sha256.New()
	h.Write([]byte(key))
	keyHash = h.Sum(nil)

	// Create cipher
	var err error
	block, err := aes.NewCipher(keyHash)
	if err != nil {
		log.Fatalf("Can't create cipher: %v\n", err)
	}
	gcm, err = cipher.NewGCMWithNonceSize(block, sha256.Size)
	if err != nil {
		log.Fatalf("Can't create gcm: %v\n", err)
	}

	// Generate the nonce
	h.Write([]byte(subj))
	h.Write(keyHash)
	nonce = h.Sum(nil)

	// Connect securely to NATS
	nc, err := nats.Connect(secureNatsURL, nats.Name(appName))
	if err != nil {
		log.Fatalf("Got an error on Connect with Secure Options: %+v\n", err)
	}

	log.Printf("Securely connected to %s", secureNatsURL)
	ec, _ := nats.NewEncodedConn(nc, nats.JSON_ENCODER)

	// Setup signal handlers to signal leaving.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	go func() {
		for range c {
			fmt.Printf("\n\n")
			if name != "" {
				exit := &chat{Name: name, Msg: encrypt("<left>\n")}
				ec.Publish(subj, exit)
				ec.Flush()
			}
			os.Exit(0)
		}
	}()

	// Collect the name
	fmt.Printf("Enter Name: ")
	fmt.Scanln(&name)

	// Create a reader for stdin
	reader := bufio.NewReader(os.Stdin)

	// Subscribe to messages
	ec.Subscribe(subj, func(msg *chat) {
		if msg.Name == name {
			return
		}
		fmt.Printf("\033[2K\r[%s] %s", msg.Name, decrypt(msg))
		fmt.Printf("[%s] ", name)
	})

	// Send welcome
	welcome := &chat{Name: name, Msg: encrypt("<joined>\n")}
	ec.Publish(subj, welcome)

	// Wait on new messages to send
	for {
		fmt.Printf("[%s] ", name)
		msg, _ := reader.ReadString('\n')
		// Don't send any space.
		if strings.TrimSpace(msg) == "" {
			continue
		}
		ec.Publish(subj, chat{Name: name, Msg: encrypt(msg)})
	}
}

func encrypt(msg string) string {
	plaintext := []byte(msg)
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(name))
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func decrypt(msg *chat) string {
	data, _ := base64.StdEncoding.DecodeString(msg.Msg)
	v, err := gcm.Open(nil, nonce, data, []byte(msg.Name))
	if err != nil {
		return "<unable to decrypt, wrong key?>\n"
	}
	return string(v)
}
