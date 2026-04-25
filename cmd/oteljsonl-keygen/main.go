package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/090809/oteljsonl"
)

func main() {
	keyID := flag.String("key-id", "", "optional key id to print; default is derived from the public key")

	flag.Parse()

	publicKey, privateKey, err := oteljsonl.GenerateX25519KeyPair()
	if err != nil {
		log.Fatal(err)
	}

	resolvedKeyID := *keyID
	if resolvedKeyID == "" {
		sum := sha256.Sum256(publicKey)
		resolvedKeyID = hex.EncodeToString(sum[:8])
	}

	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	publicKeyHex := hex.EncodeToString(publicKey)
	privateKeyHex := hex.EncodeToString(privateKey)

	fmt.Fprintf(writer, "key_id=%s\n", resolvedKeyID)
	fmt.Fprintf(writer, "public_key_hex=%s\n", publicKeyHex)
	fmt.Fprintf(writer, "private_key_hex=%s\n", privateKeyHex)
	fmt.Fprintf(writer, "cmd_gen_flags=-recipient-key-id %s -recipient-public-key-hex %s\n", resolvedKeyID, publicKeyHex)
	fmt.Fprintf(writer, "lovie_flags=-recipient-key-id %s -recipient-private-key-hex %s\n", resolvedKeyID, privateKeyHex)
}
