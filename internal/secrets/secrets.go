// Package secrets manages ~/.yashigatakae/secrets.env via SSH from the VPS.
// v0.1 stubs Pull/Push to print a friendly TODO; v0.7 implements them properly.
package secrets

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

func Path() (string, error) {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(yashDir, "secrets.env"), nil
}

// Pull is implemented in v0.7 (token rotation + secrets management milestone).
func Pull() error {
	p, _ := Path()
	if _, err := os.Stat(p); err == nil {
		fmt.Printf("secrets.env already exists at %s\n", p)
		fmt.Println("v0.1 does not auto-sync; ships in v0.7.")
		return nil
	}
	fmt.Println("secrets.env not present — copy your secrets.example.env from yashigatakae-state and fill it in.")
	fmt.Println("Auto-sync from VPS ships in v0.7.")
	return nil
}

func Push() error {
	fmt.Println("secrets push: ships in v0.7. For now, scp manually.")
	return nil
}
