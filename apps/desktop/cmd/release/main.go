// Command release makes the update channel's files (D-28, §10.2).
//
//	release keygen -out DIR
//	    writes update-key (private, base64) and update-key.pub (public, base64)
//	release manifest -version V -page URL -base URL -out FILE linux-appimage=PATH linux-deb=PATH …
//	    writes latest.json, signed with the key in $SONG_SLEEVE_UPDATE_KEY (base64) or
//	    the file in -key
//
// The private key lives in GitHub Actions secrets with an offline backup; the
// public key is built into the app.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: release keygen|manifest …")
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "manifest":
		manifest(os.Args[2:])
	default:
		fail("unknown command " + os.Args[1])
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}

func keygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", ".", "folder for update-key and update-key.pub")
	fs.Parse(args)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail(err.Error())
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		fail(err.Error())
	}
	privPath := filepath.Join(*out, "update-key")
	if _, err := os.Stat(privPath); err == nil {
		fail(privPath + " exists; not overwriting a signing key")
	}
	if err := os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(filepath.Join(*out, "update-key.pub"), []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
}

func manifest(args []string) {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	version := fs.String("version", "", "release version, e.g. 0.2.0")
	page := fs.String("page", "", "the release page URL")
	base := fs.String("base", "", "URL the assets are downloaded from (asset name appended)")
	keyFile := fs.String("key", "", "private key file (default: $SONG_SLEEVE_UPDATE_KEY)")
	out := fs.String("out", "latest.json", "output file")
	fs.Parse(args)
	if *version == "" || *base == "" || fs.NArg() == 0 {
		fail("manifest needs -version, -base and at least one KEY=PATH asset")
	}
	encoded := os.Getenv("SONG_SLEEVE_UPDATE_KEY")
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			fail(err.Error())
		}
		encoded = string(b)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		fail("no valid signing key: set $SONG_SLEEVE_UPDATE_KEY or -key")
	}

	m := update.Manifest{Version: *version, Published: time.Now().UTC().Truncate(time.Second), PageURL: *page, Assets: map[string]update.Asset{}}
	for _, arg := range fs.Args() {
		k, path, ok := strings.Cut(arg, "=")
		if !ok {
			fail("asset " + arg + " isn't KEY=PATH")
		}
		sum, size, err := hashFile(path)
		if err != nil {
			fail(err.Error())
		}
		name := filepath.Base(path)
		m.Assets[k] = update.Asset{Name: name, URL: strings.TrimSuffix(*base, "/") + "/" + name, SHA256: sum, Size: size}
	}
	b, err := update.Sign(m, ed25519.PrivateKey(key))
	if err != nil {
		fail(err.Error())
	}
	if _, err := update.Verify(b, ed25519.PrivateKey(key).Public().(ed25519.PublicKey)); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fail(err.Error())
	}
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
