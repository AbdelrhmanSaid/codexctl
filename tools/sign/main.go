// Command sign manages the Ed25519 key that signs codexctl releases.
//
//	go run ./tools/sign keygen -out FILE     write a private key, print the public key
//	go run ./tools/sign sign -out SIG FILE   sign FILE with $CODEXCTL_SIGNING_KEY
//	go run ./tools/sign verify -key PUB -sig SIG FILE
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AbdelrhmanSaid/codexctl/internal/update"
)

const keyEnv = "CODEXCTL_SIGNING_KEY"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sign keygen|sign|verify")
	}

	switch args[0] {
	case "keygen":
		return keygen(args[1:])
	case "sign":
		return sign(args[1:])
	case "verify":
		return verify(args[1:])
	}

	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	outPath := flags.String("out", "", "write the private key to this file (mode 0600) instead of stdout")

	if err := flags.Parse(args); err != nil {
		return err
	}

	public, private, err := update.GenerateKey()
	if err != nil {
		return err
	}

	if *outPath == "" {
		fmt.Println(private)
	} else {
		if err := os.WriteFile(*outPath, []byte(private+"\n"), 0o600); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "private key written to %s; store it as the %s secret\n", *outPath, keyEnv)
	}

	fmt.Fprintf(os.Stderr, "public key (embed in internal/update/sign.go):\n")
	fmt.Fprintln(os.Stderr, public)

	return nil
}

func sign(args []string) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	outPath := flags.String("out", "", "signature output path (default FILE.sig)")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 1 {
		return fmt.Errorf("usage: sign sign -out SIG FILE")
	}

	file := flags.Arg(0)

	encodedKey := os.Getenv(keyEnv)
	if encodedKey == "" {
		return fmt.Errorf("%s is not set", keyEnv)
	}

	key, err := update.DecodePrivateKey(encodedKey)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	if *outPath == "" {
		*outPath = file + ".sig"
	}

	return os.WriteFile(*outPath, update.Sign(key, data), 0o644)
}

func verify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	publicKeyFlag := flags.String("key", "", "base64 public key (default: the key embedded in codexctl)")
	sigPath := flags.String("sig", "", "signature path (default FILE.sig)")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 1 {
		return fmt.Errorf("usage: sign verify [-key PUB] [-sig SIG] FILE")
	}

	file := flags.Arg(0)
	if *sigPath == "" {
		*sigPath = file + ".sig"
	}

	publicKey, err := update.PublicKey()
	if *publicKeyFlag != "" {
		publicKey, err = update.DecodePublicKey(*publicKeyFlag)
	}

	if err != nil {
		return err
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	signature, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}

	if err := update.Verify(publicKey, data, signature); err != nil {
		return err
	}

	fmt.Println("ok")

	return nil
}
