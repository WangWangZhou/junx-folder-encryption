//go:build !windows

package backend

func dsStorePath(folder string) (string, error) {
	return "", errWindowsOnly
}

func writeDSStore(folder string, data []byte) error {
	return errWindowsOnly
}

func readDSStore(folder string) ([]byte, error) {
	return nil, errWindowsOnly
}

func removeDSStore(folder string) error {
	return errWindowsOnly
}
