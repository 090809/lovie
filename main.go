package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"lovie/oteljsonl"
)

var version = "dev"

const usageFmt = `lovie %s — OpenTelemetry JSONL viewer

Usage:
  lovie [options] <file.jsonl>

Arguments:
  file.jsonl  Path to an OTLP or oteljsonl JSONL file

Options:
  -aad string
        Additional authenticated data for oteljsonl decode
  -key-hex string
        32-byte symmetric AES-256-GCM key in hex for oteljsonl decode
  -recipient-key-id string
        Optional recipient key id for oteljsonl recipient-based decode
  -recipient-private-key-hex string
        X25519 recipient private key in hex for oteljsonl recipient-based decode
`

func main() {
	if code := run(os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}

func run(args []string) int {
	opts, err := parseOptions(args)
	if err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintln(os.Stderr, err)
		}

		fmt.Fprintf(os.Stderr, usageFmt, version)

		return 1
	}

	filePath := filepath.Clean(opts.filePath)

	//nolint:gosec // Opening the user-provided local file is the primary CLI behavior.
	f, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)

		return 1
	}

	defer f.Close()

	absPath, _ := filepath.Abs(filePath)
	fmt.Fprintf(os.Stderr, "📂 Parsing %s …\n", absPath)

	data, err := parseOTLP(f, opts.decryptConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing: %v\n", err)

		return 1
	}

	data.Meta.File = filepath.Base(filePath)

	fmt.Fprintf(os.Stderr, "✔  %d traces · %d logs · %d metrics\n",
		len(data.Traces), len(data.Logs), len(data.Metrics))

	if err := serve(data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)

		return 1
	}

	return 0
}

type runOptions struct {
	filePath      string
	decryptConfig oteljsonl.DecryptConfig
}

func parseOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("lovie", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	aad := fs.String("aad", "", "")
	keyHex := fs.String("key-hex", "", "")
	recipientKeyID := fs.String("recipient-key-id", "", "")
	recipientPrivateKeyHex := fs.String("recipient-private-key-hex", "", "")

	if err := fs.Parse(args); err != nil {
		return runOptions{}, err
	}

	if fs.NArg() != 1 {
		return runOptions{}, fmt.Errorf("expected exactly one input file")
	}

	decryptConfig, err := buildDecryptConfig(*aad, *keyHex, *recipientKeyID, *recipientPrivateKeyHex)
	if err != nil {
		return runOptions{}, err
	}

	return runOptions{
		filePath:      fs.Arg(0),
		decryptConfig: decryptConfig,
	}, nil
}

func buildDecryptConfig(aad string, keyHex string, recipientKeyID string, recipientPrivateKeyHex string) (oteljsonl.DecryptConfig, error) {
	if keyHex != "" && recipientPrivateKeyHex != "" {
		return oteljsonl.DecryptConfig{}, fmt.Errorf("use either -key-hex or -recipient-private-key-hex, not both")
	}

	cfg := oteljsonl.DecryptConfig{
		AAD: []byte(aad),
	}

	if keyHex != "" {
		key, err := decodeHexArg(keyHex, "symmetric key")
		if err != nil {
			return oteljsonl.DecryptConfig{}, err
		}

		cfg.Key = key
	}

	if recipientPrivateKeyHex != "" {
		privateKey, err := decodeHexArg(recipientPrivateKeyHex, "recipient private key")
		if err != nil {
			return oteljsonl.DecryptConfig{}, err
		}

		cfg.RecipientKeys = []oteljsonl.RecipientPrivateKey{{
			KeyID:      recipientKeyID,
			PrivateKey: privateKey,
		}}
	}

	return cfg, nil
}

func decodeHexArg(value string, label string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s hex: %w", label, err)
	}

	return decoded, nil
}
