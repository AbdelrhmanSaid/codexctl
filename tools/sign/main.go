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
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "write the private key to this file (mode 0600) instead of stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	public, private, err := update.GenerateKey()
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Println(private)
	} else {
		if err := os.WriteFile(*out, []byte(private+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "private key written to %s; store it as the %s secret\n", *out, keyEnv)
	}
	fmt.Fprintf(os.Stderr, "public key (embed in internal/update/sign.go):\n")
	fmt.Fprintln(os.Stderr, public)
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	out := fs.String("out", "", "signature output path (default FILE.sig)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sign sign -out SIG FILE")
	}
	file := fs.Arg(0)
	encoded := os.Getenv(keyEnv)
	if encoded == "" {
		return fmt.Errorf("%s is not set", keyEnv)
	}
	key, err := update.DecodePrivateKey(encoded)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = file + ".sig"
	}
	return os.WriteFile(*out, update.Sign(key, data), 0o644)
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	pub := fs.String("key", "", "base64 public key (default: the key embedded in codexctl)")
	sig := fs.String("sig", "", "signature path (default FILE.sig)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: sign verify [-key PUB] [-sig SIG] FILE")
	}
	file := fs.Arg(0)
	if *sig == "" {
		*sig = file + ".sig"
	}
	pk, err := update.PublicKey()
	if *pub != "" {
		pk, err = update.DecodePublicKey(*pub)
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	signature, err := os.ReadFile(*sig)
	if err != nil {
		return err
	}
	if err := update.Verify(pk, data, signature); err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}
