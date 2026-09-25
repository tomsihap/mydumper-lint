//go:build !unix

package fix

import "os"

func preserveOwner(*os.File, os.FileInfo) {}

func syncDir(string) {}
