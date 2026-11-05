//go:build !unix && !windows

package lockfile

import (
	"fmt"
	"os"
)

func lockFile(*os.File) error   { return fmt.Errorf("file locking is not supported on this platform") }
func unlockFile(*os.File) error { return nil }
