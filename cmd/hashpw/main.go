// Command hashpw prints a PASSWORD_HASH value for the .env file:
//
//	go run ./cmd/hashpw 'my password'
//	echo -n 'my password' | go run ./cmd/hashpw -i 600000
//
// The output line looks like
// pbkdf2-sha256$600000$<base64 salt>$<base64 digest> and is verified by the
// server instead of the plaintext PASSWORD. Each run uses a fresh random
// salt, so two runs never produce the same line.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"go_file_manager/internal/server"
)

func main() {
	iterations := flag.Int("i", 0, "PBKDF2 iterations (0 = the recommended default)")
	flag.Parse()

	var password string
	if args := flag.Args(); len(args) > 0 {
		password = args[0]
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr, "no password given: pass it as an argument or pipe it on stdin")
			os.Exit(2)
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, "empty password")
		os.Exit(2)
	}

	hash, err := server.NewPasswordHash(password, *iterations)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
